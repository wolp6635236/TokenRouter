package qoder

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"html"
	"strings"
)

// stream_tool_text.go converts XML, JSON, and DSML tool invocations embedded in
// visible text into structured Qoder events. Opening markers and call segments
// can span adjacent text events, so the transformer retains unfinished suffixes
// until later text completes them or a non-text event flushes them. Text
// outside recognized calls remains available to the protocol writers.
//
// Complete segments use their original bytes and invocation position when
// generating call identities. Failed recognition releases the segment literally,
// including a DSML wrapper whose earlier children have already advanced the
// counter. Direct normalization maps supported shell names and arguments before
// the call accumulator combines fragments for client output. Unchanged events
// retain their borrowed usage-detail references.

const (
	// XML marker constants identify text-encoded calls and named argument tags. The
	// transformer retains possible opening fragments until a complete segment or
	// final flush resolves them.
	QoderTextToolCallStart = "<tool_call>"
	QoderTextToolCallEnd   = "</tool_call>"
	QoderTextArgKeyStart   = "<arg_key>"
	QoderTextArgKeyEnd     = "</arg_key>"
	QoderTextArgValueStart = "<arg_value>"
	QoderTextArgValueEnd   = "</arg_value>"

	// DSML marker constants delimit invocation and parameter wrappers in text
	// output. The parser entity-decodes named parameters and emits calls with
	// generated sequence identities.
	QoderDSMLToolCallsStart = "<｜｜DSML｜｜tool_calls>"
	QoderDSMLToolCallsEnd   = "</｜｜DSML｜｜tool_calls>"
	QoderDSMLInvokeStart    = "<｜｜DSML｜｜invoke"
	QoderDSMLInvokeEnd      = "</｜｜DSML｜｜invoke>"
	QoderDSMLParameterStart = "<｜｜DSML｜｜parameter"
	QoderDSMLParameterEnd   = "</｜｜DSML｜｜parameter>"
)

// QoderTextToolCallTransformer buffers possible call markup across adjacent text
// events.
type QoderTextToolCallTransformer struct {
	// buffer holds possible opening-tag suffixes and incomplete segments across
	// adjacent text events. Non-text input and final flush release unfinished text
	// literally.
	buffer       string // pending text suffix
	nextToolCall int    // next invocation position
}

// NewQoderTextToolCallTransformer creates an empty buffer and invocation counter
// for one event sequence.
func NewQoderTextToolCallTransformer() *QoderTextToolCallTransformer {
	return &QoderTextToolCallTransformer{}
}

// NormalizeQoderTextToolCallEvents converts text-encoded calls across an ordered
// event slice and flushes its tail. Empty input is returned as supplied; new
// output events retain shared usage-detail references from unchanged events.
func NormalizeQoderTextToolCallEvents(events []SSEEvent) []SSEEvent {
	if len(events) == 0 {
		return events
	}
	transformer := NewQoderTextToolCallTransformer()
	out := make([]SSEEvent, 0, len(events))
	for _, event := range events {
		out = append(out, transformer.Append(event)...)
	}
	out = append(out, transformer.Flush()...)
	return out
}

// Append buffers text deltas and returns complete text or tool-call observations
// available so far. Nil transformer returns the supplied event, and an empty
// text delta contributes no output. Nonempty text extends the held suffix before
// drain examines complete markers.
//
// Non-text input first releases buffered text using final handling and then
// follows it with that event. A reasoning, usage, or structured call event
// therefore cannot complete a marker begun on the visible text channel.
func (t *QoderTextToolCallTransformer) Append(event SSEEvent) []SSEEvent {
	if t == nil {
		return []SSEEvent{event}
	}
	if event.Type == "text_delta" {
		if event.Text == "" {
			return nil
		}
		t.buffer += event.Text
		return t.drain(false)
	}
	out := t.drain(true)
	out = append(out, event)
	return out
}

// Flush emits any remaining buffered text and complete calls at an
// event-sequence end. Nil transformer returns nil; an incomplete or unparseable
// call segment becomes literal text through drain's final handling.
func (t *QoderTextToolCallTransformer) Flush() []SSEEvent {
	if t == nil {
		return nil
	}
	return t.drain(true)
}

