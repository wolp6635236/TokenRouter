package provider

import (
	"context"
	"fmt"
	"time"
)

const (
	// INTERNAL 500 渐进惩罚：连续多轮全部返回特定 500 错误时的惩罚时长。
	Internal500PenaltyTier1Duration  = 30 * time.Minute // 第 1 轮：临时不可调度 30 分钟
	Internal500PenaltyTier2Duration  = 2 * time.Hour    // 第 2 轮：临时不可调度 2 小时
	Internal500PenaltyTier3Threshold = 3                // 第 3+ 轮：永久禁用

	// CreditsExhaustedKey 是 model_rate_limits 中标记积分耗尽的特殊 key。
	// 与普通模型限流完全同构：通过 SetModelRateLimit / isRateLimitActiveForKey 读写。
	CreditsExhaustedKey      = "AICredits"
	CreditsExhaustedDuration = 5 * time.Hour
)

type AntigravityHealthStore interface {
	SetError(context.Context, int64, string) error
	SetTempUnschedulable(context.Context, int64, time.Time, string) error
	SetModelRateLimit(context.Context, int64, string, time.Time, ...string) error
	UpdateExtra(context.Context, int64, map[string]any) error
}

type AntigravityHealth struct {
	Store             AntigravityHealthStore
	Counter           Internal500CounterCache
	Publish           func(context.Context, *Record) error
	ModelKeys         func(string) []string
	Error, Warn, Info func(string, ...any)
	Logf              func(string, ...any)
}

// Internal500CounterCache 追踪 Antigravity 提供商连续 INTERNAL 500 失败轮数。
type Internal500CounterCache interface {
	// IncrementInternal500Count 原子递增计数并返回当前值
	IncrementInternal500Count(ctx context.Context, providerID int64) (int64, error)
	// ResetInternal500Count 清零计数器（成功响应时调用）
	ResetInternal500Count(ctx context.Context, providerID int64) error
}

// ApplyInternal500Penalty 根据连续 INTERNAL 500 轮次数应用渐进惩罚
// count=1: temp_unschedulable 30 分钟
// count=2: temp_unschedulable 2 小时
// count>=3: SetError 永久禁用。
func (s *AntigravityHealth) ApplyInternal500Penalty(
	ctx context.Context, prefix string, provider *Record, count int64,
) {
	switch {
	case count >= int64(Internal500PenaltyTier3Threshold):
		reason := fmt.Sprintf("INTERNAL 500 consecutive failures: %d rounds", count)
		if err := s.Store.SetError(ctx, provider.ID, reason); err != nil {
			s.Error("internal500_set_error_failed", "provider_id", provider.ID, "error", err)
			return
		}
		s.Warn("internal500_provider_disabled",
			"provider_id", provider.ID, "provider_name", provider.Name, "consecutive_count", count)
	case count == 2:
		until := time.Now().Add(Internal500PenaltyTier2Duration)
		reason := fmt.Sprintf("INTERNAL 500 x%d (temp unsched %v)", count, Internal500PenaltyTier2Duration)
		if err := s.Store.SetTempUnschedulable(ctx, provider.ID, until, reason); err != nil {
			s.Error("internal500_temp_unsched_failed", "provider_id", provider.ID, "error", err)
			return
		}
		s.Warn("internal500_temp_unschedulable",
			"provider_id", provider.ID, "provider_name", provider.Name,
			"duration", Internal500PenaltyTier2Duration, "consecutive_count", count)
	case count == 1:
		until := time.Now().Add(Internal500PenaltyTier1Duration)
		reason := fmt.Sprintf("INTERNAL 500 x%d (temp unsched %v)", count, Internal500PenaltyTier1Duration)
		if err := s.Store.SetTempUnschedulable(ctx, provider.ID, until, reason); err != nil {
			s.Error("internal500_temp_unsched_failed", "provider_id", provider.ID, "error", err)
			return
		}
		s.Info("internal500_temp_unschedulable",
			"provider_id", provider.ID, "provider_name", provider.Name,
			"duration", Internal500PenaltyTier1Duration, "consecutive_count", count)
	}
}

// HandleInternal500RetryExhausted 处理 INTERNAL 500 重试耗尽：递增计数器并应用惩罚。
func (s *AntigravityHealth) HandleInternal500RetryExhausted(
	ctx context.Context, prefix string, provider *Record,
) {
	if s.Counter == nil {
		return
	}
	count, err := s.Counter.IncrementInternal500Count(ctx, provider.ID)
	if err != nil {
		s.Error("internal500_counter_increment_failed",
			"prefix", prefix, "provider_id", provider.ID, "error", err)
		return
	}
	s.ApplyInternal500Penalty(ctx, prefix, provider, count)
}

// ResetInternal500Counter 成功响应时清零 INTERNAL 500 计数器。
func (s *AntigravityHealth) ResetInternal500Counter(
	ctx context.Context, prefix string, providerID int64,
) {
	if s.Counter == nil {
		return
	}
	if err := s.Counter.ResetInternal500Count(ctx, providerID); err != nil {
		s.Error("internal500_counter_reset_failed",
			"prefix", prefix, "provider_id", providerID, "error", err)
	}
}

