package provider

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

func attachCNMonitorLimits(provider *Record, observedAt time.Time, limits []UpstreamUsageLimit) {
	if provider.Extra == nil {
		provider.Extra = make(map[string]any)
	}
	queryConfig, err := EffectiveUpstreamUsageConfig(provider)
	if err != nil {
		panic(err)
	}
	queryConfig.Adapter = CNUpstreamUsageAdapterName(provider)
	provider.Extra[CNUsageMonitorSnapshotExtraKey] = &CNUsageMonitorSnapshot{
		Version:       CNUsageMonitorSnapshotVersion,
		Adapter:       queryConfig.Adapter,
		IdentityHash:  CNUsageMonitorIdentityFingerprint(provider),
		Provider:      provider.Platform,
		Mode:          "limits",
		Unit:          "PERCENT",
		Limits:        limits,
		ObservedAt:    &observedAt,
		LastAttemptAt: observedAt,
	}
}

func cnCodingTestProvider(platform string) *Record {
	return &Record{
		ID:       1,
		Platform: platform,
		Type:     capability.ProviderTypeAPIKey,
		Status:   billing.StatusActive,
		Credentials: map[string]any{
			"api_key":       "sk-test",
			"provider_mode": ProviderModeCoding,
		},
		Extra: map[string]any{},
	}
}

// TestCNProviderThresholdCandidates 从统一监控快照读取 5h / weekly 候选。
func TestCNProviderThresholdCandidates(t *testing.T) {
	t.Parallel()
	observed := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	reset5h := observed.Add(3 * time.Hour)
	resetWeekly := observed.Add(4 * 24 * time.Hour)
	used5h, usedWeekly := 90.0, 50.0
	provider := cnCodingTestProvider(capability.PlatformKimi)
	attachCNMonitorLimits(provider, observed, []UpstreamUsageLimit{
		{Name: "5h", Used: &used5h, ResetAt: &reset5h},
		{Name: "7d", Used: &usedWeekly, ResetAt: &resetWeekly},
	})
	cands := CNProviderThresholdCandidates(provider, capability.PlatformKimi)
	require.Len(t, cands, 2)

	// 缺少 used 的窗口不产生候选。
	partial := cnCodingTestProvider(capability.PlatformKimi)
	attachCNMonitorLimits(partial, observed, []UpstreamUsageLimit{{Name: "5h", ResetAt: &reset5h}})
	require.Empty(t, filterNil(CNProviderThresholdCandidates(partial, capability.PlatformKimi)))

	// 身份变化后旧快照失效。
	provider.Credentials["api_key"] = "sk-changed"
	require.Empty(t, CNProviderThresholdCandidates(provider, capability.PlatformKimi))
}

func filterNil(cands []*SchedulingThresholdCandidate) []*SchedulingThresholdCandidate {
	var out []*SchedulingThresholdCandidate
	for _, c := range cands {
		if c != nil {
			out = append(out, c)
		}
	}
	return out
}

// TestEvaluateProviderSchedulingThreshold_KimiCodingPlan 集成验证：kimi coding 提供商
// 5h 用量超阈值且窗口未重置 → 主动停调至 5h 重置点。
func TestEvaluateProviderSchedulingThreshold_KimiCodingPlan(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	reset := now.Add(3 * time.Hour)
	used5h, usedWeekly := 90.0, 30.0
	weeklyReset := now.Add(7 * 24 * time.Hour)
	provider := cnCodingTestProvider(capability.PlatformKimi)
	attachCNMonitorLimits(provider, now, []UpstreamUsageLimit{
		{Name: "5h", Used: &used5h, ResetAt: &reset},
		{Name: "7d", Used: &usedWeekly, ResetAt: &weeklyReset},
	})
	decision := EvaluateProviderSchedulingThreshold(provider, map[string]int{capability.PlatformKimi: 80}, now)
	require.True(t, decision.ShouldPause)
	require.Equal(t, capability.PlatformKimi, decision.Platform)
	require.Equal(t, "5h", decision.Window)
	require.InDelta(t, 90.0, decision.UsedPercent, 1e-9)
	require.NotNil(t, decision.Until)
	require.True(t, reset.Equal(*decision.Until))
}

// TestEvaluateProviderSchedulingThreshold_CNWindowResetSkipped 窗口已重置（reset<=now）
// 或用量低于阈值 → 不停调（candidateMatchesThreshold 要求 until.After(now)）。
func TestEvaluateProviderSchedulingThreshold_CNWindowResetSkipped(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	// 重置时间已过。
	expiredUsed := 99.0
	expiredReset := now.Add(-time.Hour)
	expired := cnCodingTestProvider(capability.PlatformZhipu)
	attachCNMonitorLimits(expired, now, []UpstreamUsageLimit{{Name: "5h", Used: &expiredUsed, ResetAt: &expiredReset}})
	require.False(t, EvaluateProviderSchedulingThreshold(expired, map[string]int{capability.PlatformZhipu: 80}, now).ShouldPause)

	// 用量低于阈值。
	lowUsed := 20.0
	lowReset := now.Add(3 * time.Hour)
	low := cnCodingTestProvider(capability.PlatformZhipu)
	attachCNMonitorLimits(low, now, []UpstreamUsageLimit{{Name: "5h", Used: &lowUsed, ResetAt: &lowReset}})
	require.False(t, EvaluateProviderSchedulingThreshold(low, map[string]int{capability.PlatformZhipu: 80}, now).ShouldPause)
}

// TestCNProviderQuotaSnapshotReset Coding Plan 429 冷却：取快照中最早的「仍在未来」窗口重置点。
func TestCNProviderQuotaSnapshotReset(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	future5h := now.Add(2 * time.Hour)
	futureWeekly := now.Add(3 * 24 * time.Hour)
	pastWeekly := now.Add(-24 * time.Hour)

	// 5h 在未来、weekly 已过期 → 返回 5h。
	used := 100.0
	provider := cnCodingTestProvider(capability.PlatformKimi)
	attachCNMonitorLimits(provider, now, []UpstreamUsageLimit{
		{Name: "5h", Used: &used, ResetAt: &future5h},
		{Name: "7d", Used: &used, ResetAt: &pastWeekly},
	})
	got := CNProviderQuotaSnapshotReset(provider, now)
	require.NotNil(t, got)
	require.True(t, future5h.Equal(*got))

	// 两个窗口都尚未重置时取较早时间，429 通常由 5h 窗口触发。
	both := cnCodingTestProvider(capability.PlatformKimi)
	attachCNMonitorLimits(both, now, []UpstreamUsageLimit{
		{Name: "5h", Used: &used, ResetAt: &future5h},
		{Name: "7d", Used: &used, ResetAt: &futureWeekly},
	})
	gotBoth := CNProviderQuotaSnapshotReset(both, now)
	require.NotNil(t, gotBoth)
	require.True(t, future5h.Equal(*gotBoth))

	// 两窗口均过期 → nil。
	expired := cnCodingTestProvider(capability.PlatformKimi)
	attachCNMonitorLimits(expired, now, []UpstreamUsageLimit{
		{Name: "5h", Used: &used, ResetAt: &pastWeekly},
		{Name: "7d", Used: &used, ResetAt: &pastWeekly},
	})
	require.Nil(t, CNProviderQuotaSnapshotReset(expired, now))

	// payg 提供商（非 coding）→ nil（余额型走余额检测）。
	payg := cnCodingTestProvider(capability.PlatformKimi)
	payg.Credentials["provider_mode"] = ProviderModePayG
	require.Nil(t, CNProviderQuotaSnapshotReset(payg, now))
}