// drain emits safe text and complete tool calls while retaining a possible
// unfinished marker. A trailing marker prefix stays buffered when final is
// false, so a later text delta can complete its opening tag. Text before a full
// marker is emitted immediately.
//
// Complete segments are parsed using their marker format. Failed parses remain
// literal text, and final processing also releases unfinished segments. This
// keeps visible input available when tool recognition fails while avoiding
// premature emission of a marker split across adjacent text events.
func (t *QoderTextToolCallTransformer) drain(final bool) []SSEEvent {
	out := make([]SSEEvent, 0)
	for t.buffer != "" {
		start, marker := QoderTextToolCallMarkerStart(t.buffer)
		if start < 0 {
			if final {
				out = append(out, SSEEvent{Type: "text_delta", Text: t.buffer})
				t.buffer = ""
				return out
			}
			// A trailing opening-tag prefix can continue in the next text event. Emit the
			// preceding text now and retain only that suffix to avoid leaking split
			// markup.
			suffixLen := QoderToolCallStartSuffixLen(t.buffer)
			text := t.buffer
			if suffixLen > 0 {
				text = t.buffer[:len(t.buffer)-suffixLen]
				t.buffer = t.buffer[len(t.buffer)-suffixLen:]
			} else {
				t.buffer = ""
			}
			if text != "" {
				out = append(out, SSEEvent{Type: "text_delta", Text: text})
			}
			return out
		}
		if start > 0 {
			out = append(out, SSEEvent{Type: "text_delta", Text: t.buffer[:start]})
			t.buffer = t.buffer[start:]
			continue
		}

		startTag, endTag := QoderTextToolCallMarkerTags(marker)
		end := strings.Index(t.buffer[len(startTag):], endTag)
		if end < 0 {
			if final {
				out = append(out, SSEEvent{Type: "text_delta", Text: t.buffer})
				t.buffer = ""
			}
			return out
		}
		segmentEnd := len(startTag) + end + len(endTag)
		segment := t.buffer[:segmentEnd]
		// Recognition failure leaves the complete segment as literal visible text.
		// This keeps unsupported or malformed markup available instead of silently
		// discarding user output.
		if events, ok := t.parseToolCallSegment(segment, marker); ok {
			out = append(out, events...)
		} else {
			out = append(out, SSEEvent{Type: "text_delta", Text: segment})
		}
		t.buffer = t.buffer[segmentEnd:]
	}
	return out
}

// QoderTextToolCallMarkerStart returns the earliest XML or DSML call marker
// position and format. Missing markers report a negative position; XML wins when
// it occurs before the DSML marker.
func QoderTextToolCallMarkerStart(text string) (int, string) {
	xmlStart := strings.Index(text, QoderTextToolCallStart)
	dsmlStart := strings.Index(text, QoderDSMLToolCallsStart)
	if xmlStart < 0 {
		return dsmlStart, QoderDSMLToolCallsStart
	}
	if dsmlStart < 0 || xmlStart < dsmlStart {
		return xmlStart, QoderTextToolCallStart
	}
	return dsmlStart, QoderDSMLToolCallsStart
}

// QoderTextToolCallMarkerTags returns the matching DSML tag pair for its
// supported marker. Every other marker selects the XML pair, letting drain use
// one extraction sequence for either format.
func QoderTextToolCallMarkerTags(marker string) (string, string) {
	if marker == QoderDSMLToolCallsStart {
		return QoderDSMLToolCallsStart, QoderDSMLToolCallsEnd
	}
	return QoderTextToolCallStart, QoderTextToolCallEnd
}

// QoderToolCallStartSuffixLen returns the longest trailing fragment that
// prefixes a supported opening marker. It examines XML and DSML markers and
// excludes complete opening tags, which drain handles as full segments. Zero
// means no suffix needs to remain buffered.
//
// The longest match retains all bytes that could continue into a marker on the
// next visible-text event. Shorter matches could release part of that opening
// sequence as ordinary text and prevent later recognition. The caller emits
// the preceding text and holds this suffix until continuation or final flush.
func QoderToolCallStartSuffixLen(text string) int {
	maxSuffix := 0
	for _, marker := range []string{QoderTextToolCallStart, QoderDSMLToolCallsStart} {
		limit := min(len(marker)-1, len(text))
		for i := limit; i > 0; i-- {
			if strings.HasPrefix(marker, text[len(text)-i:]) && i > maxSuffix {
				maxSuffix = i
			}
		}
	}
	return maxSuffix
}

// parseToolCallSegment dispatches one complete text segment to DSML or XML/JSON
// recognition. Failed recognition reports false so drain can emit the complete
// segment as visible text.
func (t *QoderTextToolCallTransformer) parseToolCallSegment(segment string, marker string) ([]SSEEvent, bool) {
	if marker == QoderDSMLToolCallsStart {
		return t.parseDSMLToolCalls(segment)
	}
	event, ok := t.parseToolCall(segment)
	if !ok {
		return nil, false
	}
	return []SSEEvent{event}, true
}

