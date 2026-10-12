package httpapi

// 透传策略场景覆盖 gateway/ws/service_tier_frame.go、gateway/ws/client_frames.go、gateway/ws/usage_meta.go 和 gateway/ws/turn_lifecycle.go，检查帧过滤、用量记录和轮次切换。

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	protocolforward "github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	gatewayhttp "github.com/TokenFlux/TokenRouter/internal/gateway/httpapi"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	"github.com/TokenFlux/TokenRouter/internal/gateway/tierpolicy"
	gatewayws "github.com/TokenFlux/TokenRouter/internal/gateway/ws"
	protocolanthropic "github.com/TokenFlux/TokenRouter/internal/protocol/anthropic"
	protocolbridge "github.com/TokenFlux/TokenRouter/internal/protocol/bridge"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	claude "github.com/TokenFlux/TokenRouter/internal/upstream/anthropic"
	"github.com/TokenFlux/TokenRouter/internal/upstream/openai"
	openaiwsv2 "github.com/TokenFlux/TokenRouter/internal/upstream/openai/ws/relay"
)

// fakePassthroughFrameConn 依次返回预设的客户端帧，读完后返回 io.EOF，并记录写入供断言检查。
type fakePassthroughFrameConn struct {
	reads     [][]byte
	idx       int
	writes    [][]byte
	closeOnce bool
}

type openAIWSPolicyEnforcingFrameConn struct {
	inner       openaiwsv2.FrameConn
	filter      func(coderws.MessageType, []byte) ([]byte, *tierpolicy.BlockedError, error)
	writeFilter func(coderws.MessageType, []byte) ([]byte, error)
	onBlock     func(*tierpolicy.BlockedError)
	once        sync.Once
	core        gatewayws.FrameConn
}

type openAIWSClientFrameConn struct {
	interTurnStarted   chan struct{}
	waitingForNextTurn atomic.Bool
}

// openAIWSPassthroughUsageMeta 保存会话用量和推理强度。
type openAIWSPassthroughUsageMeta struct {
	*gatewayws.UsageMeta
	// reasoningEffort 指向会话共享的推理强度。
	reasoningEffort *atomic.Pointer[string]
}

// 兼容方法使用共享的 turn 屏障。
type openAIWSPassthroughTurnLifecycle struct{ *gatewayws.TurnLifecycle }

func TestWSResponseCreate_DefaultPassesPriorityAndNormalizesFast(t *testing.T) {
	svc := newWSFastPolicy(t, tierpolicy.Default())
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}

	frame := []byte(`{"type":"response.create","model":"gpt-5.5","service_tier":"priority","input":[{"type":"input_text","text":"hi"}]}`)
	updated, blocked, err := gatewayws.ApplyServiceTierFrame(frame, "gpt-5.5", svc.Input(context.Background(), provider, "gpt-5.5"))
	require.NoError(t, err)
	require.Nil(t, blocked)
	require.Equal(t, "priority", gjson.GetBytes(updated, "service_tier").String(), "default policy should preserve priority tier")
	// 其他字段保持不变。
	require.Equal(t, "response.create", gjson.GetBytes(updated, "type").String())
	require.Equal(t, "gpt-5.5", gjson.GetBytes(updated, "model").String())
	require.Equal(t, "hi", gjson.GetBytes(updated, "input.0.text").String())

	frame = []byte(`{"type":"response.create","model":"gpt-5.5","service_tier":"fast"}`)
	updated, blocked, err = gatewayws.ApplyServiceTierFrame(frame, "gpt-5.5", svc.Input(context.Background(), provider, "gpt-5.5"))
	require.NoError(t, err)
	require.Nil(t, blocked)
	require.Equal(t, "priority", gjson.GetBytes(updated, "service_tier").String(), "fast alias should normalize before reaching upstream")

	// 混合大小写和前后空白的别名也应归一化。
	frame = []byte(`{"type":"response.create","model":"gpt-5.5","service_tier":"  Fast  "}`)
	updated, blocked, err = gatewayws.ApplyServiceTierFrame(frame, "gpt-5.5", svc.Input(context.Background(), provider, "gpt-5.5"))
	require.NoError(t, err)
	require.Nil(t, blocked)
	require.Equal(t, "priority", gjson.GetBytes(updated, "service_tier").String())
}

func TestWSResponseCreate_RejectsUltraBeforeUpstream(t *testing.T) {
	svc := newWSFastPolicy(t, tierpolicy.Default())
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}

	frames := [][]byte{
		[]byte(`{"type":"response.create","model":"gpt-5.6-sol","reasoning":{"effort":"ultra"}}`),
		[]byte(`{"type":"session.update","session":{"model":"gpt-5.6-sol","reasoning":{"effort":"ultra"}}}`),
	}
	for _, frame := range frames {
		updated, blocked, err := gatewayws.ApplyServiceTierFrame(frame, "gpt-5.6-sol", svc.Input(context.Background(), provider, "gpt-5.6-sol"))
		require.ErrorContains(t, err, "not supported")
		require.Nil(t, blocked)
		require.Equal(t, frame, updated)
	}
}

func TestWSResponseCreate_ExplicitFilterStripsServiceTier(t *testing.T) {
	svc := newWSFastPolicy(t, openAIFastFilterPriorityPolicy())
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}

	frame := []byte(`{"type":"response.create","model":"gpt-5.5","service_tier":"priority","input":[{"type":"input_text","text":"hi"}]}`)
	updated, blocked, err := gatewayws.ApplyServiceTierFrame(frame, "gpt-5.5", svc.Input(context.Background(), provider, "gpt-5.5"))
	require.NoError(t, err)
	require.Nil(t, blocked)
	require.NotContains(t, string(updated), `"service_tier"`, "filter action should strip service_tier")

	frame = []byte(`{"type":"response.create","model":"gpt-5.5","service_tier":"fast"}`)
	updated, blocked, err = gatewayws.ApplyServiceTierFrame(frame, "gpt-5.5", svc.Input(context.Background(), provider, "gpt-5.5"))
	require.NoError(t, err)
	require.Nil(t, blocked)
	require.NotContains(t, string(updated), `"service_tier"`)
}

