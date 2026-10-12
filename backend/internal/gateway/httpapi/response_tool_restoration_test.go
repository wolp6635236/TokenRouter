package httpapi

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/TokenFlux/TokenRouter/internal/protocol/bridge"
	"github.com/TokenFlux/TokenRouter/internal/upstream/openai"
)

func TestRestoreCodexToolNamesFromContext_HTTPAndWSPayloadShapes(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	SetCodexToolNameReverse(c, map[string]string{openai.CodexPythonToolAlias: "python"})

	streamEvent := RestoreCodexToolNamesFromContext(c, []byte(
		`{"type":"response.output_item.done","item":{"type":"function_call","name":"python__tokenrouter"},"note":"python__tokenrouter"}`,
	))
	require.Equal(t, "python", gjson.GetBytes(streamEvent, "item.name").String())
	require.Equal(t, "python__tokenrouter", gjson.GetBytes(streamEvent, "note").String())

	nonStreaming := RestoreCodexToolNamesFromContext(c, []byte(
		`{"id":"resp_1","output":[{"type":"function_call","name":"python__tokenrouter"}]}`,
	))
	require.Equal(t, "python", gjson.GetBytes(nonStreaming, "output.0.name").String())

	SetCodexToolNameReverse(c, nil)
	require.JSONEq(t,
		`{"type":"response.output_item.added","item":{"name":"python__tokenrouter"}}`,
		string(RestoreCodexToolNamesFromContext(c, []byte(`{"type":"response.output_item.added","item":{"name":"python__tokenrouter"}}`))),
	)
}

func TestRestoreCodexToolNamesInJSON_OnlyTouchesResponseToolCallNodesAndPreservesNumbers(t *testing.T) {
	reverse := map[string]string{openai.CodexPythonToolAlias: "python"}
	body := []byte(`{"type":"response.completed","response":{"output":[{"type":"function_call","name":"python__tokenrouter"},{"type":"message","name":"python__tokenrouter","content":[]}]},"metadata":{"name":"python__tokenrouter"},"sequence":900719925474099312345}`)

	restored := openai.RestoreCodexToolNamesInJSON(body, reverse)
	require.Equal(t, "python", gjson.GetBytes(restored, "response.output.0.name").String())
	require.Equal(t, openai.CodexPythonToolAlias, gjson.GetBytes(restored, "response.output.1.name").String())
	require.Equal(t, openai.CodexPythonToolAlias, gjson.GetBytes(restored, "metadata.name").String())
	require.Equal(t, "900719925474099312345", gjson.GetBytes(restored, "sequence").Raw)
}

func TestRestoreCodexToolNamesInJSON_ExplicitHTTPAndSSEToolCallProtocols(t *testing.T) {
	reverse := map[string]string{openai.CodexPythonToolAlias: "python"}
	tests := []struct {
		name string
		body string
		path string
	}{
		{
			name: "chat http",
			body: `{"choices":[{"message":{"tool_calls":[{"type":"function","function":{"name":"python__tokenrouter"}}]}}],"metadata":{"name":"python__tokenrouter"}}`,
			path: "choices.0.message.tool_calls.0.function.name",
		},
		{
			name: "chat sse",
			body: `{"choices":[{"delta":{"tool_calls":[{"type":"function","function":{"name":"python__tokenrouter"}}]}}],"metadata":{"name":"python__tokenrouter"}}`,
			path: "choices.0.delta.tool_calls.0.function.name",
		},
		{
			name: "messages tool use",
			body: `{"type":"content_block_start","content":[{"type":"tool_use","name":"python__tokenrouter"}],"metadata":{"name":"python__tokenrouter"}}`,
			path: "content.0.name",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			restored := openai.RestoreCodexToolNamesInJSON([]byte(tt.body), reverse)
			require.Equal(t, "python", gjson.GetBytes(restored, tt.path).String())
			require.Equal(t, openai.CodexPythonToolAlias, gjson.GetBytes(restored, "metadata.name").String())
		})
	}
}

