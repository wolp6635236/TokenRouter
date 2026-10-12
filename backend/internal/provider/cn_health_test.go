package provider

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

// 健康规则测试检查处理顺序和输入，适配器负责转换模型数据。
type cnHealthFailureStore struct {
	HealthStore
	order *[]string
}

func (s cnHealthFailureStore) SetTempUnschedulable(context.Context, int64, time.Time, string) error {
	*s.order = append(*s.order, "store")
	return errors.New("fixture persistence failed")
}

func TestCNConcurrencyRepositoryFailureKeepsRuntimeBlock(t *testing.T) {
	var order []string
	var blocked *Record
	now := time.Now()
	core := NewHealthService(cnHealthFailureStore{order: &order}, nil, HealthOptions{Now: func() time.Time { return now }, Block: func(v *Record, until time.Time, reason string) {
		order = append(order, "block")
		blocked = v
		require.Equal(t, now.Add(10*time.Minute), until)
		require.Equal(t, "cn_concurrency_limit", reason)
	}})
	original := &Record{ID: 406, Platform: PlatformKimi, Type: ProviderTypeAPIKey}
	core.ApplyCNConcurrencyLimit(context.Background(), original, "fixture concurrency")
	require.Equal(t, []string{"block", "store"}, order)
	require.Same(t, original, blocked)
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