func TestWSResponseCreate_UserScopedRuleOverridesGlobalRule(t *testing.T) {
	settings := &tierpolicy.OpenAIFastPolicySettings{
		Rules: []tierpolicy.OpenAIFastPolicyRule{
			{
				ServiceTier: tierpolicy.OpenAIFastTierPriority,
				Action:      claude.BetaPolicyActionFilter,
				Scope:       claude.BetaPolicyScopeAll,
			},
			{
				ServiceTier: tierpolicy.OpenAIFastTierPriority,
				Action:      claude.BetaPolicyActionPass,
				Scope:       claude.BetaPolicyScopeAll,
				UserIDs:     []int64{42},
			},
		},
	}
	svc := newWSFastPolicy(t, settings)
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}
	frame := []byte(`{"type":"response.create","model":"gpt-5.5","service_tier":"priority"}`)

	allowedUserCtx := apikey.WithAccessSnapshot(context.Background(), apikey.AccessSnapshot{PayerUserID: int64(42)})
	updated, blocked, err := gatewayws.ApplyServiceTierFrame(frame, "gpt-5.5", svc.Input(allowedUserCtx, provider, "gpt-5.5"))
	require.NoError(t, err)
	require.Nil(t, blocked)
	require.Equal(t, "priority", gjson.GetBytes(updated, "service_tier").String())

	otherUserCtx := apikey.WithAccessSnapshot(context.Background(), apikey.AccessSnapshot{PayerUserID: int64(43)})
	updated, blocked, err = gatewayws.ApplyServiceTierFrame(frame, "gpt-5.5", svc.Input(otherUserCtx, provider, "gpt-5.5"))
	require.NoError(t, err)
	require.Nil(t, blocked)
	require.NotContains(t, string(updated), `"service_tier"`)
}

func TestWSResponseCreate_ForcePriorityRewritesKnownTier(t *testing.T) {
	settings := &tierpolicy.OpenAIFastPolicySettings{
		Rules: []tierpolicy.OpenAIFastPolicyRule{{
			ServiceTier: tierpolicy.OpenAIFastTierAny,
			Action:      tierpolicy.OpenAIFastPolicyActionForcePriority,
			Scope:       claude.BetaPolicyScopeAll,
		}},
	}
	svc := newWSFastPolicy(t, settings)
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}

	for _, tier := range []string{"flex", "auto", "default", "scale", "fast", "priority", "ultrafast"} {
		frame := []byte(`{"type":"response.create","model":"gpt-5.5","service_tier":"` + tier + `"}`)
		updated, blocked, err := gatewayws.ApplyServiceTierFrame(frame, "gpt-5.5", svc.Input(context.Background(), provider, "gpt-5.5"))
		require.NoError(t, err)
		require.Nil(t, blocked)
		require.Equal(t, tierpolicy.OpenAIFastTierPriority, gjson.GetBytes(updated, "service_tier").String(),
			"tier %q should be forced to priority", tier)
	}
}

func TestWSResponseCreate_FlexPassThrough(t *testing.T) {
	svc := newWSFastPolicy(t, tierpolicy.Default())
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}

	// 默认配置没有规则；flex 应保持原样。
	frame := []byte(`{"type":"response.create","model":"gpt-5.5","service_tier":"flex"}`)
	updated, blocked, err := gatewayws.ApplyServiceTierFrame(frame, "gpt-5.5", svc.Input(context.Background(), provider, "gpt-5.5"))
	require.NoError(t, err)
	require.Nil(t, blocked)
	require.Equal(t, "flex", gjson.GetBytes(updated, "service_tier").String(), "flex frames must reach upstream untouched under default policy")
}

func TestWSResponseCreate_BlockReturnsTypedError(t *testing.T) {
	settings := &tierpolicy.OpenAIFastPolicySettings{
		Rules: []tierpolicy.OpenAIFastPolicyRule{{
			ServiceTier:    tierpolicy.OpenAIFastTierPriority,
			Action:         claude.BetaPolicyActionBlock,
			Scope:          claude.BetaPolicyScopeAll,
			ErrorMessage:   "ws fast blocked",
			ModelWhitelist: []string{"gpt-5.5"},
			FallbackAction: claude.BetaPolicyActionPass,
		}},
	}
	svc := newWSFastPolicy(t, settings)
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}

	frame := []byte(`{"type":"response.create","model":"gpt-5.5","service_tier":"priority"}`)
	updated, blocked, err := gatewayws.ApplyServiceTierFrame(frame, "gpt-5.5", svc.Input(context.Background(), provider, "gpt-5.5"))
	require.NoError(t, err)
	require.NotNil(t, blocked)
	require.Equal(t, "ws fast blocked", blocked.Message)
	// On block, payload returned unchanged so caller can inspect / log it.
	require.Equal(t, string(frame), string(updated))
}

func TestWSResponseCreate_NoServiceTierUntouched(t *testing.T) {
	svc := newWSFastPolicy(t, tierpolicy.Default())
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}

	frame := []byte(`{"type":"response.create","model":"gpt-5.5","input":[]}`)
	updated, blocked, err := gatewayws.ApplyServiceTierFrame(frame, "gpt-5.5", svc.Input(context.Background(), provider, "gpt-5.5"))
	require.NoError(t, err)
	require.Nil(t, blocked)
	require.Equal(t, string(frame), string(updated), "no service_tier present must result in zero mutation")
}

func TestWSResponseCreate_NonResponseCreateFrameUntouched(t *testing.T) {
	settings := &tierpolicy.OpenAIFastPolicySettings{
		Rules: []tierpolicy.OpenAIFastPolicyRule{{
			ServiceTier:    tierpolicy.OpenAIFastTierPriority,
			Action:         claude.BetaPolicyActionFilter,
			Scope:          claude.BetaPolicyScopeAll,
			ModelWhitelist: []string{"*"},
			FallbackAction: claude.BetaPolicyActionFilter,
		}},
	}
	svc := newWSFastPolicy(t, settings)
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}

	// response.cancel 的 service_tier 字段保持原样。
	frame := []byte(`{"type":"response.cancel","service_tier":"priority"}`)
	updated, blocked, err := gatewayws.ApplyServiceTierFrame(frame, "gpt-5.5", svc.Input(context.Background(), provider, "gpt-5.5"))
	require.NoError(t, err)
	require.Nil(t, blocked)
	require.Equal(t, string(frame), string(updated))
}

