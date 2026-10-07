package httpapi

import (
	"bytes"
	"net/http/httptest"
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/server/httpx"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// bindGroupPlatformJSON 管理接口明确拒绝已经移除的字段，避免旧表单静默丢失配置。
func bindGroupPlatformJSON(t *testing.T, target any, body string) error {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return httpx.BindJSONStrict(c, target)
}

func TestGroupManagementRejectsRetiredFields(t *testing.T) {
	for _, body := range []string{`{"name":"g","platform":"openai"}`, `{"name":"g","is_default":false}`} {
		require.Error(t, bindGroupPlatformJSON(t, &CreateGroupRequest{}, body))
		require.Error(t, bindGroupPlatformJSON(t, &UpdateGroupRequest{}, body))
	}
	var req CreateGroupRequest
	require.NoError(t, bindGroupPlatformJSON(t, &req, `{"name":"mixed","allowed_protocols":["anthropic_messages","openai_responses"],"protocol_fallbacks":{"anthropic_messages":[]}}`))
	require.Equal(t, "mixed", req.Name)
	require.NotNil(t, req.ProtocolFallbacks["anthropic_messages"])
}

// TestGroupManagementRejectsRetiredMessagesMapping 验证管理端不得继续保存已经移除的协议专用模型映射。
func TestGroupManagementRejectsRetiredMessagesMapping(t *testing.T) {
	body := `{"messages_dispatch_model_config":{"exact_model_mappings":{"claude-sonnet-4-6":"target"}}}`
	require.Error(t, bindGroupPlatformJSON(t, &CreateGroupRequest{}, body))
	require.Error(t, bindGroupPlatformJSON(t, &UpdateGroupRequest{}, body))
	var req UpdateGroupRequest
	require.NoError(t, bindGroupPlatformJSON(t, &req, `{"routing_policy":{"enabled":true,"model_mapping":{"claude-sonnet-4-6":"target"}}}`))
	require.Equal(t, "target", req.RoutingPolicy.ModelMapping["claude-sonnet-4-6"])
}
