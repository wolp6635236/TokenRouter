package composite

import (
	"maps"

	settingvalues "github.com/TokenFlux/TokenRouter/internal/settings"
	"github.com/TokenFlux/TokenRouter/internal/team"

	"github.com/TokenFlux/TokenRouter/internal/audit"
	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/creative"
	"github.com/TokenFlux/TokenRouter/internal/moderation"
	"github.com/TokenFlux/TokenRouter/internal/notification"
	"github.com/TokenFlux/TokenRouter/internal/ops"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/search"
	"github.com/TokenFlux/TokenRouter/internal/usage"

	"github.com/TokenFlux/TokenRouter/internal/scheduler"

	"github.com/TokenFlux/TokenRouter/internal/server/runtimeconfig"

	"github.com/TokenFlux/TokenRouter/internal/identity"

	"github.com/TokenFlux/TokenRouter/internal/gateway"

	"github.com/TokenFlux/TokenRouter/internal/payment"
	"github.com/TokenFlux/TokenRouter/internal/promotion"
	"github.com/TokenFlux/TokenRouter/internal/site"
)

// ReadOptions 包含各模块的设置读取器和当前运行快照。
type ReadOptions struct {
	OAuth              *identity.OAuthSettings
	Gateway            gateway.AdminSettingsRules
	Scheduler          scheduler.AdminDefaults
	DefaultBalance     func() float64
	DefaultConcurrency func() int
	Forwarded          func() runtimeconfig.ForwardedInput
	PublishModel       func(string)
}

// Parse 将传入设置和各模块的解析结果组合为管理快照。
func Parse(settings map[string]string, options ReadOptions) *Snapshot {
	prior := options.Forwarded()
	forwarded := runtimeconfig.ReadForwardedSettings(settings, prior)
	result := &Snapshot{
		StoredValues:              maps.Clone(settings),
		LocalizedSettings:         settingvalues.ReadLocalizedTexts(settings),
		APIKeyACLTrustForwardedIP: forwarded.APIKeyACLTrustForwardedIP,
		ForwardedClientIPHeaders:  forwarded.ForwardedClientIPHeaders,
		TeamEnabled:               settings[team.SettingKeyTeamEnabled] != "false",
	}
	result.ApplySearchAdminReadSettings(search.ReadAdminSettings(settings))
	result.ApplyOpsAdminReadSettings(ops.ReadAdminSettings(settings))
	result.ApplyPaymentAdminReadSettings(payment.ReadAdminSettings(settings))
	result.ApplyProviderAdminReadSettings(provider.ReadAdminSettings(settings))
	result.ApplyGatewayAdminReadSettings(gateway.ReadAdminSettings(settings, options.Gateway))
	result.ApplyBillingAdminReadSettings(billing.ReadAdminSettings(settings, options.DefaultBalance))
	result.ApplyUsageAdminReadSettings(usage.ReadAdminSettings(settings))
	result.ApplyAuditAdminReadSettings(audit.ReadAdminSettings(settings))
	result.ApplyCreativeAdminReadSettings(creative.ReadAdminSettings(settings))
	result.ApplyModerationAdminReadSettings(moderation.ReadAdminSettings(settings))
	result.ApplyRoutingAdminReadSettings(routing.ReadAdminSettings(settings))
	result.ApplyPromotionAdminReadSettings(promotion.ReadAdminSettings(settings))
	result.ApplyNotificationAdminReadSettings(notification.ReadAdminSettings(settings))
	result.ApplySiteAdminReadSettings(site.ReadAdminSettings(settings))
	result.ApplyIdentityAdminReadSettings(options.OAuth.ReadAdminSettings(settings, options.DefaultConcurrency))

	result.ApplySchedulerAdminReadSettings(scheduler.ReadAdminSettings(settings, options.Scheduler))

	// 读取管理快照时，通过注入的函数发布动态默认模型。
	if options.PublishModel != nil {
		options.PublishModel(result.GrokDefaultTextModel)
	}

	return result
}