// TestWSResponseCreate_EmptyTypeFrameUntouched 类型为空的帧保持完整，策略匹配 response.create 类型。
func TestWSResponseCreate_EmptyTypeFrameUntouched(t *testing.T) {
	settings := &tierpolicy.OpenAIFastPolicySettings{
		Rules: []tierpolicy.OpenAIFastPolicyRule{{
			ServiceTier:    tierpolicy.OpenAIFastTierPriority,
			Action:         claude.BetaPolicyActionFilter,
			Scope:          claude.BetaPolicyScopeAll,
			ModelWhitelist: []string{"*"},
			FallbackAction: claude.BetaPolicyActionFilter,
		}},
	}
	svc := newWSFastPolicy(t, settings)
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}

	// Frame with no "type" field: must pass through completely unchanged
	// even with a service_tier-shaped field present.
	frame := []byte(`{"service_tier":"priority","model":"gpt-5.5"}`)
	updated, blocked, err := gatewayws.ApplyServiceTierFrame(frame, "gpt-5.5", svc.Input(context.Background(), provider, "gpt-5.5"))
	require.NoError(t, err)
	require.Nil(t, blocked)
	require.Equal(t, string(frame), string(updated), "empty type must NOT be policy-checked — Realtime spec requires type, malformed frames are passed through")

	// Explicit empty string also passes through.
	frame = []byte(`{"type":"","service_tier":"priority","model":"gpt-5.5"}`)
	updated, blocked, err = gatewayws.ApplyServiceTierFrame(frame, "gpt-5.5", svc.Input(context.Background(), provider, "gpt-5.5"))
	require.NoError(t, err)
	require.Nil(t, blocked)
	require.Equal(t, string(frame), string(updated))
}

func (f *fakePassthroughFrameConn) ReadFrame(ctx context.Context) (coderws.MessageType, []byte, error) {
	if f.idx >= len(f.reads) {
		return coderws.MessageText, nil, openai.ErrWSConnClosed
	}
	payload := f.reads[f.idx]
	f.idx++
	return coderws.MessageText, payload, nil
}

func (f *fakePassthroughFrameConn) WriteFrame(ctx context.Context, msgType coderws.MessageType, payload []byte) error {
	cp := append([]byte(nil), payload...)
	f.writes = append(f.writes, cp)
	return nil
}

func (f *fakePassthroughFrameConn) Close() error {
	f.closeOnce = true
	return nil
}

// gpt55WhitelistFastPolicy 返回带模型白名单的策略，供 capturedSessionModel 回退测试使用。
// 默认配置没有规则，无法观察模型回退后的策略结果。
func gpt55WhitelistFastPolicy() *tierpolicy.OpenAIFastPolicySettings {
	return &tierpolicy.OpenAIFastPolicySettings{
		Rules: []tierpolicy.OpenAIFastPolicyRule{{
			ServiceTier:    tierpolicy.OpenAIFastTierPriority,
			Action:         claude.BetaPolicyActionFilter,
			Scope:          claude.BetaPolicyScopeAll,
			ModelWhitelist: []string{"gpt-5.5", "gpt-5.5*"},
			FallbackAction: claude.BetaPolicyActionPass,
		}},
	}
}

// TestPolicyEnforcingFrameConn_FollowupFrameWithoutModelUsesCapturedModel 检查省略 model 的后续帧使用首帧模型匹配策略。
func TestPolicyEnforcingFrameConn_FollowupFrameWithoutModelUsesCapturedModel(t *testing.T) {
	// 此处特意使用带 whitelist 的策略，以便观察 capturedSessionModel
	// fallback 是否生效（默认配置没有规则，fallback 与否结果一致，
	// 不能用来覆盖此回归）。
	svc := newWSFastPolicy(t, gpt55WhitelistFastPolicy())
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}

	// Simulate the passthrough adapter capturing model from the first frame.
	firstFrame := []byte(`{"type":"response.create","model":"gpt-5.5","service_tier":"priority"}`)
	capturedSessionModel := openAIWSPassthroughPolicyModelForFrame(provider, firstFrame)
	require.Equal(t, "gpt-5.5", capturedSessionModel)

	// Realtime 允许后续帧省略 model。
	followupFrame := []byte(`{"type":"response.create","service_tier":"priority"}`)

	inner := &fakePassthroughFrameConn{
		reads: [][]byte{followupFrame},
	}
	wrapper := &openAIWSPolicyEnforcingFrameConn{
		inner: inner,
		filter: func(msgType coderws.MessageType, payload []byte) ([]byte, *tierpolicy.BlockedError, error) {
			if msgType != coderws.MessageText {
				return payload, nil, nil
			}
			model := openAIWSPassthroughPolicyModelForFrame(provider, payload)
			if model == "" {
				model = capturedSessionModel
			}
			return gatewayws.ApplyServiceTierFrame(payload, model, svc.Input(context.Background(), provider, model))
		},
	}

	// Read the follow-up frame through the wrapper. The policy MUST still
	// trigger filter (gpt-5.5 + priority → filter), so the service_tier
	// field is gone by the time the relay sees it.
	_, payload, err := wrapper.ReadFrame(context.Background())
	require.NoError(t, err)
	require.NotContains(t, string(payload), `"service_tier"`,
		"D5 regression: empty model on follow-up frame must fall back to capturedSessionModel; whitelist policy filters service_tier=priority for gpt-5.5")
	require.Equal(t, "response.create", gjson.GetBytes(payload, "type").String())
}

func TestOpenAIWSPassthroughPolicyModelDoesNotApplyProviderMapping(t *testing.T) {
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI,
			Type: capability.ProviderTypeOAuth,
			Credentials: map[string]any{
				"model_mapping": map[string]any{"public-model": "private-model"},
			},
			Extra: map[string]any{"openai_passthrough": true},
		},
	}

	responseCreate := []byte(`{"type":"response.create","model":"public-model"}`)
	require.Equal(t, "public-model", openAIWSPassthroughPolicyModelForFrame(provider, responseCreate))

	sessionUpdate := []byte(`{"type":"session.update","session":{"model":"public-model"}}`)
	require.Equal(t, "public-model", openAIWSPassthroughPolicyModelFromSessionFrame(provider, sessionUpdate))
}

// TestPolicyEnforcingFrameConn_WithoutCapturedFallbackPolicyMisses 检查缺少帧模型和会话模型时，模型白名单无法匹配。
func TestPolicyEnforcingFrameConn_WithoutCapturedFallbackPolicyMisses(t *testing.T) {
	// 同样使用带 whitelist 的策略以观察 leak。
	svc := newWSFastPolicy(t, gpt55WhitelistFastPolicy())
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}

	followupFrame := []byte(`{"type":"response.create","service_tier":"priority"}`)
	inner := &fakePassthroughFrameConn{reads: [][]byte{followupFrame}}
	wrapper := &openAIWSPolicyEnforcingFrameConn{
		inner: inner,
		filter: func(msgType coderws.MessageType, payload []byte) ([]byte, *tierpolicy.BlockedError, error) {
			// 省略回退模型，检查空模型时的处理。
			model := openAIWSPassthroughPolicyModelForFrame(provider, payload)
			return gatewayws.ApplyServiceTierFrame(payload, model, svc.Input(context.Background(), provider, model))
		},
	}

	_, payload, err := wrapper.ReadFrame(context.Background())
	require.NoError(t, err)
	// Pre-fix: empty model misses ["gpt-5.5","gpt-5.5*"] whitelist → fallback=pass → service_tier kept.
	require.Contains(t, string(payload), `"service_tier"`,
		"sanity: without capturedSessionModel fallback the leak (D5) reproduces — confirms the fix is load-bearing")
}

