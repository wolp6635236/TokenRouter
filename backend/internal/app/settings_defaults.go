package app

import (
	"context"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/config"
	"github.com/TokenFlux/TokenRouter/internal/gateway"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/identity"
	identityprovider "github.com/TokenFlux/TokenRouter/internal/identity/provider"
	"github.com/TokenFlux/TokenRouter/internal/ops"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/scheduler"
	"github.com/TokenFlux/TokenRouter/internal/scheduler/policy"
	"github.com/TokenFlux/TokenRouter/internal/server/runtimeconfig"
	"github.com/TokenFlux/TokenRouter/internal/settings"
	"github.com/TokenFlux/TokenRouter/internal/upstream/anthropic"
	"github.com/TokenFlux/TokenRouter/internal/upstream/antigravity"
	"github.com/TokenFlux/TokenRouter/internal/upstream/grok"
)

// provideOAuthSettings 从启动配置提取认证需要的字段，交给 OAuth 设置读取器。
func provideOAuthSettings(store *settings.Store, cfg *config.Config) *identity.OAuthSettings {
	var defaults *identity.OAuthSettingsDefaults
	if cfg != nil {
		defaults = &identity.OAuthSettingsDefaults{LinuxDo: cfg.LinuxDo, DingTalk: cfg.DingTalk, OIDC: cfg.OIDC, WeChat: cfg.WeChat, GitHubOAuth: cfg.GitHubOAuth, GoogleOAuth: cfg.GoogleOAuth}
	}
	return identity.NewOAuthSettings(store, defaults, identityprovider.ResolveSettingsOIDCMetadata)
}

// provideGrantSettings 为注册赠送设置绑定 billing 套餐查询。
func provideGrantSettings(store *settings.Store, cfg *config.Config, plans *billing.Plans) *identity.GrantSettings {
	options := identity.GrantSettingsOptions{ValidatePlans: func(ctx context.Context, items []identity.DefaultSubscriptionSetting) error {
		return identity.ValidateDefaultSubscriptionPlans(ctx, items, plans.GetPlan)
	}}
	if cfg != nil {
		options.DefaultBalance = cfg.Default.UserBalance
		options.DefaultConcurrency = cfg.Default.UserConcurrency
	}
	return identity.NewGrantSettings(store, options)
}

// provideForwardedSettings 将启动配置中的可信代理和共享运行状态传给 server。
func provideForwardedSettings(store *settings.Store, cfg *config.Config) *runtimeconfig.ForwardedSettings {
	return runtimeconfig.NewForwardedSettings(store, runtimeconfig.ForwardedSettingsOptions{InitialTrust: cfg.Security.TrustForwardedIPForAPIKeyACL, TrustedProxiesConfigured: cfg.Server.TrustedProxiesConfigured, Headers: func() []string { return cfg.ForwardedClientIPSettings().Headers }, Publish: cfg.SetForwardedClientIPSettings})
}

// provideSchedulerAdminDefaults 提取调度需要的进程配置，由 scheduler/policy 规范化。
func provideSchedulerAdminDefaults(cfg *config.Config) *scheduler.AdminDefaults {
	value := scheduler.DefaultAdminSettingsDefaults()
	if cfg != nil {
		source := cfg.Gateway.AdvancedScheduler
		value.TopK = source.LBTopK
		value.Weights = source.ScoreWeights
		value.Process.EwmaErrorRateAlpha = source.EWMAErrorRateAlpha
		value.Process.EwmaTTFTAlpha = source.EWMATTFTAlpha
		value.Process.StickyEscape = policy.NormalizeStickyEscape(policy.StickyEscapeConfig{Enabled: source.StickyEscapeEnabled, TtftMs: float64(source.StickyEscapeTTFTMs), ErrorRate: source.StickyEscapeErrorRate})
	}
	return &value
}

// provideGatewayAdminRules 返回各平台的规则函数。
func provideGatewayAdminRules() *gateway.AdminSettingsRules {
	return &gateway.AdminSettingsRules{GrokDefaultTextModel: grok.DefaultTextModel, NormalizeUserAgentVersion: antigravity.NormalizeUserAgentVersion, ValidateClaudePromptBlocks: anthropic.ValidateClaudeOAuthSystemPromptBlocksConfig}
}

// provideGatewaySettings 将平台默认值转换为配置参数，构造网关运行设置实例。
func provideGatewaySettings(store *settings.Store) *gateway.RuntimeSettings {
	return gateway.NewRuntimeSettings(store, settings.ErrSettingNotFound, func() *gateway.BetaPolicySettings {
		return gatewayprovider.GatewayBetaPolicy(anthropic.DefaultBetaPolicySettings())
	}, gateway.ClientSettingsOptions{NormalizeUserAgentVersion: antigravity.NormalizeUserAgentVersion, DefaultUserAgentVersion: antigravity.GetDefaultUserAgentVersion})
}

// provideQuotaSettings 通过 Ops 读取共享 JSON 中的配额设置，传给提供商模块。
func provideQuotaSettings(store *settings.Store) *provider.QuotaSettingsCache {
	return provider.NewQuotaSettingsCache(store, settings.ErrSettingNotFound, ops.ParseRuntimeQuotaAutoPauseSettings)
}
