package provider_test

import (
	"context"
	"testing"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

type updateProviderOveragesRepoStub struct {
	providercore.AdminStore
	provider    *providercore.Record
	updateCalls int
}

func (r *updateProviderOveragesRepoStub) GetByID(ctx context.Context, id int64) (*providercore.Record, error) {
	value := providercore.CloneRecord(r.provider)
	if value != nil {
		value.LoadLocation = time.LoadLocation
	}
	return value, nil
}

func (r *updateProviderOveragesRepoStub) Update(ctx context.Context, provider *providercore.Record) error {
	r.updateCalls++
	r.provider = providercore.CloneRecord(provider)
	return nil
}

func TestUpdateProvider_DisableOveragesClearsAICreditsKey(t *testing.T) {
	providerID := int64(101)
	repo := &updateProviderOveragesRepoStub{
		provider: &providercore.Record{
			ID:       providerID,
			Platform: capability.PlatformAntigravity,
			Type:     capability.ProviderTypeOAuth,
			Status:   billing.StatusActive,
			Extra: map[string]any{
				"allow_overages":   true,
				"mixed_scheduling": true,
				"model_rate_limits": map[string]any{
					"claude-sonnet-4-5": map[string]any{
						"rate_limited_at":     "2026-03-15T00:00:00Z",
						"rate_limit_reset_at": "2099-03-15T00:00:00Z",
					},
					providercore.CreditsExhaustedKey: map[string]any{
						"rate_limited_at":     "2026-03-15T00:00:00Z",
						"rate_limit_reset_at": time.Now().Add(5 * time.Hour).UTC().Format(time.RFC3339),
					},
				},
			},
		},
	}

	svc := newProviderEditorForTest(repo)
	updated, err := svc.UpdateProvider(context.Background(), providerID, &providercore.UpdateProviderInput{
		Extra: map[string]any{
			"mixed_scheduling": true,
			"model_rate_limits": map[string]any{
				"claude-sonnet-4-5": map[string]any{
					"rate_limited_at":     "2026-03-15T00:00:00Z",
					"rate_limit_reset_at": "2099-03-15T00:00:00Z",
				},
				providercore.CreditsExhaustedKey: map[string]any{
					"rate_limited_at":     "2026-03-15T00:00:00Z",
					"rate_limit_reset_at": time.Now().Add(5 * time.Hour).UTC().Format(time.RFC3339),
				},
			},
		},
	})

	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Equal(t, 1, repo.updateCalls)
	require.False(t, updated.IsOveragesEnabled())

	// 关闭 overages 后，AICredits key 应被清除
	rawLimits, ok := repo.provider.Extra["model_rate_limits"].(map[string]any)
	if ok {
		_, exists := rawLimits[providercore.CreditsExhaustedKey]
		require.False(t, exists, "关闭 overages 时应清除 AICredits 限流 key")
	}
	// 普通模型限流应保留
	require.True(t, ok)
	_, exists := rawLimits["claude-sonnet-4-5"]
	require.True(t, exists, "普通模型限流应保留")
}

func TestUpdateProvider_EnableOveragesClearsModelRateLimitsBeforePersist(t *testing.T) {
	providerID := int64(102)
	repo := &updateProviderOveragesRepoStub{
		provider: &providercore.Record{
			ID:       providerID,
			Platform: capability.PlatformAntigravity,
			Type:     capability.ProviderTypeOAuth,
			Status:   billing.StatusActive,
			Extra: map[string]any{
				"mixed_scheduling": true,
				"model_rate_limits": map[string]any{
					"claude-sonnet-4-5": map[string]any{
						"rate_limited_at":     "2026-03-15T00:00:00Z",
						"rate_limit_reset_at": "2099-03-15T00:00:00Z",
					},
				},
			},
		},
	}

	svc := newProviderEditorForTest(repo)
	updated, err := svc.UpdateProvider(context.Background(), providerID, &providercore.UpdateProviderInput{
		Extra: map[string]any{
			"mixed_scheduling": true,
			"allow_overages":   true,
		},
	})

	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Equal(t, 1, repo.updateCalls)
	require.True(t, updated.IsOveragesEnabled())

	_, exists := repo.provider.Extra["model_rate_limits"]
	require.False(t, exists, "开启 overages 时应在持久化前清掉旧模型限流")
}

func TestUpdateProvider_EmptyExtraPayloadCanClearQuotaLimits(t *testing.T) {
	providerID := int64(103)
	repo := &updateProviderOveragesRepoStub{
		provider: &providercore.Record{
			ID:       providerID,
			Platform: capability.PlatformAnthropic,
			Type:     capability.ProviderTypeAPIKey,
			Status:   billing.StatusActive,
			Extra: map[string]any{
				"quota_limit":        100.0,
				"quota_daily_limit":  10.0,
				"quota_weekly_limit": 40.0,
			},
		},
	}

	svc := newProviderEditorForTest(repo)
	updated, err := svc.UpdateProvider(context.Background(), providerID, &providercore.UpdateProviderInput{
		// 空对象表示清空 Extra 中的可配置键，例如关闭配额限制。
		Extra: map[string]any{},
	})

	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Equal(t, 1, repo.updateCalls)
	require.NotNil(t, repo.provider.Extra)
	require.NotContains(t, repo.provider.Extra, "quota_limit")
	require.NotContains(t, repo.provider.Extra, "quota_daily_limit")
	require.NotContains(t, repo.provider.Extra, "quota_weekly_limit")
	require.Len(t, repo.provider.Extra, 0)
}

func TestUpdateProvider_FixedWeeklyResetClearsLegacyRollingUsage(t *testing.T) {
	now := time.Now().UTC()
	daysSinceMonday := (int(now.Weekday()) + 6) % 7
	currentWeekStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -daysSinceMonday)
	legacyRollingStart := currentWeekStart.Add(-24 * time.Hour)
	providerID := int64(104)
	repo := &updateProviderOveragesRepoStub{
		provider: &providercore.Record{
			ID:       providerID,
			Platform: capability.PlatformAnthropic,
			Type:     capability.ProviderTypeAPIKey,
			Status:   billing.StatusActive,
			Extra: map[string]any{
				"quota_weekly_limit": 40.0,
				"quota_weekly_used":  12.5,
				"quota_weekly_start": legacyRollingStart.Format(time.RFC3339),
			},
		},
	}

	svc := newProviderEditorForTest(repo)
	updated, err := svc.UpdateProvider(context.Background(), providerID, &providercore.UpdateProviderInput{
		Extra: map[string]any{
			"quota_weekly_limit":      40.0,
			"quota_weekly_reset_mode": "fixed",
			"quota_weekly_reset_day":  float64(1),
			"quota_weekly_reset_hour": float64(0),
			"quota_reset_timezone":    "UTC",
		},
	})

	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Equal(t, 1, repo.updateCalls)
	require.InDelta(t, 0.0, updated.GetQuotaWeeklyUsed(), 1e-9)
	require.Equal(t, currentWeekStart.Format(time.RFC3339), updated.Extra["quota_weekly_start"])
	require.False(t, updated.IsWeeklyQuotaPeriodExpired())
}