// TestApplyOpenAIFastPolicyToBody_BlockShortCircuitsUpstream 验证 block 规则返回 OpenAIFastBlockedError，请求体保持原样。
// Chat Completions 和 Messages 调用方据此结束请求。
func TestApplyOpenAIFastPolicyToBody_BlockShortCircuitsUpstream(t *testing.T) {
	settings := &tierpolicy.OpenAIFastPolicySettings{
		Rules: []tierpolicy.OpenAIFastPolicyRule{{
			ServiceTier:    tierpolicy.OpenAIFastTierPriority,
			Action:         claude.BetaPolicyActionBlock,
			Scope:          claude.BetaPolicyScopeAll,
			ErrorMessage:   "priority blocked",
			ModelWhitelist: []string{"gpt-5.5"},
			FallbackAction: claude.BetaPolicyActionPass,
		}},
	}
	svc := newWSFastPolicy(t, settings)
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}

	body := []byte(`{"model":"gpt-5.5","service_tier":"priority","input":[]}`)
	updated, err := tierpolicy.ApplyBody(body, svc.Input(context.Background(), provider, "gpt-5.5"))
	require.Error(t, err)
	var blocked *tierpolicy.BlockedError
	require.True(t, errors.As(err, &blocked), "block must surface as typed error so caller can skip upstream HTTP request")
	require.Equal(t, "priority blocked", blocked.Message)
	require.Equal(t, string(body), string(updated), "block must not mutate body")
}

// TestForwardAsAnthropicMessages_BetaFastModePassesOpenAIFastPolicyByDefault 验证 Anthropic fast-mode 检测后设置 BetaFastMode，
// 转换为 Responses 的 ServiceTier=priority，再按默认 Fast 策略透传。测试调用内部转换和策略函数。
func TestForwardAsAnthropicMessages_BetaFastModePassesOpenAIFastPolicyByDefault(t *testing.T) {
	svc := newWSFastPolicy(t, tierpolicy.Default())
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}

	// Step 1: parse Anthropic request (mirrors openai_gateway_messages.go:38-50).
	anthropicBody := []byte(`{"model":"gpt-5.5","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}`)
	var anthropicReq protocolanthropic.AnthropicRequest
	require.NoError(t, json.Unmarshal(anthropicBody, &anthropicReq))
	responsesReq, err := protocolbridge.AnthropicToResponses(&anthropicReq, protocolforward.ConversionOptionsForModel(anthropicReq.Model))
	require.NoError(t, err)

	// Step 2: BetaFastMode header → service_tier="priority" (mirrors line 58-61).
	headers := http.Header{}
	headers.Set("anthropic-beta", claude.BetaFastMode)
	require.True(t, claude.ContainsBetaToken(headers.Get("anthropic-beta"), claude.BetaFastMode))
	responsesReq.ServiceTier = "priority"
	responsesReq.Model = "gpt-5.5"

	// Step 3: marshal & apply fast policy (mirrors line 78 + 149).
	responsesBody, err := json.Marshal(responsesReq)
	require.NoError(t, err)
	require.Equal(t, "priority", gjson.GetBytes(responsesBody, "service_tier").String(), "前置：beta 翻译应当注入 priority")

	upstreamBody, policyErr := tierpolicy.ApplyBody(responsesBody, svc.Input(context.Background(), provider, "gpt-5.5"))
	require.NoError(t, policyErr)

	// 默认策略保留请求指定的 fast/priority。
	require.Equal(t, "priority", gjson.GetBytes(upstreamBody, "service_tier").String(),
		"default policy should pass service_tier=priority through to upstream")
}

// TestPolicyEnforcingFrameConn_SessionUpdateRotatesCapturedModel 验证 session.update 更新 capturedSessionModel。
// 客户端首帧使用白名单外的 gpt-4o，更新为 gpt-5.5 后，省略 model 的 response.create 继承 gpt-5.5，策略据此过滤 service_tier。
func TestPolicyEnforcingFrameConn_SessionUpdateRotatesCapturedModel(t *testing.T) {
	svc := newWSFastPolicy(t, gpt55WhitelistFastPolicy())
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}

	// 首帧使用白名单外的模型，默认 fallback=pass 保留 service_tier。
	first := []byte(`{"type":"response.create","model":"gpt-4o","service_tier":"priority"}`)
	// Frame 2: session.update rotates the session model to gpt-5.5.
	rotate := []byte(`{"type":"session.update","session":{"model":"gpt-5.5"}}`)
	// 第三帧省略 model，继承 gpt-5.5。
	followup := []byte(`{"type":"response.create","service_tier":"priority"}`)

	inner := &fakePassthroughFrameConn{reads: [][]byte{first, rotate, followup}}

	// Replicate the production wiring in openai_ws_v2_passthrough_adapter.go
	// so capturedSessionModel state is shared across frames.
	capturedSessionModel := openAIWSPassthroughPolicyModelForFrame(provider, first)
	require.Equal(t, "gpt-4o", capturedSessionModel)
	wrapper := &openAIWSPolicyEnforcingFrameConn{
		inner: inner,
		filter: func(msgType coderws.MessageType, payload []byte) ([]byte, *tierpolicy.BlockedError, error) {
			if msgType != coderws.MessageText {
				return payload, nil, nil
			}
			if updated := openAIWSPassthroughPolicyModelFromSessionFrame(provider, payload); updated != "" {
				capturedSessionModel = updated
			}
			model := openAIWSPassthroughPolicyModelForFrame(provider, payload)
			if model == "" {
				model = capturedSessionModel
			}
			return gatewayws.ApplyServiceTierFrame(payload, model, svc.Input(context.Background(), provider, model))
		},
	}

	// Frame 1: gpt-4o miss whitelist → pass (service_tier preserved).
	_, payload1, err := wrapper.ReadFrame(context.Background())
	require.NoError(t, err)
	require.Contains(t, string(payload1), `"service_tier"`, "frame1: gpt-4o miss whitelist → pass keeps service_tier")

	// 第二帧 session.update 原样透传，并将 capturedSessionModel 更新为 gpt-5.5。
	_, payload2, err := wrapper.ReadFrame(context.Background())
	require.NoError(t, err)
	require.Equal(t, string(rotate), string(payload2), "session.update frame is forwarded verbatim")
	require.Equal(t, "gpt-5.5", capturedSessionModel, "fix1: session.update must rotate capturedSessionModel")

	// Frame 3: empty model + new captured gpt-5.5 → matches whitelist → filter.
	_, payload3, err := wrapper.ReadFrame(context.Background())
	require.NoError(t, err)
	require.NotContains(t, string(payload3), `"service_tier"`,
		"fix1: post-rotate response.create without model must use refreshed capturedSessionModel and trigger filter")
}

