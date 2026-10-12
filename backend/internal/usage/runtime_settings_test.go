package usage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	settingscore "github.com/TokenFlux/TokenRouter/internal/settings"
)

type usageRankingSettingsRepoStub struct {
	settingscore.Repository
	values map[string]string
}

func (s *usageRankingSettingsRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	result := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := s.values[key]; ok {
			result[key] = value
		}
	}
	return result, nil
}

func TestGetUsageRankingSettingsUsesCompatibleDefaults(t *testing.T) {
	svc := NewRuntimeSettings(&usageRankingSettingsRepoStub{values: map[string]string{}})

	settings, err := svc.GetUsageRankingSettings(context.Background())

	require.NoError(t, err)
	require.True(t, settings.Enabled)
	require.Equal(t, UsageRankingSortByTotalTokens, settings.SortBy)
	require.True(t, settings.ShowTotalTokens)
	require.True(t, settings.ShowRequests)
	require.True(t, settings.ShowActualCost)
	require.Equal(t, DefaultUsageRankingLimit, settings.Limit)
}

func TestGetUsageRankingSettingsForcesSortMetricVisible(t *testing.T) {
	svc := NewRuntimeSettings(&usageRankingSettingsRepoStub{values: map[string]string{
		SettingKeyUsageRankingSortBy:          string(UsageRankingSortByActualCost),
		SettingKeyUsageRankingShowTotalTokens: "false",
		SettingKeyUsageRankingShowRequests:    "false",
		SettingKeyUsageRankingShowActualCost:  "false",
		SettingKeyUsageRankingLimit:           "999",
	}})

	settings, err := svc.GetUsageRankingSettings(context.Background())

	require.NoError(t, err)
	require.Equal(t, UsageRankingSortByActualCost, settings.SortBy)
	require.False(t, settings.ShowTotalTokens)
	require.False(t, settings.ShowRequests)
	require.True(t, settings.ShowActualCost)
	require.Equal(t, MaxUsageRankingLimit, settings.Limit)
}

func TestSettingKeyAllowUserViewErrorRequests_Constant(t *testing.T) {
	if SettingKeyAllowUserViewErrorRequests != "allow_user_view_error_requests" {
		t.Fatalf("unexpected key: %s", SettingKeyAllowUserViewErrorRequests)
	}
}
