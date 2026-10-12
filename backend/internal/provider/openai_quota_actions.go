package provider

import (
	"context"
	"time"

	wire "github.com/TokenFlux/TokenRouter/internal/protocol/openai"
)

const (
	OpenAIQuotaResetWarningCacheRefreshFailed     = "reset_credit_cache_refresh_failed"
	OpenAIQuotaResetWarningProviderRecoveryFailed = "provider_state_recovery_failed"
	OpenAIQuotaResetWarningProviderRefreshFailed  = "provider_state_refresh_failed"
	OpenAIQuotaResetPostProcessTimeout            = 8 * time.Second
)

type OpenAIQuotaOperations interface {
	QueryUsage(context.Context, int64) (*wire.OpenAIQuotaUsage, error)
	CacheResetCreditsSnapshot(context.Context, int64, *wire.OpenAIRateLimitResetCredits) error
	CachePostResetSnapshot(context.Context, int64, *wire.OpenAIQuotaUsage) error
	ResetCredit(context.Context, int64) (*wire.OpenAIQuotaResetResult, error)
}
type OpenAIQuotaRecoverer interface {
	RecoverProviderState(context.Context, int64, ProviderRecoveryOptions) (*SuccessfulTestRecovery, error)
}
type OpenAIQuotaProviderReader interface {
	GetProvider(context.Context, int64) (*Record, error)
}
type OpenAIQuotaOutcomeError struct{ Message string }

type OpenAIQuotaResetOutcome struct {
	wire.OpenAIQuotaResetResult
	Quota                  *wire.OpenAIQuotaUsage
	Provider               *Record `json:"-"`
	CacheRefreshed         bool
	ProviderStateRecovered bool
	WarningCode            string
}
type OpenAIQuotaRefreshOutcome struct {
	wire.OpenAIQuotaUsage
	CachePersisted bool
}
type OpenAIQuotaActions struct {
	Quota     OpenAIQuotaOperations
	Recovery  OpenAIQuotaRecoverer
	Providers OpenAIQuotaProviderReader
	Warn      func(string, ...any)
	activity  operationActivity
}

func (e *OpenAIQuotaOutcomeError) Error() string { return e.Message }

func NewOpenAIQuotaActions(quota OpenAIQuotaOperations, recovery OpenAIQuotaRecoverer, providers OpenAIQuotaProviderReader, warn func(string, ...any)) *OpenAIQuotaActions {
	return &OpenAIQuotaActions{Quota: quota, Recovery: recovery, Providers: providers, Warn: warn}
}

func (s *OpenAIQuotaActions) StopContext(ctx context.Context) error {
	return s.activity.stop(ctx, "OpenAIQuotaActions")
}

func (s *OpenAIQuotaActions) Reset(ctx context.Context, providerID int64) (*OpenAIQuotaResetOutcome, error) {
	runtimeCtx, done, err := s.activity.begin(context.WithoutCancel(ctx), ErrOpenAIQuotaStopped)
	if err != nil {
		return nil, err
	}
	defer done()
	// 消费阶段仍响应客户端取消，同时接受应用停止；成功消费后的收尾只屏蔽客户端取消。
	creditCtx, cancelCredit := context.WithCancel(ctx)
	stopRuntimeCancel := context.AfterFunc(runtimeCtx, cancelCredit)
	defer func() { stopRuntimeCancel(); cancelCredit() }()
	result, err := s.Quota.ResetCredit(creditCtx, providerID)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, &OpenAIQuotaOutcomeError{Message: "openai quota reset returned an empty result"}
	}

	resetResponse := OpenAIQuotaResetOutcome{OpenAIQuotaResetResult: *result}
	postCtx, cancelPost := context.WithTimeout(runtimeCtx, OpenAIQuotaResetPostProcessTimeout)
	defer cancelPost()

	// 重置次数一旦消费就不可退还，优先恢复提供商运行时状态，且不修改人工 schedulable 开关。
	if s.Recovery == nil {
		resetResponse.WarningCode = OpenAIQuotaResetWarningProviderRecoveryFailed
		return &resetResponse, nil
	}
	if _, err := s.Recovery.RecoverProviderState(postCtx, providerID, ProviderRecoveryOptions{
		InvalidateToken: true,
	}); err != nil {
		s.Warn("openai_quota_reset_provider_recovery_failed", "provider_id", providerID, "error", err)
		resetResponse.WarningCode = OpenAIQuotaResetWarningProviderRecoveryFailed
		return &resetResponse, nil
	}
	resetResponse.ProviderStateRecovered = true

	// 状态恢复后回读上游额度；回读或缓存失败不能掩盖已经完成的提供商恢复。
	usage, usageErr := s.Quota.QueryUsage(postCtx, providerID)
	switch {
	case usageErr != nil || usage == nil:
		s.Warn("openai_quota_reset_cache_refresh_failed", "provider_id", providerID, "error", usageErr)
		resetResponse.WarningCode = OpenAIQuotaResetWarningCacheRefreshFailed
	default:
		if err := s.Quota.CachePostResetSnapshot(postCtx, providerID, usage); err != nil {
			s.Warn("openai_quota_reset_cache_refresh_failed", "provider_id", providerID, "error", err)
			resetResponse.WarningCode = OpenAIQuotaResetWarningCacheRefreshFailed
		} else {
			resetResponse.Quota = usage
			resetResponse.CacheRefreshed = true
		}
	}

	// 返回恢复后的提供商数据，API 调用方据此更新限流展示。
	provider, err := s.Providers.GetProvider(postCtx, providerID)
	if err != nil {
		s.Warn("openai_quota_reset_provider_refresh_failed", "provider_id", providerID, "error", err)
		if resetResponse.WarningCode == "" {
			resetResponse.WarningCode = OpenAIQuotaResetWarningProviderRefreshFailed
		}
		return &resetResponse, nil
	}
	resetResponse.Provider = provider
	return &resetResponse, nil
}

func (s *OpenAIQuotaActions) Refresh(ctx context.Context, providerID int64) (*OpenAIQuotaRefreshOutcome, error) {
	ctx, done, err := s.activity.begin(ctx, ErrOpenAIQuotaStopped)
	if err != nil {
		return nil, err
	}
	defer done()
	usage, err := s.Quota.QueryUsage(ctx, providerID)
	if err != nil {
		return nil, err
	}
	if usage == nil {
		return nil, &OpenAIQuotaOutcomeError{Message: "openai quota query returned an empty result"}
	}

	refreshResponse := OpenAIQuotaRefreshOutcome{OpenAIQuotaUsage: *usage}
	// 快照写入失败属于部分成功：实时查询结果仍返回给前端，旧缓存保持不变。
	if err := s.Quota.CacheResetCreditsSnapshot(ctx, providerID, usage.RateLimitResetCredits); err != nil {
		s.Warn("openai_quota_reset_credit_cache_persist_failed", "provider_id", providerID, "error", err)
		return &refreshResponse, nil
	}
	refreshResponse.CachePersisted = true
	return &refreshResponse, nil
}