// TestPolicyModelFromSessionFrame_OnlySessionUpdate 检查 session.update 更新会话模型，其他事件返回空字符串。
func TestPolicyModelFromSessionFrame_OnlySessionUpdate(t *testing.T) {
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}

	// session.created 是服务端发往客户端的事件，客户端上行过滤器忽略该事件。
	created := []byte(`{"type":"session.created","session":{"model":"gpt-5.5"}}`)
	require.Empty(t, openAIWSPassthroughPolicyModelFromSessionFrame(provider, created))

	// Non-session.* frames must NOT trigger rotation.
	notSession := []byte(`{"type":"response.create","session":{"model":"gpt-9"}}`)
	require.Empty(t, openAIWSPassthroughPolicyModelFromSessionFrame(provider, notSession))

	// 缺少 session.model 时返回空字符串，调用方保留先前捕获的模型。
	noModel := []byte(`{"type":"session.update","session":{"voice":"alloy"}}`)
	require.Empty(t, openAIWSPassthroughPolicyModelFromSessionFrame(provider, noModel))
}

// TestApplyOpenAIFastPolicyToBody_PassNormalizesFastAlias 检查 pass 策略把 fast 规范化为上游支持的 priority。
func TestApplyOpenAIFastPolicyToBody_PassNormalizesFastAlias(t *testing.T) {
	// Use a policy that deliberately misses gpt-4 so the action is pass.
	settings := &tierpolicy.OpenAIFastPolicySettings{
		Rules: []tierpolicy.OpenAIFastPolicyRule{{
			ServiceTier:    tierpolicy.OpenAIFastTierPriority,
			Action:         claude.BetaPolicyActionFilter,
			Scope:          claude.BetaPolicyScopeAll,
			ModelWhitelist: []string{"gpt-5.5"},
			FallbackAction: claude.BetaPolicyActionPass,
		}},
	}
	svc := newWSFastPolicy(t, settings)
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}

	// gpt-4 + "fast" → fallback pass. Body must be rewritten to "priority".
	body := []byte(`{"model":"gpt-4","service_tier":"fast"}`)
	updated, err := tierpolicy.ApplyBody(body, svc.Input(context.Background(), provider, "gpt-4"))
	require.NoError(t, err)
	require.Equal(t, "priority", gjson.GetBytes(updated, "service_tier").String(),
		"fix2: pass action must still normalize 'fast' → 'priority' so upstream OpenAI accepts the slug")

	// Already-canonical "priority" on pass: zero mutation (byte-equal).
	body = []byte(`{"model":"gpt-4","service_tier":"priority"}`)
	updated, err = tierpolicy.ApplyBody(body, svc.Input(context.Background(), provider, "gpt-4"))
	require.NoError(t, err)
	require.Equal(t, string(body), string(updated))

	// Mixed-case alias → normalized.
	body = []byte(`{"model":"gpt-4","service_tier":"  Fast  "}`)
	updated, err = tierpolicy.ApplyBody(body, svc.Input(context.Background(), provider, "gpt-4"))
	require.NoError(t, err)
	require.Equal(t, "priority", gjson.GetBytes(updated, "service_tier").String())

	// Unrecognized tier → still no-op (not normalized, since normTier == "").
	body = []byte(`{"model":"gpt-4","service_tier":"turbo"}`)
	updated, err = tierpolicy.ApplyBody(body, svc.Input(context.Background(), provider, "gpt-4"))
	require.NoError(t, err)
	require.Equal(t, string(body), string(updated))
}

// TestPassthroughBilling_PostFilterServiceTier 检查计费读取过滤后的 service_tier，过滤 priority 后计费值为 nil。
func TestPassthroughBilling_PostFilterServiceTier(t *testing.T) {
	svc := newWSFastPolicy(t, openAIFastFilterPriorityPolicy())
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}

	raw := []byte(`{"type":"response.create","model":"gpt-5.5","service_tier":"priority"}`)

	// 原始帧中的 priority 尚未过滤，计费值需要从过滤后的帧提取。
	pre := requeststate.ExtractOpenAIServiceTierFromBody(raw)
	require.NotNil(t, pre)
	require.Equal(t, "priority", *pre,
		"sanity: raw first frame carries priority that pre-fix billing would have reported")

	// 应用 gpt-5.5 + priority -> filter 策略。
	filtered, blocked, err := gatewayws.ApplyServiceTierFrame(raw, "gpt-5.5", svc.Input(context.Background(), provider, "gpt-5.5"))
	require.NoError(t, err)
	require.Nil(t, blocked)
	require.NotContains(t, string(filtered), `"service_tier"`)

	// Post-filter: extracting from the rewritten frame returns nil. This
	// is the value the adapter now passes to OpenAIForwardResult.ServiceTier;
	// any observed response tier remains separate for usage-time resolution.
	post := requeststate.ExtractOpenAIServiceTierFromBody(filtered)
	require.Nil(t, post, "fix3: post-filter extraction must preserve a nil outbound tier")

	// And the byte-level invariant the adapter relies on: filtering an
	// already-filtered frame is a no-op (idempotent), so re-running the
	// policy doesn't accidentally re-introduce the field.
	again, blocked2, err := gatewayws.ApplyServiceTierFrame(filtered, "gpt-5.5", svc.Input(context.Background(), provider, "gpt-5.5"))
	require.NoError(t, err)
	require.Nil(t, blocked2)
	require.Equal(t, string(filtered), string(again),
		"policy is idempotent: filtering an already-filtered frame leaves bytes unchanged")
}

