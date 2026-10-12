package provider

import (
	"time"

	"github.com/TokenFlux/TokenRouter/internal/pkg/apperror"
	"github.com/TokenFlux/TokenRouter/internal/upstream/usageview"
)

var (
	ErrUpstreamUsageUnavailable = apperror.ServiceUnavailable(
		"UPSTREAM_USAGE_UNAVAILABLE", "upstream usage query service is unavailable",
	)
	ErrUpstreamUsageProviderInvalid = apperror.BadRequest(
		"UPSTREAM_USAGE_PROVIDER_INVALID", "provider is not a supported API key provider",
	)
	ErrUpstreamUsageProviderDisabled = apperror.New(apperror.Category(422),
		"UPSTREAM_USAGE_PROVIDER_DISABLED", "provider is disabled",
	)
	ErrUpstreamUsageDisabled = apperror.New(apperror.Category(422),
		"UPSTREAM_USAGE_DISABLED", "upstream usage query is disabled for this provider",
	)
	ErrUpstreamUsageUnsupported = usageview.ErrUpstreamUsageUnsupported

	ErrUpstreamUsageWalletUnavailable = usageview.ErrUpstreamUsageWalletUnavailable

	ErrUpstreamUsageTimeout         = usageview.ErrUpstreamUsageTimeout
	ErrUpstreamUsageInvalidResponse = usageview.ErrUpstreamUsageInvalidResponse
	ErrUpstreamUsageRequestFailed   = usageview.ErrUpstreamUsageRequestFailed
	ErrUpstreamUsageIdentityChanged = apperror.Conflict(
		"UPSTREAM_USAGE_IDENTITY_CHANGED", "provider credentials or connection settings changed during the query",
	)
	ErrUpstreamUsageConfigInvalid = usageview.ErrUpstreamUsageConfigInvalid
	ErrUpstreamUsageBatchInvalid  = apperror.BadRequest(
		"UPSTREAM_USAGE_BATCH_INVALID", "upstream usage batch request is invalid",
	)
	ErrUpstreamUsageBatchTooLarge = apperror.BadRequest(
		"UPSTREAM_USAGE_BATCH_TOO_LARGE", "too many providers in one upstream usage query",
	)
)

// UpstreamUsageQueryConfig 是提供商 Extra 中公开给管理员的非敏感查询配置。
type UpstreamUsageQueryConfig struct {
	Enabled bool   `json:"enabled"`
	Adapter string `json:"adapter"`
	BaseURL string `json:"base_url,omitempty"`
}

type UpstreamUsageAmount = usageview.UpstreamUsageAmount

type UpstreamUsageBalanceEntry = usageview.UpstreamUsageBalanceEntry

type UpstreamUsageLimit = usageview.UpstreamUsageLimit

type UpstreamUsageSubscription = usageview.UpstreamUsageSubscription

type UpstreamUsageInfo = usageview.UpstreamUsageInfo

// UpstreamUsageQueryResult 是管理员查询接口的成功响应。
type UpstreamUsageQueryResult struct {
	ProviderID   int64                       `json:"provider_id"`
	Adapter      string                      `json:"adapter"`
	ObservedAt   time.Time                   `json:"observed_at"`
	Provider     string                      `json:"provider,omitempty"`
	Mode         string                      `json:"mode,omitempty"`
	Unit         string                      `json:"unit,omitempty"`
	Balance      *UpstreamUsageAmount        `json:"balance,omitempty"`
	Balances     []UpstreamUsageBalanceEntry `json:"balances,omitempty"`
	Available    *bool                       `json:"available,omitempty"`
	Limits       []UpstreamUsageLimit        `json:"limits,omitempty"`
	Subscription *UpstreamUsageSubscription  `json:"subscription,omitempty"`
	ExpiresAt    *time.Time                  `json:"expires_at,omitempty"`
	// Usage 保存服务内部使用的归一化结果。
	// 管理员响应使用外层的展示字段。
	Usage *UpstreamUsageInfo `json:"-"`
}

// UpstreamUsageMetrics 按适配器和错误类别记录进程内的查询次数。
type UpstreamUsageMetrics struct {
	Counts map[string]int64 `json:"counts"`
}

// UpstreamUsageAdapterOption 用于前端或诊断页面展示可用适配器。
type UpstreamUsageAdapterOption struct {
	Name  string `json:"name"`
	Label string `json:"label"`
}
