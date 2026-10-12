package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"

	forwardcore "github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	gatewayhttp "github.com/TokenFlux/TokenRouter/internal/gateway/httpapi"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/session"
	gatewayws "github.com/TokenFlux/TokenRouter/internal/gateway/ws"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"
	"github.com/TokenFlux/TokenRouter/internal/scheduler"
	"github.com/TokenFlux/TokenRouter/internal/upstream/openai"
	openaiws "github.com/TokenFlux/TokenRouter/internal/upstream/openai/ws"
)

const (
	openAIWSTurnStateHeader               = "x-codex-turn-state"
	openAIWSPayloadKeySizeTopN            = 6
	openAIWSPassthroughIdleTimeoutDefault = time.Hour

	openAIWSStoreDisabledConnModeStrict   = "strict"
	openAIWSStoreDisabledConnModeAdaptive = "adaptive"
	openAIWSStoreDisabledConnModeOff      = "off"
)

var openAIWSIngressPreflightPingIdle = 20 * time.Second

// WS 重试资格与当前轮重放载荷由 gateway/ws 唯一持有。

// openAIWSFastModePolicyContext 为当前 turn 生成带最新单 Key Fast 策略的上下文。
func openAIWSFastModePolicyContext(ctx context.Context, hooks *gatewayws.OpenAIIngressHooks, turn int) context.Context {
	if hooks == nil || hooks.ResolveFastModePolicy == nil {
		return ctx
	}
	return gatewayprovider.
		WithAPIKeyFastModePolicy(ctx, hooks.ResolveFastModePolicy(turn))
}

// resolveOpenAIWSTurnModels 按 R -> G -> U 顺序解析单个 WebSocket turn 的模型。
// 调用方保存 originalModel，返回值用于通过提供商能力检查后的上游请求。
func resolveOpenAIWSTurnModels(provider *gatewayprovider.ExecutionProvider, hooks *gatewayws.OpenAIIngressHooks, turn int, requestedModel string, payload []byte) (string, string, error) {
	routingModel := strings.TrimSpace(requestedModel)
	if hooks != nil && hooks.ResolveRoutingModel != nil {
		resolved, err := hooks.ResolveRoutingModel(turn, routingModel, payload)
		if err != nil {
			return "", "", err
		}
		routingModel = strings.TrimSpace(resolved)
	}
	if routingModel == "" {
		return "", "", gatewayhttp.NewOpenAIWSClientCloseError(
			coderws.StatusPolicyViolation,
			"model is required in response.create payload",
			nil,
		)
	}

	upstreamModel := gatewayprovider.ExecutionModelPolicy(provider).NormalizeOpenAI(providercore.ResolveForwardMappedModel(gatewayprovider.ExecutionRecord(provider), routingModel, provideradapter.ModelDefaults()))
	if upstreamModel == "" {
		upstreamModel = routingModel
	}
	return routingModel, upstreamModel, nil
}

func (s *OpenAIWebSocketExecutor) SnapshotOpenAIWSPoolMetrics() openaiws.WSPoolMetricsSnapshot {
	pool := s.Connections.Pool()
	if pool == nil {
		return openaiws.WSPoolMetricsSnapshot{}
	}
	return pool.SnapshotMetrics()
}

func (s *OpenAIWebSocketExecutor) OpenAIHTTPResponseStickyTTL() time.Duration {
	if s != nil && s.Options != nil {
		seconds := s.Options.StickyResponseIDTTLSeconds
		if seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	return time.Hour
}

func (s *OpenAIWebSocketExecutor) openAIWSIngressPreviousResponseRecoveryEnabled() bool {
	if s != nil && s.Options != nil {
		return s.Options.IngressPreviousResponseRecoveryEnabled
	}
	return true
}

func (s *OpenAIWebSocketExecutor) openAIWSReadTimeout() time.Duration {
	if s != nil && s.Options != nil && s.Options.ReadTimeoutSeconds > 0 {
		return time.Duration(s.Options.ReadTimeoutSeconds) * time.Second
	}
	return 15 * time.Minute
}

func (s *OpenAIWebSocketExecutor) openAIWSPassthroughIdleTimeout() time.Duration {
	if timeout := s.openAIWSReadTimeout(); timeout > 0 {
		return timeout
	}
	return openAIWSPassthroughIdleTimeoutDefault
}

func (s *OpenAIWebSocketExecutor) openAIWSWriteTimeout() time.Duration {
	if s != nil && s.Options != nil && s.Options.WriteTimeoutSeconds > 0 {
		return time.Duration(s.Options.WriteTimeoutSeconds) * time.Second
	}
	return 2 * time.Minute
}

func (s *OpenAIWebSocketExecutor) openAIWSDialTimeout() time.Duration {
	if s != nil && s.Options != nil && s.Options.DialTimeoutSeconds > 0 {
		return time.Duration(s.Options.DialTimeoutSeconds) * time.Second
	}
	return 10 * time.Second
}

func (s *OpenAIWebSocketExecutor) openAIWSAcquireTimeout() time.Duration {
	// Acquire 的预算覆盖连接复用、排队和新建连接，高并发排队也使用该预算。
	dial := s.openAIWSDialTimeout()
	if dial <= 0 {
		dial = 10 * time.Second
	}
	return dial + 2*time.Second
}

// bindOpenAIWSResponseSessionOwner 将上游返回的 response_id 记录为当前分组的会话归属。
// 后续客户端携带 previous_response_id 切到开启隔离的其它分组时，会被统一拦截。
func (s *OpenAIWebSocketExecutor) bindOpenAIWSResponseSessionOwner(ctx context.Context, c *gin.Context, responseID string) {
	if s == nil || c == nil {
		return
	}
	apiKey := gatewayhttp.GetExecutionAPIKey(c)
	if apiKey == nil || apiKey.UserID <= 0 {
		return
	}
	responseHash, _ := scheduler.DeriveSessionHashes(responseID)
	if responseHash == "" {
		return
	}
	_ = s.EnsureSessionIsolation(ctx, apiKey, apiKey.UserID, session.SessionIsolationSourceOpenAIPreviousResponse, responseHash)
}

func buildOpenAIWSUpstreamWarning(eventType string, message []byte) *forwardcore.UpstreamWarning {
	if !openAIWSEventMayCarryUpstreamWarning(eventType) || len(message) == 0 {
		return nil
	}
	statusCode := http.StatusBadGateway
	if strings.TrimSpace(eventType) == "error" {
		statusCode = openai.WSErrorHTTPStatus(message)
	}
	return &forwardcore.UpstreamWarning{
		StatusCode:   statusCode,
		ResponseBody: append([]byte(nil), message...),
		Message:      extractOpenAIWSUpstreamWarningMessage(message),
	}
}

func extractOpenAIWSUpstreamWarningMessage(message []byte) string {
	if len(message) == 0 {
		return ""
	}
	paths := []string{
		"error.message",
		"response.error.message",
		"response.status_details.error.message",
		"response.incomplete_details.reason",
	}
	for _, path := range paths {
		if value := strings.TrimSpace(gjson.GetBytes(message, path).String()); value != "" {
			return value
		}
	}
	return ""
}

func openAIWSEventMayCarryUpstreamWarning(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case "error", "response.failed", "response.incomplete":
		return true
	default:
		return false
	}
}