// TestApplyOpenAIFastPolicyToBody_NonStringServiceTier 检查非字符串 service_tier 跳过策略并完整转发。
func TestApplyOpenAIFastPolicyToBody_NonStringServiceTier(t *testing.T) {
	svc := newWSFastPolicy(t, tierpolicy.Default())
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}

	// 数字 1 经 gjson.String() 转为字符串 “1”，规范化返回空字符串，策略跳过处理。
	cases := [][]byte{
		[]byte(`{"model":"gpt-5.5","service_tier":1}`),
		[]byte(`{"model":"gpt-5.5","service_tier":null}`),
		[]byte(`{"model":"gpt-5.5","service_tier":{"nested":"priority"}}`),
		[]byte(`{"model":"gpt-5.5","service_tier":["priority"]}`),
		[]byte(`{"model":"gpt-5.5","service_tier":true}`),
	}
	for _, body := range cases {
		updated, err := tierpolicy.ApplyBody(body, svc.Input(context.Background(), provider, "gpt-5.5"))
		require.NoError(t, err, "non-string service_tier must not error: %s", string(body))
		require.Equal(t, string(body), string(updated),
			"non-string service_tier must pass through unchanged: %s", string(body))
	}

	// Same guard for the WS response.create entry.
	for _, body := range cases {
		frame := body
		updated, blocked, err := gatewayws.ApplyServiceTierFrame(frame, "gpt-5.5", svc.Input(context.Background(), provider, "gpt-5.5"))
		require.NoError(t, err, "non-string service_tier ws frame must not error: %s", string(frame))
		require.Nil(t, blocked, "non-string service_tier must not trigger block: %s", string(frame))
		require.Equal(t, string(frame), string(updated),
			"non-string service_tier ws frame must pass through unchanged: %s", string(frame))
	}
}

// TestPassthroughBilling_MultiTurnServiceTierFollowsFilteredFrames 验证计费使用每轮过滤后的 service_tier。
// OpenAI Realtime / Responses WS 的 response.create 可逐轮指定 service_tier。Codex 的 build_responses_request 也会在每次请求中填写该字段。
// 过滤器在每个成功的 response.create 后更新 atomic.Pointer[string]。
// 第一轮 priority 命中过滤规则，计费值为 nil。第二轮 flex 透传，计费值为 flex。
// 第三轮省略 service_tier，按默认层级处理，计费值为 nil。response.cancel 中的同名字段保持计费值原样。
func TestPassthroughBilling_MultiTurnServiceTierFollowsFilteredFrames(t *testing.T) {
	svc := newWSFastPolicy(t, openAIFastFilterPriorityPolicy())
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}

	// 使用生产过滤闭包的逐帧 Store 行为，检查每轮计费值更新。
	var requestServiceTierPtr atomic.Pointer[string]
	capturedSessionModel := ""
	filter := func(msgType coderws.MessageType, payload []byte) ([]byte, *tierpolicy.BlockedError, error) {
		if msgType != coderws.MessageText {
			return payload, nil, nil
		}
		if updated := openAIWSPassthroughPolicyModelFromSessionFrame(provider, payload); updated != "" {
			capturedSessionModel = updated
		}
		model := openAIWSPassthroughPolicyModelForFrame(provider, payload)
		if model == "" {
			model = capturedSessionModel
		}
		out, blocked, policyErr := gatewayws.ApplyServiceTierFrame(payload, model, svc.Input(context.Background(), provider, model))
		if policyErr == nil && blocked == nil &&
			strings.TrimSpace(gjson.GetBytes(payload, "type").String()) == "response.create" {
			requestServiceTierPtr.Store(requeststate.ExtractOpenAIServiceTierFromBody(out))
		}
		return out, blocked, policyErr
	}

	// First-frame initialization mirrors the adapter: extract from the
	// post-filter payload so a filter-on-first-frame clears the outbound tier.
	firstFrame := []byte(`{"type":"response.create","model":"gpt-5.5","service_tier":"priority"}`)
	firstOut, firstBlocked, firstErr := gatewayws.ApplyServiceTierFrame(firstFrame, "gpt-5.5", svc.Input(context.Background(), provider, "gpt-5.5"))
	require.NoError(t, firstErr)
	require.Nil(t, firstBlocked)
	requestServiceTierPtr.Store(requeststate.ExtractOpenAIServiceTierFromBody(firstOut))
	capturedSessionModel = openAIWSPassthroughPolicyModelForFrame(provider, firstFrame)
	require.Nil(t, requestServiceTierPtr.Load(),
		"turn 1: filter strips service_tier=priority, leaving a nil outbound tier")

	// Turn 2: client switches to flex, should pass and update billing.
	turn2 := []byte(`{"type":"response.create","model":"gpt-5.5","service_tier":"flex"}`)
	out2, blocked2, err2 := filter(coderws.MessageText, turn2)
	require.NoError(t, err2)
	require.Nil(t, blocked2)
	require.Equal(t, "flex", gjson.GetBytes(out2, "service_tier").String(), "turn 2: flex must pass to upstream untouched")
	tier2 := requestServiceTierPtr.Load()
	require.NotNil(t, tier2, "turn 2: billing must update to reflect flex")
	require.Equal(t, "flex", *tier2)

	// A non-response.create frame with a stray service_tier-shaped field
	// must NOT overwrite the billing pointer (those frames don't carry
	// per-response service_tier in the Realtime spec).
	cancelFrame := []byte(`{"type":"response.cancel","service_tier":"priority"}`)
	_, blockedCancel, errCancel := filter(coderws.MessageText, cancelFrame)
	require.NoError(t, errCancel)
	require.Nil(t, blockedCancel)
	tierAfterCancel := requestServiceTierPtr.Load()
	require.NotNil(t, tierAfterCancel, "response.cancel must not clobber billing tier to nil")
	require.Equal(t, "flex", *tierAfterCancel,
		"non-response.create frames must not update billing tier even if they carry a service_tier-shaped field")

	// Turn 3: response.create without any service_tier. We deliberately
	// overwrite billing back to nil so it tracks what the upstream actually
	// sees on this turn (default tier).
	turn3 := []byte(`{"type":"response.create","model":"gpt-5.5"}`)
	out3, blocked3, err3 := filter(coderws.MessageText, turn3)
	require.NoError(t, err3)
	require.Nil(t, blocked3)
	require.Equal(t, string(turn3), string(out3), "turn 3 has no service_tier — filter must not mutate")
	require.Nil(t, requestServiceTierPtr.Load(),
		"turn 3: response.create without service_tier overwrites billing to nil to match upstream default")
}