// parseToolCall converts one XML-style segment, trying embedded JSON recognition
// first. If JSON recognition fails, the text before argument tags becomes the
// candidate name. Blank names and incomplete key/value tags report false so
// drain can retain the segment as literal output.
//
// Argument keys and values are entity-decoded into a JSON object, with later
// repeated keys replacing earlier values. Successful recognition assigns the
// next invocation index and a segment-based ID before advancing the counter;
// failed XML recognition leaves that counter unchanged.
func (t *QoderTextToolCallTransformer) parseToolCall(segment string) (SSEEvent, bool) {
	inner := strings.TrimPrefix(segment, QoderTextToolCallStart)
	inner = strings.TrimSuffix(inner, QoderTextToolCallEnd)
	inner = strings.TrimSpace(inner)
	if inner == "" {
		return SSEEvent{}, false
	}
	// Text markers can hold JSON invocations or XML-style names and arguments. Try
	// JSON first so a valid serialized object cannot become a function name.
	if event, ok := t.parseJSONToolCall(inner, segment); ok {
		return event, true
	}

	firstArgTag := len(inner)
	for _, tag := range []string{QoderTextArgValueStart, QoderTextArgKeyStart} {
		if idx := strings.Index(inner, tag); idx >= 0 && idx < firstArgTag {
			firstArgTag = idx
		}
	}
	name := strings.TrimSpace(html.UnescapeString(inner[:firstArgTag]))
	if name == "" {
		return SSEEvent{}, false
	}

	args := map[string]any{}
	rest := inner[firstArgTag:]
	for {
		keyOpen := strings.Index(rest, QoderTextArgKeyStart)
		if keyOpen < 0 {
			break
		}
		keyStart := keyOpen + len(QoderTextArgKeyStart)
		keyEnd := strings.Index(rest[keyStart:], QoderTextArgKeyEnd)
		if keyEnd < 0 {
			return SSEEvent{}, false
		}
		key := strings.TrimSpace(html.UnescapeString(rest[keyStart : keyStart+keyEnd]))
		afterKey := rest[keyStart+keyEnd+len(QoderTextArgKeyEnd):]

		valueOpen := strings.Index(afterKey, QoderTextArgValueStart)
		if valueOpen < 0 {
			return SSEEvent{}, false
		}
		valueStart := valueOpen + len(QoderTextArgValueStart)
		valueEnd := strings.Index(afterKey[valueStart:], QoderTextArgValueEnd)
		if valueEnd < 0 {
			return SSEEvent{}, false
		}
		if key != "" {
			args[key] = html.UnescapeString(afterKey[valueStart : valueStart+valueEnd])
		}
		rest = afterKey[valueStart+valueEnd+len(QoderTextArgValueEnd):]
	}

	arguments, err := json.Marshal(args)
	if err != nil {
		return SSEEvent{}, false
	}
	index := t.nextToolCall
	id := QoderTextToolCallID(index, segment)
	t.nextToolCall++
	return SSEEvent{
		Type:             "tool_call_delta",
		ToolCallID:       id,
		ToolCallIndex:    index,
		HasToolCallIndex: true,
		ToolType:         "function",
		ToolName:         name,
		Arguments:        string(arguments),
	}, true
}

// parseJSONToolCall converts a named JSON invocation inside a text-call segment.
// The inner text is entity-decoded before JSON parsing. Nested function fields
// precede flat name and argument aliases, while inline command fields can supply
// otherwise absent arguments.
//
// A supplied ID precedes the generated segment-based identity. Name/argument
// normalization maps supported shell forms before assigning the next invocation
// index. Only successful named recognition advances the counter, and missing
// arguments use an empty object representation for later assembly.
func (t *QoderTextToolCallTransformer) parseJSONToolCall(inner string, segment string) (SSEEvent, bool) {
	decodedText := strings.TrimSpace(html.UnescapeString(inner))
	if !strings.HasPrefix(decodedText, "{") {
		return SSEEvent{}, false
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(decodedText), &payload); err != nil {
		return SSEEvent{}, false
	}
	function, _ := payload["function"].(map[string]any)
	name := FirstNonEmptyQoder(
		QoderStringField(function, "name"),
		QoderStringField(payload, "name"),
		QoderStringField(payload, "tool"),
		QoderStringField(payload, "tool_name"),
	)
	if name == "" {
		return SSEEvent{}, false
	}
	arguments := FirstPresentQoderToolArguments(
		function["arguments"],
		payload["arguments"],
		payload["input"],
		payload["parameters"],
		QoderInlineToolArguments(payload),
	)
	name, arguments = NormalizeQoderTextToolCallNameAndArguments(name, arguments)
	index := t.nextToolCall
	id := FirstNonEmptyQoder(
		QoderStringField(payload, "id"),
		QoderStringField(payload, "tool_call_id"),
		QoderStringField(payload, "call_id"),
		QoderTextToolCallID(index, segment),
	)
	t.nextToolCall++
	return SSEEvent{
		Type:             "tool_call_delta",
		ToolCallID:       id,
		ToolCallIndex:    index,
		HasToolCallIndex: true,
		ToolType:         FirstNonEmptyQoder(QoderStringField(payload, "type"), "function"),
		ToolName:         name,
		Arguments:        arguments,
	}, true
}

