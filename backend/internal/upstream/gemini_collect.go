package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
)

// gemini_collect.go assembles validated Gemini streams for buffered clients.
// Candidate indices own independent content and metadata, so interleaved streams
// cannot replace an earlier candidate when a later candidate finishes. Unknown
// object fields survive collection, and adjacent text parts merge only when all
// their other metadata agrees. This preserves thought signatures and vendor
// fields that would otherwise be lost by merging text alone.
//
// The shared reader owns transport completion and deadlines. Collection retains
// partial response state beside an error without presenting it as successful JSON.
// Sparse usage comes from that reader's accumulated snapshot, independently of
// whichever candidate carried the latest text. Repeated terminal candidates may
// update metadata but cannot append already completed content again.

// geminiCollectedCandidate owns one candidate's accumulated content and final metadata.
type geminiCollectedCandidate struct {
	value    map[string]any // candidate response fields
	parts    []any          // ordered generated parts
	finished bool           // candidate completion observed
}

// CollectGeminiStream builds a response while preserving reader failure and partial usage.
// The observer receives validated payloads synchronously before collection, and
// may be nil. The shared reader closes the response and enforces the supplied limits.
//
// Numbers retain their JSON spelling, and candidate maps remain indexed until
// final assembly rather than following arrival order. Content-bearing and terminal
// frames can be separated by usage-only tails without dropping candidate data.
// Failed reads still return the accumulated map for diagnostics; callers must
// check the error before writing any successful buffered client representation.
func CollectGeminiStream(ctx context.Context, resp *http.Response, options GeminiStreamOptions, observe func([]byte)) (map[string]any, GeminiStreamResult, error) {
	response := make(map[string]any)
	candidates := make(map[int]*geminiCollectedCandidate)
	result, err := ReadGeminiStream(ctx, resp, options, func(event GeminiStreamEvent) error {
		if len(event.Data) == 0 {
			return nil
		}
		if observe != nil {
			observe(event.Data)
		}
		var payload map[string]any
		decoder := json.NewDecoder(bytes.NewReader(event.Data))
		decoder.UseNumber()
		if err := decoder.Decode(&payload); err != nil {
			return err
		}
		for key, value := range payload {
			if key != "candidates" {
				response[key] = value
			}
		}
		values, _ := payload["candidates"].([]any)
		for position, value := range values {
			candidate, ok := value.(map[string]any)
			if !ok {
				continue
			}
			index := position
			if number, ok := candidate["index"].(json.Number); ok {
				if parsed, err := number.Int64(); err == nil && parsed >= 0 {
					index = int(parsed)
				}
			}
			collected := candidates[index]
			if collected == nil {
				collected = &geminiCollectedCandidate{value: make(map[string]any)}
				candidates[index] = collected
			}
			collected.add(candidate)
		}
		return nil
	})
	if len(candidates) > 0 {
		indices := make([]int, 0, len(candidates))
		for index := range candidates {
			indices = append(indices, index)
		}
		slices.Sort(indices)
		values := make([]any, 0, len(indices))
		for _, index := range indices {
			values = append(values, candidates[index].value)
		}
		response["candidates"] = values
	}
	if len(result.UsageJSON) > 0 {
		var usage map[string]json.RawMessage
		if json.Unmarshal(result.UsageJSON, &usage) == nil {
			response["usageMetadata"] = usage["usageMetadata"]
		}
	}
	return response, result, err
}

// add merges one indexed candidate without losing unknown content metadata.
// Parts remain ordered, and previously completed candidates accept metadata only.
// Text concatenation requires identical remaining fields so distinct signatures survive.
// Incoming maps are private decoded frames, while the collector owns its retained
// candidate containers. Terminal metadata can arrive after earlier content, and
// later usage frames contain no candidate at all. Preserving the old content map
// across both cases avoids losing completed candidates during final JSON assembly.
func (c *geminiCollectedCandidate) add(candidate map[string]any) {
	for key, value := range candidate {
		if key != "content" {
			c.value[key] = value
		}
	}
	if incoming, ok := candidate["content"].(map[string]any); ok {
		content, _ := c.value["content"].(map[string]any)
		if content == nil {
			content = make(map[string]any)
			c.value["content"] = content
		}
		for key, value := range incoming {
			if key != "parts" {
				content[key] = value
			}
		}
		if !c.finished {
			parts, _ := incoming["parts"].([]any)
			for _, part := range parts {
				if len(c.parts) > 0 && mergeGeminiCollectedText(c.parts[len(c.parts)-1], part) {
					continue
				}
				c.parts = append(c.parts, part)
			}
		}
		content["parts"] = c.parts
	}
	if finish, ok := candidate["finishReason"].(string); ok && finish != "" && finish != "FINISH_REASON_UNSPECIFIED" {
		c.finished = true
	}
}

// mergeGeminiCollectedText joins adjacent text only when every metadata field agrees.
// Both values must be JSON object parts with string text. The previous part is
// collector-owned and is changed in place only after metadata comparison succeeds.
// Missing metadata is distinct from an explicit false or null value, preventing
// the merge from erasing a field supplied by either part. Nested metadata uses
// structural equality because decoded maps do not have stable field order.
// Failed comparisons retain both original parts for lossless buffered presentation.
func mergeGeminiCollectedText(previous, next any) bool {
	a, ok := previous.(map[string]any)
	if !ok {
		return false
	}
	b, ok := next.(map[string]any)
	if !ok || len(a) != len(b) {
		return false
	}
	first, ok := a["text"].(string)
	if !ok {
		return false
	}
	second, ok := b["text"].(string)
	if !ok {
		return false
	}
	for key, value := range a {
		if key != "text" && !reflect.DeepEqual(value, b[key]) {
			return false
		}
	}
	a["text"] = first + second
	return true
}
