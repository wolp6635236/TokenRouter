package provider

import (
	"context"
	"strings"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	"github.com/TokenFlux/TokenRouter/internal/gateway/tierpolicy"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/upstream/anthropic"
)

var openAIFastPolicyCtxKey = openAIFastPolicyCtxKeyType{}

// ExecutionFastPolicy 在处理请求时读取 Fast 设置和价格。
type ExecutionFastPolicy struct {
	Readers *RuntimeReaders
	Prices  *billing.PriceResolver
}

// openAIFastPolicyCtxKeyType 标识 WebSocket 会话共用的 Fast 策略快照。
// 同一会话的各帧复用该快照，策略更新在新会话中生效。
// 管理员可断开会话，使重连后的请求读取新策略。
type openAIFastPolicyCtxKeyType struct{}

// Evaluate 返回指定提供商、模型和 service_tier 应执行的动作及错误消息。
// 策略服务不可用或没有规则命中时返回 pass，调用方可安全地直接放行。
//
// 匹配规则：
//   - Scope 按提供商类型过滤（all / oauth / apikey / bedrock）
//   - UserIDs 非空时按 API Key 所属的可信用户 ID 过滤
//   - ServiceTier 必须为空、all 或等于归一化后的 tier
//   - ModelWhitelist 将规则限制到指定模型，FallbackAction 处理未匹配模型
//   - 用户专属规则优先于全局规则，两组内部均保持配置顺序并首条命中
//
// 一次请求携带一个 service_tier，filter 删除该字段。同一 scope 和 tier 下
// 多条模型白名单重叠时，管理员通过配置顺序决定先应用哪条规则。
func (s *ExecutionFastPolicy) Evaluate(ctx context.Context, provider *ExecutionProvider, model, serviceTier string) (action, errMsg string) {
	if s == nil || s.Readers == nil {
		return anthropic.BetaPolicyActionPass, ""
	}
	tier := strings.ToLower(strings.TrimSpace(serviceTier))
	if tier == "" {
		return anthropic.BetaPolicyActionPass, ""
	}
	settings := FastPolicySettingsFromContext(ctx)
	if settings == nil {
		fetched, err := s.Readers.Gateway.GetOpenAIFastPolicySettings(ctx)
		if err != nil || fetched == nil {
			return anthropic.BetaPolicyActionPass, ""
		}
		settings = fetched
	}
	return tierpolicy.Evaluate(settings, openAIFastPolicyUserID(ctx), provider != nil && provider.View().IsOAuth(), provider != nil && provider.View().IsBedrock(), model, tier)
}

// openAIFastPolicyUserID 从可信请求上下文读取 API Key 所属用户 ID。
func openAIFastPolicyUserID(ctx context.Context) int64 {
	if ctx == nil {
		return 0
	}
	access, _ := apikey.AccessSnapshotFromContext(ctx)
	userID := access.PayerUserID
	if userID <= 0 {
		return 0
	}
	return userID
}

// WithFastPolicyContext 将一份 settings 快照绑定到 context，供该 ctx
// 衍生 goroutine 中的 Evaluate 复用。
func WithFastPolicyContext(ctx context.Context, settings *tierpolicy.OpenAIFastPolicySettings) context.Context {
	if ctx == nil || settings == nil {
		return ctx
	}
	return context.WithValue(ctx, openAIFastPolicyCtxKey, settings)
}

func FastPolicySettingsFromContext(ctx context.Context) *tierpolicy.OpenAIFastPolicySettings {
	if ctx == nil {
		return nil
	}
	if v, ok := ctx.Value(openAIFastPolicyCtxKey).(*tierpolicy.OpenAIFastPolicySettings); ok {
		return v
	}
	return nil
}

// GroupFastPolicy 只信任认证链路完整加载的分组，并限于 OpenAI 提供商。
func GroupFastPolicy(ctx context.Context, provider *ExecutionProvider) string {
	if ctx == nil || provider == nil || !provider.View().IsOpenAI() {
		return routing.GroupOpenAIFastPolicyFollowRequest
	}
	group, _ := requeststate.GroupFromContext(ctx)
	if !routing.IsGroupContextValid(group) {
		return routing.GroupOpenAIFastPolicyFollowRequest
	}
	return group.EffectiveOpenAIFastPolicy()
}

func (s *ExecutionFastPolicy) Input(ctx context.Context, value *ExecutionProvider, model string) tierpolicy.DecisionInput {
	return tierpolicy.DecisionInput{
		Model: model, GroupPolicy: GroupFastPolicy(ctx, value), OpenAI: value != nil && value.View().IsOpenAI(),
		Evaluate: func(tier string) (string, string) { return s.Evaluate(ctx, value, model, tier) },
		KeyPolicy: func() string {
			return APIKeyFastModePolicy(ctx)
		},
		ForceOnSupported: func() bool { return s.ForceOnSupported(ctx, value, model) },
	}
}

func (s *ExecutionFastPolicy) ForceOnSupported(ctx context.Context, provider *ExecutionProvider, model string) bool {
	return provider != nil && provider.View().IsOpenAI() &&
		SupportsFastMode(ctx, s.Prices, model)
}