// FirstPresentQoderToolArguments returns the first nonblank serialized argument
// candidate in order. Nil and blank candidates are skipped; no usable input
// returns an empty JSON object string for call construction.
func FirstPresentQoderToolArguments(values ...any) string {
	for _, value := range values {
		if value == nil {
			continue
		}
		arguments := QoderToolArgumentsString(value)
		if strings.TrimSpace(arguments) != "" {
			return arguments
		}
	}
	return "{}"
}

// QoderInlineToolArguments copies recognized command, cmd, and description
// fields into an argument object. No matching field returns nil so JSON call
// parsing can consider that candidate absent.
func QoderInlineToolArguments(payload map[string]any) any {
	if len(payload) == 0 {
		return nil
	}
	args := map[string]any{}
	for _, key := range []string{"command", "cmd", "description"} {
		if value, ok := payload[key]; ok {
			args[key] = value
		}
	}
	if len(args) == 0 {
		return nil
	}
	return args
}

// NormalizeQoderTextToolCallNameAndArguments normalizes shell names and their
// argument representation for client output. Shell and execute_bash become Bash;
// other names retain trimmed spelling and supplied arguments.
//
// Bash object arguments rename cmd to command only when command is absent.
// Non-JSON command text becomes a command object, while malformed
// object/array-looking text remains available for protocol validation. This
// avoids treating an incomplete structured argument as a literal shell command
// while supporting text command variants.
func NormalizeQoderTextToolCallNameAndArguments(name string, arguments string) (string, string) {
	originalName := strings.TrimSpace(name)
	normalizedName := originalName
	if strings.EqualFold(originalName, "shell") || strings.EqualFold(originalName, "execute_bash") {
		normalizedName = "Bash"
	}
	if normalizedName != "Bash" {
		return normalizedName, arguments
	}
	args := map[string]any{}
	// Shell arguments can be plain command text or structured JSON. A malformed
	// object-looking fragment stays available for validation, while plain command
	// text gains a command field.
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		command := strings.TrimSpace(arguments)
		if command == "" || command == "{}" {
			return normalizedName, "{}"
		}
		if strings.HasPrefix(command, "{") || strings.HasPrefix(command, "[") {
			return normalizedName, arguments
		}
		// The replacement contains a string-valued command field, which JSON can
		// encode. Return that object for plain command text after structured parsing
		// failed.
		body, _ := json.Marshal(map[string]any{"command": command})
		return normalizedName, string(body)
	}
	if _, ok := args["command"]; !ok {
		if cmd, ok := args["cmd"]; ok {
			args["command"] = cmd
			delete(args, "cmd")
		}
	}
	body, err := json.Marshal(args)
	if err != nil {
		return normalizedName, arguments
	}
	return normalizedName, string(body)
}

// parseDSMLToolCalls scans a complete DSML wrapper for invocation segments and
// returns recognized calls. Malformed opening or closing invocation tags report
// false. Individually unusable invocations are skipped, and at least one
// recognized call is needed for success.
//
// Child recognition assigns sequence indices as it succeeds. A later malformed
// wrapper fragment can therefore reject the segment after earlier invocations
// have advanced the counter; drain then emits the segment literally. Callers
// process a segment once instead of assuming failed wrapper recognition leaves
// transformer state unchanged.
func (t *QoderTextToolCallTransformer) parseDSMLToolCalls(segment string) ([]SSEEvent, bool) {
	inner := strings.TrimPrefix(segment, QoderDSMLToolCallsStart)
	inner = strings.TrimSuffix(inner, QoderDSMLToolCallsEnd)
	inner = strings.TrimSpace(inner)
	if inner == "" {
		return nil, false
	}
	events := make([]SSEEvent, 0)
	rest := inner
	for {
		start := strings.Index(rest, QoderDSMLInvokeStart)
		if start < 0 {
			break
		}
		rest = rest[start:]
		openEnd := strings.Index(rest, ">")
		if openEnd < 0 {
			return nil, false
		}
		closeStart := strings.Index(rest[openEnd+1:], QoderDSMLInvokeEnd)
		if closeStart < 0 {
			return nil, false
		}
		openTag := rest[:openEnd+1]
		body := rest[openEnd+1 : openEnd+1+closeStart]
		// A framed wrapper can contain invocations lacking names or usable parameters.
		// Skip those child records so later recognizable calls can still contribute
		// events from this segment.
		if event, ok := t.parseDSMLInvoke(openTag, body, segment); ok {
			events = append(events, event)
		}
		rest = rest[openEnd+1+closeStart+len(QoderDSMLInvokeEnd):]
	}
	return events, len(events) > 0
}

