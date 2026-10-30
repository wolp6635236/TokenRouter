package provider

import (
	"bytes"
	"errors"
	"io"
	"strings"

	"github.com/tidwall/gjson"

	"github.com/TokenFlux/TokenRouter/internal/pkg/logredact"
	"github.com/TokenFlux/TokenRouter/internal/protocol/openai"
	"github.com/TokenFlux/TokenRouter/internal/upstream"
	"github.com/TokenFlux/TokenRouter/internal/upstream/gemini"
	upstreamopenai "github.com/TokenFlux/TokenRouter/internal/upstream/openai"
	"github.com/TokenFlux/TokenRouter/internal/upstream/qoder"
	"github.com/TokenFlux/TokenRouter/internal/usage"
)

// system_usage_stream.go extracts accounting evidence from bounded test
// response frames while leaving preview parsing independent. Cumulative
// snapshots replace earlier observations instead of adding the same tokens
// repeatedly. Messages input excludes cache buckets already, whereas OpenAI
// input includes them; the shared protocol parsers retain each convention until
// the final usage row receives disjoint input and cache quantities.
//
// Completion evidence and usage presence remain separate. A successful reply
// can lack usage, and a failed reply can carry real billable tokens. Oversized
// frames and interrupted reads retain known counters but prevent a complete
// usage claim. Frame bytes exist only until parsing and are never persisted;
// the observer retains model identifiers, numeric counters, and status flags.

// systemUsageObserver retains bounded parsing state for one model exchange.
// Its owner serializes reads and invokes apply only after all reading stops.
type systemUsageObserver struct {
	buffer        []byte              // incomplete transient frame bytes
	sse           bool                // response uses SSE framing
	messages      bool                // request uses Messages semantics
	discarding    bool                // current frame exceeded limit
	platform      string              // provider parsing family
	image         bool                // dedicated image generation endpoint
	ended         bool                // original reader reached EOF or error
	terminal      bool                // protocol completion was observed
	failed        bool                // response carries failure evidence
	truncated     bool                // parsing or reading was incomplete
	seenInput     bool                // explicit valid input count observed
	seenOutput    bool                // explicit valid output count observed
	seenUsage     bool                // explicit usage counters were observed
	openAI        bool                // input includes cache subcategories
	tokens        upstream.TokenUsage // latest normalized protocol counters
	imageInput    int                 // observed image input subset
	imageCount    int                 // observed completed image quantity
	responseTier  string              // observed upstream billing tier
	responseModel string              // observed upstream model identity
}

// consume retains incomplete response frames and parses complete SSE lines.
// The caller serializes reads and invokes finish when the original reader ends;
// nonstreaming JSON stays buffered until that completion step. A frame can span
// several reads without losing its prefix or merging separate newline frames.
// An oversized SSE line is discarded through its newline, allowing later usage
// to survive while marking the observation partial. Nonstreaming JSON has no
// recovery boundary and is discarded as a whole if it exceeds the limit.
func (o *systemUsageObserver) consume(data []byte) {
	if !o.sse {
		if o.discarding {
			return
		}
		if len(o.buffer)+len(data) > systemUsageFrameLimit {
			// A single JSON document cannot resume at an interior byte boundary;
			// release its content while keeping already observed numeric evidence.
			o.truncated, o.discarding = true, true
			o.buffer = nil
			return
		}
		o.buffer = append(o.buffer, data...)
		return
	}
	for len(data) > 0 {
		index := bytes.IndexByte(data, '\n')
		line := data
		if index >= 0 {
			line = data[:index]
		}
		if !o.discarding {
			if len(o.buffer)+len(line) > systemUsageFrameLimit {
				// Discard the entire oversized line, including prefixes received
				// earlier, so a suffix cannot masquerade as a new usage event.
				o.truncated = true
				o.discarding = true
				o.buffer = nil
			} else {
				o.buffer = append(o.buffer, line...)
			}
		}
		if index < 0 {
			return
		}
		if !o.discarding {
			o.frame(systemDataPayload(o.buffer))
		}
		o.buffer = o.buffer[:0]
		o.discarding = false
		data = data[index+1:]
	}
}

