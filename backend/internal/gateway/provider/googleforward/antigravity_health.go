package googleforward

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	"github.com/TokenFlux/TokenRouter/internal/infra/httpclient"
	"github.com/TokenFlux/TokenRouter/internal/infra/telemetry/logging"
	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"
)

// antigravityRetryLoopParams 重试循环的参数。
type antigravityRetryLoopParams struct {
	userAgent       string // 指定提供商探测显式提供，普通转发保持空值。
	ctx             context.Context
	prefix          string
	provider        *gatewayprovider.ExecutionProvider
	proxyURL        string
	accessToken     string
	action          string
	body            []byte
	c               *attempt
	httpUpstream    httpclient.UpstreamTransport
	providerRepo    gatewayprovider.ExecutionProviderStore // 用于智能重试的模型级别限流
	handleError     func(ctx context.Context, prefix string, provider *gatewayprovider.ExecutionProvider, statusCode int, headers http.Header, body []byte, requestedModel string, groupID int64, sessionHash string, isStickySession bool) *provideradapter.AntigravityModelLimitResult
	requestedModel  string // 用于限流检查的原始请求模型
	isStickySession bool   // 是否为粘性会话（用于提供商切换时的缓存计费判断）
	groupID         int64  // 用于模型级限流时清除粘性会话
	sessionHash     string // 用于模型级限流时清除粘性会话
}

// logPrefix 生成统一的日志前缀。
func logPrefix(sessionID, providerName string) string {
	if sessionID != "" {
		return fmt.Sprintf("[antigravity-Forward] session=%s provider=%s", sessionID, providerName)
	}
	return fmt.Sprintf("[antigravity-Forward] provider=%s", providerName)
}

func (s *Antigravity) shouldFailoverUpstreamError(statusCode int) bool {
	switch statusCode {
	case 401, 403, 429, 529:
		return true
	default:
		return statusCode >= 500
	}
}

// isSingleProviderRetry 检查 context 中是否设置了单提供商退避重试标记。
func isSingleProviderRetry(ctx context.Context) bool {
	v, _ := requeststate.SingleProviderRetryFromContext(ctx)
	return v
}

func (s *Antigravity) clearStickySession(ctx context.Context, groupID int64, sessionHash string) {
	if s == nil || s.Sticky == nil || strings.TrimSpace(sessionHash) == "" {
		return
	}
	if err := s.Sticky.DeleteSessionProviderID(ctx, groupID, sessionHash); err != nil {
		logging.LegacyPrintf("service.antigravity_gateway", "[antigravity-Forward] sticky_session_clear_failed group_id=%d session=%s err=%v", groupID, sessionHash[:min(8, len(sessionHash))], err)
	}
}

// 将本次观测交给 provider 判断健康状态并写入。

func (s *Antigravity) handleUpstreamError(ctx context.Context, prefix string, value *gatewayprovider.ExecutionProvider, status int, headers http.Header, body []byte, model string, groupID int64, sessionHash string, sticky bool) *provideradapter.AntigravityModelLimitResult {
	view := gatewayprovider.ExecutionRecord(value)
	observer := s.Errors
	input := provideradapter.AntigravityErrorInput{
		Context:          ctx,
		Provider:         view,
		Prefix:           prefix,
		Status:           status,
		Headers:          headers,
		Body:             body,
		RequestedModel:   model,
		Thinking:         requeststate.HealthThinking(ctx),
		OtherObservation: gatewayprovider.HealthObservationFromContext(ctx, status, headers, body, nil),
		Sticky:           sticky,
	}
	if s.Sticky != nil && sessionHash != "" {
		input.ClearSticky = func() { _ = s.Sticky.DeleteSessionProviderID(ctx, groupID, sessionHash) }
	}
	result := observer.Observe(input)
	if value != nil && view != nil {
		value.Record.Extra, value.Record.Credentials = view.Extra, view.Credentials
	}
	return result
}