func TestPassthroughUsageMeta_TracksReasoningEffortAcrossTurns(t *testing.T) {
	svc := newWSFastPolicy(t, tierpolicy.Default())
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}

	firstFrame := []byte(`{"type":"response.create","model":"gpt-5.5","reasoning":{"effort":"medium"},"service_tier":"priority"}`)
	meta := newOpenAIWSPassthroughUsageMeta("", firstFrame)
	capturedSessionModel := openAIWSPassthroughPolicyModelForFrame(provider, firstFrame)
	firstOut, firstBlocked, firstErr := gatewayws.ApplyServiceTierFrame(firstFrame, capturedSessionModel, svc.Input(context.Background(), provider, capturedSessionModel))
	require.NoError(t, firstErr)
	require.Nil(t, firstBlocked)
	meta.initFromFirstFrame(firstOut, capturedSessionModel)
	require.NotNil(t, meta.reasoningEffort.Load())
	require.Equal(t, "medium", *meta.reasoningEffort.Load())

	process := func(payload []byte) ([]byte, *tierpolicy.BlockedError, error) {
		if updated := openAIWSPassthroughPolicyModelFromSessionFrame(provider, payload); updated != "" {
			capturedSessionModel = updated
		}
		meta.updateSessionRequestModel(payload)
		requestModelForThisFrame := meta.requestModelForFrame(payload)
		model := openAIWSPassthroughPolicyModelForFrame(provider, payload)
		if model == "" {
			model = capturedSessionModel
		}
		out, blocked, policyErr := gatewayws.ApplyServiceTierFrame(payload, model, svc.Input(context.Background(), provider, model))
		if policyErr == nil && blocked == nil &&
			strings.TrimSpace(gjson.GetBytes(payload, "type").String()) == "response.create" {
			meta.updateFromResponseCreate(out, model, requestModelForThisFrame)
		}
		return out, blocked, policyErr
	}

	_, blockedSession, errSession := process([]byte(`{"type":"session.update","session":{"model":"gpt-5-high"}}`))
	require.NoError(t, errSession)
	require.Nil(t, blockedSession)
	require.NotNil(t, meta.reasoningEffort.Load())
	require.Equal(t, "medium", *meta.reasoningEffort.Load(), "session.update 只刷新后续 fallback model，不覆盖当前 turn metadata")

	_, blockedCancel, errCancel := process([]byte(`{"type":"response.cancel","reasoning_effort":"x-high"}`))
	require.NoError(t, errCancel)
	require.Nil(t, blockedCancel)
	require.NotNil(t, meta.reasoningEffort.Load())
	require.Equal(t, "medium", *meta.reasoningEffort.Load(), "非 response.create 帧不能污染当前 turn metadata")

	_, blockedFlat, errFlat := process([]byte(`{"type":"response.create","reasoning_effort":"x-high"}`))
	require.NoError(t, errFlat)
	require.Nil(t, blockedFlat)
	require.NotNil(t, meta.reasoningEffort.Load())
	require.Equal(t, "xhigh", *meta.reasoningEffort.Load(), "flat reasoning_effort 必须进入 passthrough usage metadata")

	_, blockedMax, errMax := process([]byte(`{"type":"response.create","model":"deepseek-v4-flash","reasoning":{"effort":"max"}}`))
	require.NoError(t, errMax)
	require.Nil(t, blockedMax)
	require.NotNil(t, meta.reasoningEffort.Load())
	require.Equal(t, "max", *meta.reasoningEffort.Load(), "第三方模型显式 max 必须进入 passthrough usage metadata")

	_, blockedClear, errClear := process([]byte(`{"type":"response.create","model":"gpt-4o"}`))
	require.NoError(t, errClear)
	require.Nil(t, blockedClear)
	require.Nil(t, meta.reasoningEffort.Load(), "新的 response.create 无 effort 且无可推导后缀时必须清空旧值")
}

// TestPassthroughBilling_BlockedFrameDoesNotMutateServiceTier 检查被阻止的帧继续使用上一轮计费层级。
func TestPassthroughBilling_BlockedFrameDoesNotMutateServiceTier(t *testing.T) {
	blockSettings := &tierpolicy.OpenAIFastPolicySettings{
		Rules: []tierpolicy.OpenAIFastPolicyRule{{
			ServiceTier:    tierpolicy.OpenAIFastTierPriority,
			Action:         claude.BetaPolicyActionBlock,
			Scope:          claude.BetaPolicyScopeAll,
			ErrorMessage:   "blocked",
			ModelWhitelist: []string{"gpt-5.5"},
			FallbackAction: claude.BetaPolicyActionPass,
		}},
	}
	svc := newWSFastPolicy(t, blockSettings)
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}}

	var requestServiceTierPtr atomic.Pointer[string]
	flexValue := "flex"
	requestServiceTierPtr.Store(&flexValue) // simulate prior turn billed as flex

	filter := func(msgType coderws.MessageType, payload []byte) ([]byte, *tierpolicy.BlockedError, error) {
		if msgType != coderws.MessageText {
			return payload, nil, nil
		}
		out, blocked, policyErr := gatewayws.ApplyServiceTierFrame(payload, "gpt-5.5", svc.Input(context.Background(), provider, "gpt-5.5"))
		if policyErr == nil && blocked == nil &&
			strings.TrimSpace(gjson.GetBytes(payload, "type").String()) == "response.create" {
			requestServiceTierPtr.Store(requeststate.ExtractOpenAIServiceTierFromBody(out))
		}
		return out, blocked, policyErr
	}

	frame := []byte(`{"type":"response.create","model":"gpt-5.5","service_tier":"priority"}`)
	_, blocked, err := filter(coderws.MessageText, frame)
	require.NoError(t, err)
	require.NotNil(t, blocked, "policy must block this frame")

	tier := requestServiceTierPtr.Load()
	require.NotNil(t, tier, "blocked frame must not clobber prior billing tier to nil")
	require.Equal(t, "flex", *tier,
		"blocked frame is never sent upstream; billing must retain the previous turn's tier")
}

func (c *openAIWSPolicyEnforcingFrameConn) runtime() gatewayws.FrameConn {
	c.once.Do(func() {
		var filter func(int, []byte) ([]byte, *gatewayws.PolicyBlocked, error)
		if c.filter != nil {
			filter = func(typ int, body []byte) ([]byte, *gatewayws.PolicyBlocked, error) {
				out, blocked, err := c.filter(coderws.MessageType(typ), body)
				if blocked == nil {
					return out, nil, err
				}
				return out, &gatewayws.PolicyBlocked{Message: blocked.Message, Cause: blocked}, err
			}
		}
		var writeFilter func(int, []byte) ([]byte, error)
		if c.writeFilter != nil {
			writeFilter = func(typ int, body []byte) ([]byte, error) { return c.writeFilter(coderws.MessageType(typ), body) }
		}
		var block func(*gatewayws.PolicyBlocked)
		if c.onBlock != nil {
			block = func(b *gatewayws.PolicyBlocked) {
				original, ok := b.Cause.(*tierpolicy.BlockedError)
				if !ok {
					original = &tierpolicy.BlockedError{Message: b.Message}
				}
				c.onBlock(original)
			}
		}
		c.core = gatewayws.NewPolicyFrames(openAIWSCoreFrames{c.inner}, filter, writeFilter, block, func(status int, reason string, err error) error {
			return gatewayhttp.NewOpenAIWSClientCloseError(coderws.StatusCode(status), reason, err)
		}, openai.ErrWSConnClosed)
	})
	return c.core
}

