package httpapi

import (
	"context"

	"github.com/gin-gonic/gin"

	"github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	gatewayadapter "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	"github.com/TokenFlux/TokenRouter/internal/ops"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

// 凭据预算保存在 HTTP 请求中，平台准备器通过参数接收预算状态。
const credentialBudgetKey = "grok_credential_failover_deadline"

// CredentialObserver 将凭据故障分类记录到 Ops。
type CredentialObserver struct{ Context *gin.Context }

// RequestCredentialExecutor 将同一凭据用例绑定到媒体、WS 和文本 HTTP 入口。
type RequestCredentialExecutor struct {
	Runtime *gatewayadapter.RequestCredentials
}

// RequestCredentialBudget 返回本请求共享的预算，计时由使用方启动。
func RequestCredentialBudget(c *gin.Context) *requeststate.CredentialBudget {
	if c == nil {
		return nil
	}
	if value, ok := c.Get(credentialBudgetKey); ok {
		if state, ok := value.(*requeststate.CredentialBudget); ok {
			return state
		}
	}
	state := &requeststate.CredentialBudget{}
	c.Set(credentialBudgetKey, state)
	return state
}

func (o CredentialObserver) ObserveCredentialFailure(id int64, class forward.GrokCredentialFailure) {
	AppendOpsUpstreamError(o.Context, ops.OpsUpstreamErrorEvent{
		Platform:   capability.PlatformGrok,
		ProviderID: id,
		Stage:      string(forward.GatewayFailureStageProviderAuth),
		Scope:      string(class.Scope),
		Reason:     string(class.Reason),
		Kind:       "credential_failover",
		Message:    class.Message,
	})
}

func (s *RequestCredentialExecutor) Resolve(ctx context.Context, c *gin.Context, target *gatewayadapter.ExecutionProvider) (string, string, error) {
	return s.Runtime.Resolve(ctx, RequestCredentialBudget(c), CredentialObserver{Context: c}, target)
}
