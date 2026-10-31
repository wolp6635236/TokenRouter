package systemone

import (
	"encoding/json"
	"fmt"
	"maps"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestModerationBodyDecodesDescriptions supplies strings, nested descriptions,
// escaped keys, large numbers, and literal backslashes. Moderation output must
// contain each asserted decoded token, while the literal-escape fixture must
// exclude the independently decoded word blocked.
func TestModerationBodyDecodesDescriptions(t *testing.T) {
	for _, tc := range []struct {
		// name labels the fixture; state and questions supply their respective
		// raw JSON descriptions for the request constructed by this subtest.
		name, state, questions string   // named test fixture, raw state JSON, and raw question JSON
		want                   []string // expected decoded text
	}{
		{name: "string", state: `"\u0062locked"`, want: []string{"blocked"}},
		{name: "object", state: `{"\u006bey":{"text":"\u0062locked","number":9007199254740993,"flag":true}}`, want: []string{"key", "blocked", "9007199254740993", "true"}},
		{name: "array", state: `["\u0062locked",["\u4e2d\u6587"]]`, want: []string{"blocked", "中文"}},
		{name: "escaped punctuation", state: `"\u003cscript\u003e\nline\t\"quoted\""`, want: []string{"<script>\nline\t\"quoted\""}},
		{name: "literal escape", state: `"\\u0062locked"`, want: []string{`\u0062locked`}},
		{name: "instructions", questions: `{"a":{"type":"noul","instructions":{"\u0061sk":["\u0062locked"]}}}`, want: []string{"ask", "blocked"}},
		{name: "noul criteria", questions: `{"a":{"type":"noul","instructions":"ok","criteria":{"true":"\u0062locked","false":["\u4e2d\u6587"]}}}`, want: []string{"blocked", "中文"}},
		{name: "choice criteria", questions: `{"a":{"type":"choice","instructions":"ok","criteria":{"\u0062locked":null,"other":{"text":"\u4e2d\u6587"}}}}`, want: []string{"blocked", "中文"}},
		{name: "score criteria", questions: `{"a":{"type":"score","instructions":"ok","criteria":["\u0062locked",["\u4e2d\u6587"]]}}`, want: []string{"blocked", "中文"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, questions := tc.state, tc.questions
			if state == "" {
				state = `"ok"`
			}
			if questions == "" {
				questions = `{"a":{"type":"noul","instructions":"ok"}}`
			}
			request, err := ParseRequest([]byte(fmt.Sprintf(`{"model":"jev-latest","state":%s,"questions":%s}`, state, questions)))
			require.NoError(t, err)
			var body struct {
				Messages []struct {
					Content string /* decoded moderation text */
				} `json:"messages"` // generated user messages
			}
			require.NoError(t, json.Unmarshal(request.ModerationBody(), &body))
			require.Len(t, body.Messages, 1)
			for _, want := range tc.want {
				require.Contains(t, body.Messages[0].Content, want)
			}
			if tc.name == "literal escape" {
				require.NotContains(t, body.Messages[0].Content, "blocked")
			}
		})
	}
}

// TestCodecPreservesMixedQuestions parses noul, choice, score, and box questions,
// patches their model, and reads a valid mixed response. Assertions protect
// the question count, large numeric/extension bytes, a moderation key, and
// accepted usage with the supplied input count.
func TestCodecPreservesMixedQuestions(t *testing.T) {
	body := []byte(`{"model":"alias","state":{"message":"hello","number":9007199254740993},"extension":{"keep":true},"questions":{"a":{"type":"noul","instructions":["yes?"]},"b":{"type":"choice","instructions":{"ask":"pick"},"criteria":{"one":null,"two":{"hint":"second"}}},"c":{"type":"score","instructions":"rank","criteria":["low",["high"]]},"d":{"type":"box","instructions":"<photo>"}},"assets":{"<photo>":{"format":"png","data_base64":"aW1hZ2U="}}}`)
	r, err := ParseRequest(body)
	require.NoError(t, err)
	require.Len(t, r.Questions, 4)
	mapped, err := ReplaceModel(body, "jev-latest")
	require.NoError(t, err)
	require.Contains(t, string(mapped), `9007199254740993`)
	require.Contains(t, string(mapped), `"extension":{"keep":true}`)
	require.Contains(t, string(r.ModerationBody()), "message")
	response := []byte(`{"model":"jev-1.14.0","answers":{"a":{"type":"noul","noul":0.9},"b":{"type":"choice","choice":"one","confidence":0.8,"probabilities":{"one":0.9,"two":0.1}},"c":{"type":"score","score":0.1,"confidence":0.8,"probabilities":{"0":0.9,"1":0.1},"legend":{"0":"low","1":"high"}},"d":{"type":"box","box":{"top":0,"bottom":1,"left":0,"right":1},"confidence":null}},"usage":{"input_tokens":10,"output_tokens":0}}`)
	decoded, err := ParseResponse(response, r.Questions)
	require.NoError(t, err)
	require.True(t, decoded.HasUsage)
	require.Equal(t, 10, decoded.Usage.InputTokens)
}

// TestResponseUsageValidity supplies valid answers beside missing, malformed,
// negative, out-of-range, or explicit zero usage. Only the valid zero fixture
// establishes HasUsage, while invalid model/answer fixtures must return an error.
func TestResponseUsageValidity(t *testing.T) {
	questions := map[string]Question{"a": {Type: "noul"}}
	for _, usage := range []string{`null`, `{}`, `{"input_tokens":-1,"output_tokens":0}`, `{"input_tokens":"10","output_tokens":0}`, `{"input_tokens":1.5,"output_tokens":0}`, `{"input_tokens":9223372036854775808,"output_tokens":0}`, `{"input_tokens":0,"output_tokens":0}`} {
		t.Run(usage, func(t *testing.T) {
			body := []byte(`{"model":"jev","answers":{"a":{"type":"noul","noul":0}},"usage":` + usage + `}`)
			r, err := ParseResponse(body, questions)
			require.NoError(t, err)
			require.Equal(t, usage == `{"input_tokens":0,"output_tokens":0}`, r.HasUsage)
		})
	}
	for _, body := range []string{`null`, `{"model":"jev","answers":{}}`, `{"model":"jev","answers":{"a":{"type":"noul","noul":2}}}`} {
		_, err := ParseResponse([]byte(body), questions)
		require.Error(t, err)
	}
}

// TestParseRequestRejectsUnsupportedShapes replaces selected fields of a valid
// availability probe with unsupported state, stream, model, or question values.
// Each altered fixture must be rejected by request validation.
func TestParseRequestRejectsUnsupportedShapes(t *testing.T) {
	for _, field := range []map[string]any{{"state": true}, {"state": nil}, {"stream": true}, {"model": ""}, {"questions": map[string]any{}}, {"questions": map[string]any{"a": map[string]any{"type": "chat", "instructions": "test"}}}} {
		var request map[string]any
		require.NoError(t, json.Unmarshal(ProbeBody("jev-latest", ""), &request))
		maps.Copy(request, field)
		body, err := json.Marshal(request)
		require.NoError(t, err)
		_, err = ParseRequest(body)
		require.Error(t, err)
	}
}

// TestBoxResponseValidation accepts zero edges and explicitly unknown confidence.
// Missing or malformed coordinates and confidence fail independently of token
// accounting, while valid boxes remain deliverable without usable usage.
func TestBoxResponseValidation(t *testing.T) {
	questions := map[string]Question{"a": {Type: "box"}}
	for _, tc := range []struct {
		answer string // raw box answer
		valid  bool   // expected answer acceptance
	}{
		{`{"type":"box","box":{"top":0,"bottom":1,"left":0,"right":1},"confidence":null}`, true},
		{`{"type":"box","box":{"top":0.2,"bottom":0.8,"left":0.1,"right":0.9},"confidence":0}`, true},
		{`{"type":"box","box":{"top":0,"bottom":1,"left":0,"right":1}}`, false},
		{`{"type":"box","box":{"bottom":1,"left":0,"right":1},"confidence":null}`, false},
		{`{"type":"box","box":{"top":-0.1,"bottom":1,"left":0,"right":1},"confidence":null}`, false},
		{`{"type":"box","box":{"top":1,"bottom":1,"left":0,"right":1},"confidence":null}`, false},
		{`{"type":"box","box":{"top":0,"bottom":1,"left":1,"right":0},"confidence":null}`, false},
		{`{"type":"box","box":{"top":0,"bottom":1,"left":0,"right":1},"confidence":"0.9"}`, false},
		{`{"type":"box","box":{"top":0,"bottom":1,"left":0,"right":1},"confidence":2}`, false},
	} {
		for _, usage := range []string{`null`, `{"input_tokens":0,"output_tokens":0}`, `{"input_tokens":-1,"output_tokens":2}`} {
			body := []byte(`{"model":"jev-1.14.0","answers":{"a":` + tc.answer + `},"usage":` + usage + `}`)
			response, err := ParseResponse(body, questions)
			if tc.valid {
				require.NoError(t, err)
				require.Equal(t, usage == `{"input_tokens":0,"output_tokens":0}`, response.HasUsage)
			} else {
				require.Error(t, err)
			}
		}
	}
}
