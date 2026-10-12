package provider

import (
	"fmt"
	"time"
)

// 评分考虑缺省中性值、次窗口折扣和观测有效期。
const (
	openAIQuotaHeadroomNeutralFactor      = 0.5
	openAIQuotaHeadroomSecondaryLowRemain = 0.10
	openAIQuotaHeadroomSnapshotStaleAfter = 8 * time.Hour
)

// OpenAIQuotaHeadroomFactor 把 Codex quota 快照转换成 0..1 的调度因子。
// 7d/primary 剩余额度越高分越高；5h/secondary 接近耗尽时会折扣该分值。
func OpenAIQuotaHeadroomFactor(provider *Record, now time.Time) float64 {
	if provider == nil || len(provider.Extra) == 0 || openAIQuotaHeadroomSnapshotStale(provider.Extra, now) {
		return openAIQuotaHeadroomNeutralFactor
	}
	primaryUsedPercent, ok := ResolveProviderExtraNumber(provider.Extra, "codex_primary_used_percent", "codex_7d_used_percent")
	if !ok || openAIQuotaWindowResetAny(provider.Extra, now, "primary", "7d") {
		return openAIQuotaHeadroomNeutralFactor
	}

	factor := 1 - clampQuotaFactor(primaryUsedPercent/100)
	if secondaryUsedPercent, ok := ResolveProviderExtraNumber(provider.Extra, "codex_secondary_used_percent", "codex_5h_used_percent"); ok &&
		!openAIQuotaWindowResetAny(provider.Extra, now, "secondary", "5h") {
		secondaryRemaining := 1 - clampQuotaFactor(secondaryUsedPercent/100)
		if secondaryRemaining < openAIQuotaHeadroomSecondaryLowRemain {
			factor *= openAIQuotaHeadroomNeutralFactor
		}
	}
	return factor
}

// openAIQuotaHeadroomSnapshotStale 判断 quota 快照是否过旧到只能按中性分参与调度。
func openAIQuotaHeadroomSnapshotStale(extra map[string]any, now time.Time) bool {
	updatedRaw, ok := extra["codex_usage_updated_at"]
	if !ok {
		return true
	}
	updatedAt, err := ParseUsageTime(fmt.Sprint(updatedRaw))
	if err != nil {
		return true
	}
	return now.Sub(updatedAt) >= openAIQuotaHeadroomSnapshotStaleAfter
}

// openAIQuotaWindowResetAny 支持同时检查 primary/7d 或 secondary/5h 兼容字段。
func openAIQuotaWindowResetAny(extra map[string]any, now time.Time, windows ...string) bool {
	for _, window := range windows {
		if OpenAIQuotaWindowReset(extra, window, now) {
			return true
		}
	}
	return false
}

func clampQuotaFactor(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	default:
		return v
	}
}