func TestCodexToolNameReverse_WSSessionReplacementDoesNotChangeActiveTurn(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	SetCodexToolNameReverse(c, nil)
	first := []byte(`{"type":"response.create","tools":[{"type":"function","name":"python"}]}`)
	UpdateCodexToolNameReverseForWSFrame(c, first, map[string]string{openai.CodexPythonToolAlias: "python"})

	update := []byte(`{"type":"session.update","session":{"tools":[{"type":"function","name":"python__tokenrouter"}]}}`)
	UpdateCodexToolNameReverseForWSFrame(c, update, nil)
	currentOutput := RestoreCodexToolNamesFromContext(c, []byte(`{"type":"response.output_item.done","item":{"type":"function_call","name":"python__tokenrouter"}}`))
	require.Equal(t, "python", gjson.GetBytes(currentOutput, "item.name").String())
	sessionEcho := RestoreCodexToolNamesFromContext(c, []byte(`{"type":"session.updated","session":{"tools":[{"type":"function","name":"python__tokenrouter"}]}}`))
	require.Equal(t, openai.CodexPythonToolAlias, gjson.GetBytes(sessionEcho, "session.tools.0.name").String())

	next := []byte(`{"type":"response.create","input":"next"}`)
	UpdateCodexToolNameReverseForWSFrame(c, next, nil)
	nextOutput := RestoreCodexToolNamesFromContext(c, []byte(`{"type":"response.output_item.done","item":{"type":"function_call","name":"python__tokenrouter"}}`))
	require.Equal(t, openai.CodexPythonToolAlias, gjson.GetBytes(nextOutput, "item.name").String())

	sessionPython := []byte(`{"type":"session.update","session":{"tools":[{"type":"function","name":"python"}]}}`)
	UpdateCodexToolNameReverseForWSFrame(c, sessionPython, map[string]string{openai.CodexPythonToolAlias: "python"})
	explicitLiteral := []byte(`{"type":"response.create","input":[{"type":"additional_tools","tools":[{"type":"function","name":"python__tokenrouter"}]}]}`)
	UpdateCodexToolNameReverseForWSFrame(c, explicitLiteral, nil)
	literalOutput := RestoreCodexToolNamesFromContext(c, []byte(`{"type":"response.output_item.done","item":{"type":"function_call","name":"python__tokenrouter"}}`))
	require.Equal(t, openai.CodexPythonToolAlias, gjson.GetBytes(literalOutput, "item.name").String())
	UpdateCodexToolNameReverseForWSFrame(c, next, nil)
	inheritedOutput := RestoreCodexToolNamesFromContext(c, []byte(`{"type":"response.output_item.done","item":{"type":"function_call","name":"python__tokenrouter"}}`))
	require.Equal(t, "python", gjson.GetBytes(inheritedOutput, "item.name").String())
}

func TestRestoreCodexToolNamesFromSSEContextUsesEventLineTypeWithoutAddingType(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	SetCodexToolNameReverse(c, map[string]string{openai.CodexPythonToolAlias: openai.CodexReservedPythonToolName})
	payload := []byte(`{"item":{"type":"function_call","name":"python__tokenrouter"},"metadata":{"name":"python__tokenrouter"}}`)

	restored := RestoreCodexToolNamesFromSSEContext(c, payload, "response.output_item.done")

	require.Equal(t, openai.CodexReservedPythonToolName, gjson.GetBytes(restored, "item.name").String())
	require.Equal(t, openai.CodexPythonToolAlias, gjson.GetBytes(restored, "metadata.name").String())
	require.False(t, gjson.GetBytes(restored, "type").Exists())
}

func TestClearGrokResponsesClientToolMappingRemovesStaleContextState(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	SetGrokResponsesClientToolMapping(c, bridge.ResponsesClientToolMapping{
		CustomTools: map[string]bool{"stale_tool": true},
	})

	_, seeded := GrokResponsesClientToolMapping(c)
	require.True(t, seeded)
	ClearGrokResponsesClientToolMapping(c)
	_, remains := GrokResponsesClientToolMapping(c)
	require.False(t, remains)
}

func TestClearOpenAIResponsesClientToolMappingRemovesStaleContextState(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	SetOpenAIResponsesClientToolMapping(c, bridge.ResponsesClientToolMapping{CustomTools: map[string]bool{"exec": true}})

	ClearOpenAIResponsesClientToolMapping(c)

	_, ok := OpenAIResponsesClientToolMapping(c)
	require.False(t, ok)
}