// parseDSMLInvoke converts a named invocation with named parameter tags into one
// function event. The invocation needs a double-quoted name and at least one
// parsed argument. Named parameter values are entity-decoded as strings, and
// malformed parameter tags reject the invocation.
//
// Arguments are marshaled into JSON before shell normalization and sequence
// assignment. Successful recognition advances the synthetic index and derives
// its ID from that index plus the wrapper segment, keeping multiple invocations
// inside the same DSML text distinguishable.
func (t *QoderTextToolCallTransformer) parseDSMLInvoke(openTag string, body string, segment string) (SSEEvent, bool) {
	name := QoderTagAttribute(openTag, "name")
	if name == "" {
		return SSEEvent{}, false
	}
	args := map[string]any{}
	rest := body
	for {
		start := strings.Index(rest, QoderDSMLParameterStart)
		if start < 0 {
			break
		}
		rest = rest[start:]
		openEnd := strings.Index(rest, ">")
		if openEnd < 0 {
			return SSEEvent{}, false
		}
		closeStart := strings.Index(rest[openEnd+1:], QoderDSMLParameterEnd)
		if closeStart < 0 {
			return SSEEvent{}, false
		}
		openParam := rest[:openEnd+1]
		paramName := QoderTagAttribute(openParam, "name")
		if paramName != "" {
			args[paramName] = html.UnescapeString(rest[openEnd+1 : openEnd+1+closeStart])
		}
		rest = rest[openEnd+1+closeStart+len(QoderDSMLParameterEnd):]
	}
	if len(args) == 0 {
		return SSEEvent{}, false
	}
	arguments, err := json.Marshal(args)
	if err != nil {
		return SSEEvent{}, false
	}
	name, normalizedArguments := NormalizeQoderTextToolCallNameAndArguments(name, string(arguments))
	index := t.nextToolCall
	t.nextToolCall++
	return SSEEvent{
		Type:             "tool_call_delta",
		ToolCallID:       QoderTextToolCallID(index, segment),
		ToolCallIndex:    index,
		HasToolCallIndex: true,
		ToolType:         "function",
		ToolName:         name,
		Arguments:        normalizedArguments,
	}, true
}

// QoderTagAttribute reads and entity-decodes a double-quoted named attribute
// from a tag. Missing opening or closing quote returns empty text, which the
// DSML parser treats as absent identity.
func QoderTagAttribute(tag string, name string) string {
	pattern := name + `="`
	start := strings.Index(tag, pattern)
	if start < 0 {
		return ""
	}
	start += len(pattern)
	end := strings.Index(tag[start:], `"`)
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(html.UnescapeString(tag[start : start+end]))
}

// NormalizeQoderOutboundToolCallEvent applies shell name/argument normalization
// when a tool name is available. Unnamed fragments remain supplied so the
// accumulator can associate them using ID, index, or a single existing call.
func NormalizeQoderOutboundToolCallEvent(event SSEEvent) SSEEvent {
	if strings.TrimSpace(event.ToolName) == "" {
		return event
	}
	event.ToolName, event.Arguments = NormalizeQoderTextToolCallNameAndArguments(event.ToolName, event.Arguments)
	return event
}

// QoderTextToolCallID derives a call identifier from invocation index and
// segment bytes. The index separates equal segments within one sequence, and the
// first twelve SHA-1 bytes produce its hexadecimal suffix.
func QoderTextToolCallID(index int, segment string) string {
	sum := sha1.Sum([]byte(fmt.Sprintf("%d\n%s", index, segment)))
	// Twelve digest bytes supply the call identifier's hexadecimal suffix.
	return fmt.Sprintf("call_%x", sum[:12])
}
