package composite

import (
	"encoding/json"
	"maps"

	"github.com/TokenFlux/TokenRouter/internal/audit"
	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/creative"
	"github.com/TokenFlux/TokenRouter/internal/gateway"
	"github.com/TokenFlux/TokenRouter/internal/gateway/ws"
	"github.com/TokenFlux/TokenRouter/internal/identity"
	"github.com/TokenFlux/TokenRouter/internal/moderation"
	"github.com/TokenFlux/TokenRouter/internal/notification"
	"github.com/TokenFlux/TokenRouter/internal/ops"
	"github.com/TokenFlux/TokenRouter/internal/payment"
	"github.com/TokenFlux/TokenRouter/internal/promotion"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/scheduler"
	"github.com/TokenFlux/TokenRouter/internal/search"
	"github.com/TokenFlux/TokenRouter/internal/server/runtimeconfig"
	settingvalues "github.com/TokenFlux/TokenRouter/internal/settings"
	"github.com/TokenFlux/TokenRouter/internal/site"
	"github.com/TokenFlux/TokenRouter/internal/team"
	"github.com/TokenFlux/TokenRouter/internal/usage"
)

// ReadOptions 包含各模块的设置读取器和当前运行快照。
type ReadOptions struct {
	ResponsesWS        *ws.Runtime
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
	defaults := ws.DefaultParameters()
	if options.ResponsesWS != nil {
		defaults = options.ResponsesWS.Defaults()
	}
	raw := settings[ws.SettingKey]
	if raw == "" {
		raw = "{}"
	}
	result.ResponsesWS = json.RawMessage(raw)
	effective, err := ws.ResolveParameters(defaults, raw)
	if err != nil {
		effective = defaults
		if options.ResponsesWS != nil {
			effective = options.ResponsesWS.Snapshot()
		}
		if !json.Valid([]byte(raw)) {
			result.ResponsesWS = json.RawMessage("{}")
		}
	}
	result.ResponsesWSEffective = effective
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

// ApplyAuditAdminReadSettings 将审计日志保留天数写入快照。
func (s *Snapshot) ApplyAuditAdminReadSettings(value *audit.AdminReadSettings) {
	s.AuditLogRetentionDays = value.AuditLogRetentionDays
}

// ApplyCreativeAdminReadSettings 将创作台开关、模型和任务并发数写入快照。
func (s *Snapshot) ApplyCreativeAdminReadSettings(value *creative.AdminReadSettings) {
	s.CreativeEnabled = value.CreativeEnabled
	s.CreativeModelSettings = value.CreativeModelSettings
	s.CreativeWorkerCount = value.CreativeWorkerCount
}

// ApplyModerationAdminReadSettings 将会话封禁和风控设置写入快照。
func (s *Snapshot) ApplyModerationAdminReadSettings(value *moderation.AdminReadSettings) {
	s.CyberSessionBlockEnabled = value.CyberSessionBlockEnabled
	s.CyberSessionBlockTTLSeconds = value.CyberSessionBlockTTLSeconds
	s.RiskControlEnabled = value.RiskControlEnabled
}

// ApplyNotificationAdminReadSettings 将 SMTP 连接、发件人和凭据状态写入快照。
func (s *Snapshot) ApplyNotificationAdminReadSettings(value *notification.AdminReadSettings) {
	s.SMTPFrom = value.SMTPFrom
	s.SMTPFromName = value.SMTPFromName
	s.SMTPHost = value.SMTPHost
	s.SMTPPassword = value.SMTPPassword
	s.SMTPPasswordConfigured = value.SMTPPasswordConfigured
	s.SMTPPort = value.SMTPPort
	s.SMTPUseTLS = value.SMTPUseTLS
	s.SMTPUsername = value.SMTPUsername
}

// ApplyOpsAdminReadSettings 将额度自动暂停和监控设置写入快照。
func (s *Snapshot) ApplyOpsAdminReadSettings(value *ops.AdminReadSettings) {
	s.OpenAIQuotaAutoPauseSettings = value.OpenAIQuotaAutoPauseSettings
	s.OpsMetricsIntervalSeconds = value.OpsMetricsIntervalSeconds
	s.OpsMonitoringEnabled = value.OpsMonitoringEnabled
	s.OpsRealtimeMonitoringEnabled = value.OpsRealtimeMonitoringEnabled
}

// ApplyPaymentAdminReadSettings 将支付宝和微信支付的展示方式及来源写入快照。
func (s *Snapshot) ApplyPaymentAdminReadSettings(value *payment.AdminReadSettings) {
	s.PaymentVisibleMethodAlipayEnabled = value.PaymentVisibleMethodAlipayEnabled
	s.PaymentVisibleMethodAlipaySource = value.PaymentVisibleMethodAlipaySource
	s.PaymentVisibleMethodWxpayEnabled = value.PaymentVisibleMethodWxpayEnabled
	s.PaymentVisibleMethodWxpaySource = value.PaymentVisibleMethodWxpaySource
}

// ApplyPromotionAdminReadSettings 将邀请、推广和返佣设置写入快照。
func (s *Snapshot) ApplyPromotionAdminReadSettings(value *promotion.AdminReadSettings) {
	s.AdminRechargeRebateEnabled = value.AdminRechargeRebateEnabled
	s.AffiliateEnabled = value.AffiliateEnabled
	s.AffiliateRebateDurationDays = value.AffiliateRebateDurationDays
	s.AffiliateRebateFreezeHours = value.AffiliateRebateFreezeHours
	s.AffiliateRebatePerInviteeCap = value.AffiliateRebatePerInviteeCap
	s.AffiliateRebateRate = value.AffiliateRebateRate
	s.InvitationCodeEnabled = value.InvitationCodeEnabled
	s.PromoCodeEnabled = value.PromoCodeEnabled
}

// ApplyProviderAdminReadSettings 将提供商额度通知和调度阈值写入快照。
func (s *Snapshot) ApplyProviderAdminReadSettings(value *provider.AdminReadSettings) {
	s.ProviderQuotaNotifyEmails = value.ProviderQuotaNotifyEmails
	s.ProviderQuotaNotifyEnabled = value.ProviderQuotaNotifyEnabled
	s.ProviderSchedulingThresholds = value.ProviderSchedulingThresholds
}

// ApplySearchAdminReadSettings 将搜索模拟开关写入快照。
func (s *Snapshot) ApplySearchAdminReadSettings(value *search.AdminReadSettings) {
	s.WebSearchEmulationEnabled = value.WebSearchEmulationEnabled
}

// ApplyUsageAdminReadSettings 将错误请求查看权限和用量排行设置写入快照。
func (s *Snapshot) ApplyUsageAdminReadSettings(value *usage.AdminReadSettings) {
	s.AllowUserViewErrorRequests = value.AllowUserViewErrorRequests
	s.UsageRankingEnabled = value.UsageRankingEnabled
	s.UsageRankingLimit = value.UsageRankingLimit
	s.UsageRankingShowActualCost = value.UsageRankingShowActualCost
	s.UsageRankingShowRequests = value.UsageRankingShowRequests
	s.UsageRankingShowTotalTokens = value.UsageRankingShowTotalTokens
	s.UsageRankingSortBy = value.UsageRankingSortBy
}