// finish finalizes parsing after the original reader returns an ending error.
// The caller passes any simultaneously returned bytes through consume first,
// allowing a clean EOF fragment to supply the last SSE event or JSON document.
// Other read errors retain prior counters as partial evidence and do not promote
// an unfinished buffer. Nonstreaming completion requires a recognized result
// envelope, not merely valid JSON. Transient buffered content is released before
// the owner applies the observation to its usage row.
func (o *systemUsageObserver) finish(err error) {
	o.ended = true
	if !errors.Is(err, io.EOF) {
		o.truncated = true
	}
	if errors.Is(err, io.EOF) && !o.discarding && len(bytes.TrimSpace(o.buffer)) > 0 {
		if o.sse {
			o.frame(systemDataPayload(o.buffer))
		} else {
			o.frame(o.buffer)
			if gjson.ValidBytes(o.buffer) {
				data := gjson.ParseBytes(o.buffer)
				o.terminal = data.Get("choices").IsArray() || data.Get("content").IsArray() || data.Get("output").IsArray() || data.Get("candidates").IsArray() || data.Get("answers").IsObject() || data.Get("error").IsObject() || (o.image && o.imageCount > 0)
			}
		}
	}
	o.buffer = nil
}

// frame extracts terminal evidence, model identity, and cumulative usage.
// Shared OpenAI, Gemini, and Qoder decoders own their protocol-specific counter
// rules; Messages updates preserve input from message_start through message_delta.
//
// Input and output presence are checked before accepting generic usage so an
// empty usage object cannot become an explicit free request. Errors do not erase
// earlier counters. Gemini's response wrapper is removed before its shared
// decoder runs, and image outputs are counted independently of token presence.
func (o *systemUsageObserver) frame(raw []byte) {
	if len(raw) == 0 {
		return
	}
	if bytes.Equal(raw, []byte("[DONE]")) {
		o.terminal = true
		return
	}
	if !gjson.ValidBytes(raw) {
		o.truncated = true
		return
	}
	data := gjson.ParseBytes(raw)
	typeName := data.Get("type").String()
	if data.Get("error").IsObject() || data.Get("response.error").IsObject() || typeName == "error" || typeName == "response.failed" || typeName == "response.incomplete" {
		o.failed = true
	}
	if typeName == "response.completed" || typeName == "message_stop" || typeName == "image_generation.completed" || typeName == "response.failed" || typeName == "response.incomplete" {
		o.terminal = true
	}
	// The storage field permits sixteen characters. Restricting protocol tier
	// labels to sixteen bytes also fits that bound without storing malformed objects.
	for _, path := range []string{"response.service_tier", "service_tier"} {
		if value := data.Get(path); value.Type == gjson.String && len(value.Str) <= 16 && value.Str != "" {
			o.responseTier = value.Str
			break
		}
	}
	// Accept bounded string metadata only. A malformed model object must not
	// copy response content into the expense record or exceed its database field.
	for _, path := range []string{"model", "response.model", "message.model", "modelVersion"} {
		if value := data.Get(path); value.Type == gjson.String && value.Str != "" {
			o.responseModel = logredact.TruncateUTF8Value(value.Str, 200)
			break
		}
	}
	if o.platform == "qoder" {
		if events, err := qoder.ParseSSELine("data: " + string(raw)); err == nil {
			for _, event := range events {
				if event.HasUsage {
					qoder.MergeQoderUsageEvent(&o.tokens, event)
					o.seenUsage = true
					o.seenInput, o.seenOutput = true, true
				}
				o.terminal = o.terminal || event.IsDone
			}
		}
		return
	}
	if data.Get("response.usageMetadata").Exists() || data.Get("response.candidates").Exists() {
		data = data.Get("response")
		raw = []byte(data.Raw)
	}
	if parsed := gemini.ExtractGeminiUsage(raw); parsed != nil {
		o.tokens = *parsed
		o.seenUsage = true
		o.seenInput = data.Get("usageMetadata.promptTokenCount").Exists()
		o.seenOutput = data.Get("usageMetadata.candidatesTokenCount").Exists()
	}
	data.Get("candidates").ForEach(func(_, candidate gjson.Result) bool {
		if candidate.Get("finishReason").String() != "" {
			o.terminal = true
		}
		candidate.Get("content.parts").ForEach(func(_, part gjson.Result) bool {
			if strings.HasPrefix(part.Get("inlineData.mimeType").String(), "image/") && part.Get("inlineData.data").String() != "" {
				o.imageCount++
			}
			return true
		})
		return true
	})
	if o.messages || typeName == "message_start" || typeName == "message_delta" || typeName == "message" {
		value := data.Get("usage")
		if typeName == "message_start" {
			value = data.Get("message.usage")
		}
		o.messagesUsage(value)
	} else if !data.Get("usageMetadata").Exists() {
		value := data.Get("usage")
		if !value.IsObject() {
			value = data.Get("response.usage")
		}
		if value.Get("input_tokens").Exists() || value.Get("prompt_tokens").Exists() || value.Get("output_tokens").Exists() || value.Get("completion_tokens").Exists() {
			if parsed, ok := openai.ExtractOpenAIUsageFromJSONBytes(raw); ok && systemValidTokenCounts(value) {
				// Sparse output snapshots must preserve the previously observed input
				// and its cache buckets; explicit totals still replace that dimension.
				if value.Get("input_tokens").Exists() || value.Get("prompt_tokens").Exists() {
					o.tokens.InputTokens = parsed.InputTokens
					o.tokens.CacheReadInputTokens = parsed.CacheReadInputTokens
					o.tokens.CacheCreationInputTokens = parsed.CacheCreationInputTokens
					o.imageInput = parsed.ImageInputTokens
					o.seenInput = true
				}
				if value.Get("output_tokens").Exists() || value.Get("completion_tokens").Exists() {
					o.tokens.OutputTokens = parsed.OutputTokens
					o.tokens.ImageOutputTokens = parsed.ImageOutputTokens
					o.seenOutput = true
				}
				o.openAI, o.seenUsage = true, true
			}
		}
	}
	data.Get("choices").ForEach(func(_, choice gjson.Result) bool {
		if choice.Get("finish_reason").String() != "" {
			o.terminal = true
		}
		return true
	})
	if o.image {
		// Complete hosted-image usage replaces the carrier model observation as
		// one unit, matching normal image forwarding without mixing token records.
		if imageUsage, ok := upstreamopenai.OpenAIImagesToolUsageFromGJSON(data.Get("response.tool_usage.image_gen")); ok {
			o.tokens = upstream.TokenUsage{InputTokens: imageUsage.InputTokens, OutputTokens: imageUsage.OutputTokens, ImageOutputTokens: imageUsage.ImageOutputTokens}
			o.imageInput = imageUsage.ImageInputTokens
			o.seenUsage, o.seenInput, o.seenOutput, o.openAI = true, true, true, true
		}
		output := data.Get("response.output")
		if !output.Exists() {
			output = data.Get("output")
		}
		count := 0
		output.ForEach(func(_, item gjson.Result) bool {
			if item.Get("type").String() == "image_generation_call" && item.Get("result").String() != "" {
				count++
			}
			return true
		})
		o.imageCount = max(o.imageCount, count)
	}
	if o.image && data.Get("data").IsArray() {
		o.imageCount = len(data.Get("data").Array())
	}
	if typeName == "image_generation.completed" || typeName == "response.image_generation_call.completed" {
		o.imageCount = max(o.imageCount, 1)
	}
}

