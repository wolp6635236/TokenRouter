package systemone

import (
	"bytes"
	"encoding/json"
	"slices"
)

// references.go matches image placeholders in decoded SystemOne descriptions.
// A request can contain many references beside a large state, so each JSON
// description is decoded once and each string is scanned once for the complete
// reference set. Object keys and nonstring values do not supply images to the
// model. Scans restart at each string boundary to prevent references from
// appearing to span separate array entries or object values.
//
// The matcher uses trie transitions and suffix recovery links. It records
// visited states during scanning and propagates those observations through
// recovery links afterward, instead of reporting every overlapping match at
// every byte. This bounds repeated-prefix work while checking substring
// presence; actual replacement order remains upstream-owned.
// Trie size is bounded by the reference-byte limits checked in payload.go.

// assetReferenceNode records one prefix and the longest recoverable suffix.
type assetReferenceNode struct {
	next      map[byte]int // indexed byte transitions
	failure   int          // longest proper suffix
	reference string       // complete terminal reference
	seen      bool         // observed matching prefix
}

// assetReferences owns a request-local matcher and its accumulated match evidence.
type assetReferences struct {
	nodes []assetReferenceNode // root and prefix states
	// order retains breadth-first state order for reverse suffix propagation.
	// Root is excluded; a state's recovery target always precedes that state.
	order []int
}

// newAssetReferences builds a matcher for previously validated nonempty references.
// Callers enforce the total reference-byte limit before construction and use
// the returned object only within one request. Edges use bytes because JSON
// decoding has already established the UTF-8 spelling compared by the API.
//
// Breadth-first recovery links retain the longest suffix that is another trie
// prefix. A mismatch can therefore continue scanning without restarting the
// input or comparing every reference separately. Terminal references share
// prefixes but keep their own match evidence for the final missing check.
func newAssetReferences(assets map[string]Asset) *assetReferences {
	matcher := &assetReferences{nodes: []assetReferenceNode{{}}}
	for reference := range assets {
		state := 0
		for index := range len(reference) {
			value := reference[index]
			if matcher.nodes[state].next == nil {
				matcher.nodes[state].next = map[byte]int{}
			}
			next := matcher.nodes[state].next[value]
			if next == 0 {
				next = len(matcher.nodes)
				matcher.nodes[state].next[value] = next
				matcher.nodes = append(matcher.nodes, assetReferenceNode{})
			}
			state = next
		}
		matcher.nodes[state].reference = reference
	}
	for _, state := range matcher.nodes[0].next {
		matcher.order = append(matcher.order, state)
	}
	for index := 0; index < len(matcher.order); index++ {
		state := matcher.order[index]
		for value, child := range matcher.nodes[state].next {
			fallback := matcher.nodes[state].failure
			// Recover through shorter suffixes until this byte extends a known
			// prefix; the root transition also covers a new match beginning here.
			for fallback != 0 && matcher.nodes[fallback].next[value] == 0 {
				fallback = matcher.nodes[fallback].failure
			}
			matcher.nodes[child].failure = matcher.nodes[fallback].next[value]
			matcher.order = append(matcher.order, child)
		}
	}
	return matcher
}

// scan decodes one validated description and records all referenced images.
// Omitted descriptions contribute nothing. Numbers remain opaque json.Number
// values so an otherwise valid large number cannot discard neighboring strings
// through float64 overflow. The decoded description is released after scanning.
func (m *assetReferences) scan(raw json.RawMessage) {
	if len(raw) == 0 {
		return
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		// Payload parsing validates descriptions before this call. Manually
		// constructed invalid descriptions contribute no evidence toward asset usage.
		return
	}
	m.scanValue(value)
}

// scanValue visits string leaves without treating object keys as asset content.
// Each string starts at the root, while seen states accumulate across values.
// Recovery links consume mismatches without rescanning earlier input bytes;
// suffix match evidence is propagated once by missing after all scans finish.
// An object key can equal a reference without contributing any evidence, and
// neighboring string values cannot combine into a reference. Recording states
// rather than match lists keeps repeated occurrences from allocating output.
func (m *assetReferences) scanValue(value any) {
	switch value := value.(type) {
	case string:
		state := 0
		for index := range len(value) {
			character := value[index]
			// Follow suffix links on mismatch so repeated prefix-like content does
			// not make every reference retry its full search over the same string.
			for state != 0 && m.nodes[state].next[character] == 0 {
				state = m.nodes[state].failure
			}
			state = m.nodes[state].next[character]
			m.nodes[state].seen = true
		}
	case []any:
		for _, child := range value {
			m.scanValue(child)
		}
	case map[string]any:
		for _, child := range value {
			m.scanValue(child)
		}
	}
}

// missing reports one reference absent from all previously scanned descriptions.
// Call it after scanning finishes. Reverse breadth-first propagation marks
// terminal suffixes without enumerating all overlapping matches at each input
// byte; a missing reference returns true, including references seen only in keys.
// Recovery targets always represent shorter prefixes, so reversing construction
// order transfers all descendant evidence before inspecting each terminal.
// Successful coverage returns an empty string and false, without retaining
// any decoded request content in the matcher.
func (m *assetReferences) missing() (string, bool) {
	for _, state := range slices.Backward(m.order) {
		if m.nodes[state].seen {
			m.nodes[m.nodes[state].failure].seen = true
		}
	}
	for _, node := range m.nodes {
		if node.reference != "" && !node.seen {
			return node.reference, true
		}
	}
	return "", false
}
