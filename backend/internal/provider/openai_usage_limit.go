package provider

import (
	"context"
	"strings"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/protocol/openai"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

// OpenAIPlanWriter 按字段更新本次观测的套餐信息，其余字段保持当前值。
type OpenAIPlanWriter interface {
	BulkUpdate(context.Context, []int64, ProviderBulkUpdate) (int64, error)
}

// OpenAI429ResetTime 从观测到的窗口选择原提供商恢复时间。
// 返回 nil 表示无法从响应头中确定重置时间。
func OpenAI429ResetTime(snapshot *openai.OpenAICodexUsageSnapshot, clock func() time.Time, info func(string, ...any)) *time.Time {
	if snapshot == nil {
		return nil
	}

	normalized := snapshot.Normalize()
	if normalized == nil {
		return nil
	}

	now := clock()

	// 判断哪个限制被触发（used_percent >= 100）
	is7dExhausted := normalized.Used7dPercent != nil && *normalized.Used7dPercent >= 100
	is5hExhausted := normalized.Used5hPercent != nil && *normalized.Used5hPercent >= 100

	// 优先使用被触发限制的重置时间
	if is7dExhausted && normalized.Reset7dSeconds != nil {
		resetAt := now.Add(time.Duration(*normalized.Reset7dSeconds) * time.Second)
		info("openai_429_7d_limit_exhausted", "reset_after_seconds", *normalized.Reset7dSeconds, "reset_at", resetAt)
		return &resetAt
	}
	if is5hExhausted && normalized.Reset5hSeconds != nil {
		resetAt := now.Add(time.Duration(*normalized.Reset5hSeconds) * time.Second)
		info("openai_429_5h_limit_exhausted", "reset_after_seconds", *normalized.Reset5hSeconds, "reset_at", resetAt)
		return &resetAt
	}

	// 都未达到 100%但收到 429，使用较长的重置时间
	var maxResetSecs int
	if normalized.Reset7dSeconds != nil && *normalized.Reset7dSeconds > maxResetSecs {
		maxResetSecs = *normalized.Reset7dSeconds
	}
	if normalized.Reset5hSeconds != nil && *normalized.Reset5hSeconds > maxResetSecs {
		maxResetSecs = *normalized.Reset5hSeconds
	}
	if maxResetSecs > 0 {
		resetAt := now.Add(time.Duration(maxResetSecs) * time.Second)
		info("openai_429_using_max_reset", "max_reset_seconds", maxResetSecs, "reset_at", resetAt)
		return &resetAt
	}

	return nil
}

// PersistOpenAIObservedPlan 将 429 错误中观测到的计划类型同步到提供商凭据。
func PersistOpenAIObservedPlan(ctx context.Context, repo OpenAIPlanWriter, provider *Record, planType string, info, warn func(string, ...any)) {
	if repo == nil || provider == nil || provider.Platform != capability.PlatformOpenAI {
		return
	}
	// Spark 影子的 credentials 不保存凭据或 plan_type。此路径直接批量写入，必须在写入前排除影子。
	// plan_type 由母提供商在自己的请求中维护。
	if provider.IsCredentialShadow() {
		return
	}

	if planType == "" {
		return
	}

	current := strings.TrimSpace(provider.GetCredential("plan_type"))
	if strings.EqualFold(current, planType) {
		return
	}

	if _, err := repo.BulkUpdate(ctx, []int64{provider.ID}, ProviderBulkUpdate{
		Credentials: map[string]any{"plan_type": planType},
	}); err != nil {
		warn("openai_429_plan_type_sync_failed", "provider_id", provider.ID, "plan_type", planType, "error", err)
		return
	}

	if provider.Credentials == nil {
		provider.Credentials = make(map[string]any, 1)
	}
	provider.Credentials["plan_type"] = planType
	info("openai_429_plan_type_synced", "provider_id", provider.ID, "previous_plan_type", current, "plan_type", planType)
}
