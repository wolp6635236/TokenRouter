package openai

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReplaceModelInBody_PreservesTopLevelFieldOrder(t *testing.T) {
	body := []byte(`{"alpha":1,"model":"claude-3-5-sonnet-latest","messages":[],"omega":2}`)

	result := ReplaceModelInBody(body, "claude-3-5-sonnet-20241022")
	resultStr := string(result)

	assertJSONTokenOrder(t, resultStr, `"alpha"`, `"model"`, `"messages"`, `"omega"`)
	require.Contains(t, resultStr, `"model":"claude-3-5-sonnet-20241022"`)
}

func TestReplaceModelInBody(t *testing.T) {
	tests := []struct {
		name     string
		body     []byte
		newModel string
		check    func(t *testing.T, result []byte)
	}{
		{
			name:     "empty body",
			body:     []byte{},
			newModel: "new-model",
			check: func(t *testing.T, result []byte) {
				require.Equal(t, []byte{}, result)
			},
		},
		{
			name:     "model already equal",
			body:     []byte(`{"model":"claude-sonnet-4","temperature":0.7}`),
			newModel: "claude-sonnet-4",
			check: func(t *testing.T, result []byte) {
				require.Equal(t, []byte(`{"model":"claude-sonnet-4","temperature":0.7}`), result)
			},
		},
		{
			name:     "model different",
			body:     []byte(`{"model":"claude-sonnet-4","temperature":0.7}`),
			newModel: "claude-opus-4",
			check: func(t *testing.T, result []byte) {
				require.Contains(t, string(result), `"model":"claude-opus-4"`)
				require.Contains(t, string(result), `"temperature"`)
			},
		},
		{
			name:     "no model field",
			body:     []byte(`{"temperature":0.7}`),
			newModel: "claude-opus-4",
			check: func(t *testing.T, result []byte) {
				require.Contains(t, string(result), `"model":"claude-opus-4"`)
				require.Contains(t, string(result), `"temperature"`)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ReplaceModelInBody(tt.body, tt.newModel)
			tt.check(t, result)
		})
	}
}

func TestReplaceModelInBody_InvalidJSON(t *testing.T) {
	// 损坏的对象缺少可读的 model，sjson 仍尝试写入并返回修改后的字节。
	brokenBody := []byte("{broken")
	result := ReplaceModelInBody(brokenBody, "new-model")
	require.NotNil(t, result)

	// 数组输入使 sjson.SetBytes 返回错误，函数返回原始 body。
	arrayBody := []byte("[]")
	result2 := ReplaceModelInBody(arrayBody, "new-model")
	require.Equal(t, arrayBody, result2)
}

// assertJSONTokenOrder 按 JSON 原始字节中的位置检查字段顺序。
func assertJSONTokenOrder(t *testing.T, body string, tokens ...string) {
	t.Helper()

	last := -1
	for _, token := range tokens {
		pos := strings.Index(body, token)
		require.NotEqualf(t, -1, pos, "missing token %s in body %s", token, body)
		require.Greaterf(t, pos, last, "token %s should appear after previous tokens in body %s", token, body)
		last = pos
	}
}