// SetCreditsExhausted 标记提供商积分耗尽：写入 model_rate_limits["AICredits"] + 更新缓存。
func (s *AntigravityHealth) SetCreditsExhausted(ctx context.Context, provider *Record) {
	if provider == nil || provider.ID == 0 {
		return
	}
	resetAt := time.Now().Add(CreditsExhaustedDuration)
	if err := s.Store.SetModelRateLimit(ctx, provider.ID, CreditsExhaustedKey, resetAt); err != nil {
		s.Logf("set credits exhausted failed: provider=%d err=%v", provider.ID, err)
		return
	}
	s.UpdateProviderModelRateLimitInCache(ctx, provider, CreditsExhaustedKey, resetAt)
	s.Logf("credits_exhausted_marked provider=%d reset_at=%s",
		provider.ID, resetAt.UTC().Format(time.RFC3339))
}

// ClearCreditsExhausted 清除提供商的 AICredits 限流 key。
func (s *AntigravityHealth) ClearCreditsExhausted(ctx context.Context, provider *Record) {
	if provider == nil || provider.ID == 0 || provider.Extra == nil {
		return
	}
	rawLimits, ok := provider.Extra["model_rate_limits"].(map[string]any)
	if !ok {
		return
	}
	if _, exists := rawLimits[CreditsExhaustedKey]; !exists {
		return
	}
	delete(rawLimits, CreditsExhaustedKey)
	provider.Extra["model_rate_limits"] = rawLimits
	if err := s.Store.UpdateExtra(ctx, provider.ID, map[string]any{
		"model_rate_limits": rawLimits,
	}); err != nil {
		s.Logf("clear credits exhausted failed: provider=%d err=%v", provider.ID, err)
	}
}

// SetModelRateLimitByModelName 使用官方模型 ID 设置模型级限流
// 直接使用上游返回的模型 ID（如 claude-sonnet-4-5）作为限流 key
// 返回是否已成功设置（若模型名为空或 repo 为 nil 将返回 false）。
func SetModelRateLimitByModelName(ctx context.Context, repo AntigravityHealthStore, providerID int64, modelName, prefix string, statusCode int, resetAt time.Time, afterSmartRetry bool, logf func(string, ...any)) bool {
	if repo == nil || modelName == "" {
		return false
	}
	// 使用官方模型 ID 作为 key。
	if err := repo.SetModelRateLimit(ctx, providerID, modelName, resetAt); err != nil {
		logf("%s status=%d model_rate_limit_failed model=%s error=%v", prefix, statusCode, modelName, err)
		return false
	}
	if afterSmartRetry {
		logf("%s status=%d model_rate_limited_after_smart_retry model=%s provider=%d reset_in=%v", prefix, statusCode, modelName, providerID, time.Until(resetAt).Truncate(time.Second))
	} else {
		logf("%s status=%d model_rate_limited model=%s provider=%d reset_in=%v", prefix, statusCode, modelName, providerID, time.Until(resetAt).Truncate(time.Second))
	}
	return true
}

func (s *AntigravityHealth) SetAntigravityModelRateLimits(ctx context.Context, repo AntigravityHealthStore, provider *Record, modelName, prefix string, statusCode int, resetAt time.Time, afterSmartRetry bool) bool {
	if provider == nil || repo == nil {
		return false
	}
	keys := s.ModelKeys(modelName)
	if len(keys) == 0 {
		return false
	}

	success := false
	for _, key := range keys {
		if SetModelRateLimitByModelName(ctx, repo, provider.ID, key, prefix, statusCode, resetAt, afterSmartRetry, s.Logf) {
			s.UpdateProviderModelRateLimitInCache(ctx, provider, key, resetAt)
			success = true
		}
	}
	return success
}

// UpdateProviderModelRateLimitInCache 立即更新 Redis 中提供商的模型限流状态。
func (s *AntigravityHealth) UpdateProviderModelRateLimitInCache(ctx context.Context, provider *Record, modelKey string, resetAt time.Time) {
	if s.Publish == nil || provider == nil || modelKey == "" {
		return
	}

	// 更新提供商对象的 Extra 字段
	if provider.Extra == nil {
		provider.Extra = make(map[string]any)
	}

	limits, _ := provider.Extra["model_rate_limits"].(map[string]any)
	if limits == nil {
		limits = make(map[string]any)
		provider.Extra["model_rate_limits"] = limits
	}

	limits[modelKey] = map[string]any{
		"rate_limited_at":     time.Now().UTC().Format(time.RFC3339),
		"rate_limit_reset_at": resetAt.UTC().Format(time.RFC3339),
	}

	// 更新 Redis 快照
	if err := s.Publish(ctx, provider); err != nil {
		s.Logf("[antigravity-Forward] cache_update_failed provider=%d model=%s err=%v", provider.ID, modelKey, err)
	}
}