// messagesUsage merges Anthropic counters from cumulative stream snapshots.
// Present nonnegative numeric fields replace their individual stored values;
// omitted or unaccepted fields retain the evidence supplied by earlier frames.
// Input from message_start therefore survives an output-only message_delta,
// and repeated output totals are not added together. The two presence flags
// distinguish explicit zero totals from missing dimensions. A reported fast
// speed also supplies the priority billing tier used by shared cost calculation.
func (o *systemUsageObserver) messagesUsage(value gjson.Result) {
	if strings.EqualFold(value.Get("speed").String(), "fast") {
		o.responseTier = "priority"
	}
	for _, field := range []struct {
		path        string // protocol counter field path
		destination *int   // retained normalized counter
	}{
		{"input_tokens", &o.tokens.InputTokens},
		{"output_tokens", &o.tokens.OutputTokens},
		{"cache_creation_input_tokens", &o.tokens.CacheCreationInputTokens},
		{"cache_read_input_tokens", &o.tokens.CacheReadInputTokens},
		{"cache_creation.ephemeral_5m_input_tokens", &o.tokens.CacheCreation5mTokens},
		{"cache_creation.ephemeral_1h_input_tokens", &o.tokens.CacheCreation1hTokens},
	} {
		if counter := value.Get(field.path); counter.Exists() && counter.Type == gjson.Number && counter.Int() >= 0 {
			if field.path == "input_tokens" {
				o.seenInput = true
			}
			if field.path == "output_tokens" {
				o.seenOutput = true
			}
			*field.destination = int(counter.Int())
			o.seenUsage = true
		}
	}
}