func (c *openAIWSPolicyEnforcingFrameConn) ReadFrame(ctx context.Context) (coderws.MessageType, []byte, error) {
	if c == nil || c.inner == nil {
		return coderws.MessageText, nil, openai.ErrWSConnClosed
	}
	typ, body, err := c.runtime().ReadFrame(ctx)
	return coderws.MessageType(typ), body, err
}

func (c *openAIWSPolicyEnforcingFrameConn) WriteFrame(ctx context.Context, typ coderws.MessageType, body []byte) error {
	if c == nil || c.inner == nil {
		return openai.ErrWSConnClosed
	}
	return c.runtime().WriteFrame(ctx, int(typ), body)
}

func (c *openAIWSPolicyEnforcingFrameConn) Close() error {
	if c == nil || c.inner == nil {
		return nil
	}
	return c.runtime().Close()
}

func (c *openAIWSClientFrameConn) markTurnStarted() {
	if c != nil {
		gatewayws.TurnActivity{Waiting: &c.waitingForNextTurn, Started: c.interTurnStarted}.MarkStarted()
	}
}

func (c *openAIWSClientFrameConn) markTurnCompleted() {
	if c != nil {
		gatewayws.TurnActivity{Waiting: &c.waitingForNextTurn, Started: c.interTurnStarted}.MarkCompleted()
	}
}

// openAIWSPassthroughPolicyModelForFrame 读取单帧策略匹配使用的上游模型，按提供商设置执行映射和规范化。
func openAIWSPassthroughPolicyModelForFrame(provider *gatewayprovider.ExecutionProvider, payload []byte) string {
	if provider == nil || len(payload) == 0 {
		return ""
	}
	original := strings.TrimSpace(gjson.GetBytes(payload, "model").String())
	if original == "" {
		return ""
	}
	if provider.View().IsOpenAIPassthroughEnabled() {
		return original
	}
	return gatewayprovider.ExecutionModelPolicy(provider).NormalizeOpenAI(gatewayprovider.ExecutionModelPolicy(provider).Mapped(original))
}

// openAIWSPassthroughPolicyModelFromSessionFrame 从 session.update 的 session.model 解析上游模型。
// 其他事件或缺少该字段时返回空字符串。上行过滤器据此更新 capturedSessionModel。
// 客户端从 gpt-4o 更新为 gpt-5.5 后，省略 model 的 response.create 使用 gpt-5.5，Fast 策略按更新后的模型匹配。
func openAIWSPassthroughPolicyModelFromSessionFrame(provider *gatewayprovider.ExecutionProvider, payload []byte) string {
	if provider == nil || len(payload) == 0 {
		return ""
	}
	frameType := strings.TrimSpace(gjson.GetBytes(payload, "type").String())
	if frameType != "session.update" {
		return ""
	}
	original := strings.TrimSpace(gjson.GetBytes(payload, "session.model").String())
	if original == "" {
		return ""
	}
	if provider.View().IsOpenAIPassthroughEnabled() {
		return original
	}
	return gatewayprovider.ExecutionModelPolicy(provider).NormalizeOpenAI(gatewayprovider.ExecutionModelPolicy(provider).Mapped(original))
}

func newOpenAIWSPassthroughUsageMeta(model string, body []byte) *openAIWSPassthroughUsageMeta {
	meta := gatewayws.NewUsageMeta(model, body, gatewayws.RequestUsageDecoder{})
	return &openAIWSPassthroughUsageMeta{UsageMeta: meta, reasoningEffort: &meta.ReasoningEffort}
}

func (m *openAIWSPassthroughUsageMeta) initFromFirstFrame(body []byte, model string) {
	if m != nil {
		m.InitFromFirstFrame(body)
	}
}

func (m *openAIWSPassthroughUsageMeta) updateSessionRequestModel(body []byte) {
	if m != nil {
		m.UpdateSessionRequestModel(body)
	}
}

func (m *openAIWSPassthroughUsageMeta) updateFromResponseCreate(body []byte, mapped, requested string) {
	if m != nil {
		m.UpdateFromResponseCreate(body)
	}
}

func newOpenAIWSPassthroughTurnLifecycle(inFlight bool) *openAIWSPassthroughTurnLifecycle {
	return &openAIWSPassthroughTurnLifecycle{gatewayws.NewTurnLifecycle(inFlight)}
}

func (l *openAIWSPassthroughTurnLifecycle) beginResponseCreate(fn func()) bool {
	if l == nil {
		return false
	}
	return l.BeginResponseCreate(fn)
}

func (l *openAIWSPassthroughTurnLifecycle) beginTerminalWrite() {
	if l != nil {
		l.BeginTerminalWrite()
	}
}

func (l *openAIWSPassthroughTurnLifecycle) finishTerminalWrite(ok bool, fn func()) {
	if l != nil {
		l.FinishTerminalWrite(ok, fn)
	}
}

// requestModelForFrame 为竞争测试读取会话的原子模型元数据。
func (m *openAIWSPassthroughUsageMeta) requestModelForFrame(body []byte) string {
	if m == nil {
		return gatewayws.RequestModelForFrame(body)
	}
	return m.RequestModelForFrame(body)
}

func TestOpenAIWSPassthroughTurnLifecycle_SerializesTerminalCommitAndNextTurn(t *testing.T) {
	clientFrameConn := &openAIWSClientFrameConn{interTurnStarted: make(chan struct{}, 1)}
	clientFrameConn.markTurnCompleted()
	lifecycle := newOpenAIWSPassthroughTurnLifecycle(true)
	lifecycle.beginTerminalWrite()

	admitted := make(chan bool, 1)
	go func() {
		admitted <- lifecycle.beginResponseCreate(clientFrameConn.markTurnStarted)
	}()
	select {
	case <-admitted:
		t.Fatal("next response.create was admitted before terminal commit completed")
	case <-time.After(50 * time.Millisecond):
	}

	lifecycle.finishTerminalWrite(true, clientFrameConn.markTurnCompleted)
	select {
	case ok := <-admitted:
		require.True(t, ok)
	case <-time.After(time.Second):
		t.Fatal("next response.create remained blocked after terminal commit")
	}
	require.False(t, clientFrameConn.waitingForNextTurn.Load(), "accepted next turn must win over terminal idle state")

	lifecycle = newOpenAIWSPassthroughTurnLifecycle(true)
	lifecycle.beginTerminalWrite()
	admitted = make(chan bool, 1)
	go func() {
		admitted <- lifecycle.beginResponseCreate(nil)
	}()
	lifecycle.finishTerminalWrite(false, func() {
		t.Error("failed terminal write must not commit idle state")
	})
	require.False(t, <-admitted, "failed terminal write must keep the current turn in flight")
}
