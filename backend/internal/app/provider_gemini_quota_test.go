package app

import (
	"context"
	"testing"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	usagecore "github.com/TokenFlux/TokenRouter/internal/usage"
	"github.com/stretchr/testify/require"
)

func TestGeminiThirdPartyAPIKeySkipsLocalQuota(t *testing.T) {
	ctx := context.Background()
	quotaService := provider.NewGeminiQuotaService(provider.GeminiQuotaOptions{})
	official := &provider.Record{
		ID:       101,
		Platform: capability.PlatformGemini,
		Type:     capability.ProviderTypeAPIKey,
		Credentials: map[string]any{
			"tier_id":       provider.GeminiTierAIStudioFree,
			"provider_type": "official",
		},
	}
	thirdParty := &provider.Record{
		ID:       102,
		Platform: capability.PlatformGemini,
		Type:     capability.ProviderTypeAPIKey,
		Credentials: map[string]any{
			"provider_type": provider.GeminiProviderTypeThirdParty,
		},
	}

	_, officialHasQuota := quotaService.QuotaForProvider(ctx, official)
	_, thirdPartyHasQuota := quotaService.QuotaForProvider(ctx, thirdParty)
	require.True(t, officialHasQuota)
	require.False(t, thirdPartyHasQuota)
	require.True(t, thirdParty.IsGeminiThirdPartyProvider())
	require.Equal(t, 5*time.Minute, quotaService.CooldownForProvider(ctx, thirdParty))

	usageSvc := provider.NewOAuthUsageService(nil, nil, nil, provider.OAuthUsageOptions{Gemini: provider.GeminiUsageOptions{
		Location: geminiQuotaLocation, Quota: quotaService.QuotaForProvider,
		Totals: func(context.Context, int64, time.Time, time.Time) (provider.GeminiUsageTotals, error) {
			return provider.GeminiUsageTotals{}, nil
		},
	}})
	usage, err := usageSvc.GetGeminiUsage(ctx, thirdParty)
	require.NoError(t, err)
	require.Nil(t, usage.GeminiSharedDaily)
	require.Nil(t, usage.GeminiProDaily)
	require.Nil(t, usage.GeminiFlashDaily)

	// 免费档位的 Pro 日配额在此用例中为 50，从统计接口读取用量后预检拒绝满额请求。
	rateLimitSvc := provideGeminiPrecheck(quotaService, &geminiFullLocalUsage{})
	officialAllowed, err := rateLimitSvc.PreCheckUsage(ctx, official, "gemini-2.5-pro")
	require.NoError(t, err)
	require.False(t, officialAllowed)

	thirdPartyAllowed, err := rateLimitSvc.PreCheckUsage(ctx, thirdParty, "gemini-2.5-pro")
	require.NoError(t, err)
	require.True(t, thirdPartyAllowed)
}

// 满额夹具通过 SQL 查询返回配额用量。
type geminiFullLocalUsage struct{ usagecore.UsageLogRepository }

func (r *geminiFullLocalUsage) GetModelStatsWithFilters(context.Context, time.Time, time.Time, int64, int64, int64, int64, *int16, *bool, *int8) ([]usagecore.ModelStat, error) {
	return []usagecore.ModelStat{{Model: "gemini-2.5-pro", Requests: 50}}, nil
}