// apply transfers measured counters and completeness to the exclusively owned
// row. OpenAI cache counts are removed from inclusive input once, while the
// other protocol decoders already supply disjoint input buckets. A valid terminal
// reply remains successful if the caller cancels afterward; HTTP or protocol
// failures cannot become successful rows even when tokens were observed. Complete
// usage requires both input and output evidence at a terminal response, whereas
// interrupted streams preserve partial counters without implying zero expense.
func (o *systemUsageObserver) apply(row *usage.UsageLog, canceled error) {
	row.InputTokens = max(o.tokens.InputTokens, 0)
	row.OutputTokens = max(o.tokens.OutputTokens, 0)
	row.CacheReadTokens = max(o.tokens.CacheReadInputTokens, 0)
	row.CacheCreationTokens = max(o.tokens.CacheCreationInputTokens, 0)
	if o.openAI {
		// OpenAI reports cache work inside aggregate input. The pricing row uses
		// disjoint buckets so cached input is charged once at its own rate.
		row.InputTokens = max(row.InputTokens-row.CacheReadTokens-row.CacheCreationTokens, 0)
	}
	row.CacheCreation5mTokens = max(o.tokens.CacheCreation5mTokens, 0)
	row.CacheCreation1hTokens = max(o.tokens.CacheCreation1hTokens, 0)
	row.ImageInputTokens = max(o.imageInput, 0)
	row.ImageOutputTokens = max(o.tokens.ImageOutputTokens, 0)
	row.ImageCount = o.imageCount
	if o.responseTier != "" {
		row.ServiceTier = &o.responseTier
	}
	if o.responseModel != "" {
		row.UpstreamResponseModel = &o.responseModel
	}
	if row.SystemUsage.HTTPStatus >= 200 && row.SystemUsage.HTTPStatus < 300 && o.terminal && !o.failed && !o.truncated {
		row.SystemUsage.Outcome = "success"
	} else if canceled != nil {
		row.SystemUsage.Outcome = "canceled"
	}
	if o.seenUsage {
		row.SystemUsage.UsageState = "partial"
		if o.terminal && !o.truncated && o.seenInput && o.seenOutput {
			row.SystemUsage.UsageState = "complete"
		}
	}
}

// systemValidTokenCounts rejects malformed or negative generic token totals.
// A present zero remains valid evidence, while missing dimensions are retained
// separately by the observer and cannot establish complete usage.
func systemValidTokenCounts(value gjson.Result) bool {
	for _, key := range []string{"input_tokens", "prompt_tokens", "output_tokens", "completion_tokens"} {
		counter := value.Get(key)
		if counter.Exists() && (counter.Type != gjson.Number || counter.Int() < 0 || counter.Float() != float64(counter.Int())) {
			return false
		}
	}
	return true
}
