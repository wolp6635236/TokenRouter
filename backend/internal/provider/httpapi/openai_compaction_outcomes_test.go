package httpapi

import (
	"fmt"
	"testing"
	"time"

	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

// TestManualCompactionTestsPreserveConfigurationAcrossOutcomes 验证手动测试保留认证错误和限流处理，但所有结果均不得改写管理员能力配置。
func TestManualCompactionTestsPreserveConfigurationAcrossOutcomes(t *testing.T) {
	for _, mode := range []string{providercore.ProviderTestModeCompact, providercore.ProviderTestModeLegacyCompact} {
		for _, status := range []int{200, 401, 404, 429} {
			t.Run(fmt.Sprintf("%s/%d", mode, status), func(t *testing.T) {
				extra := map[string]any{"openai_compact_mode": "force_off", providercore.OpenAINativeCompactionV2ModeExtraKey: "force_on"}
				provider := &providercore.Record{
					ID: 13, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, Status: billing.StatusActive,
					Credentials: map[string]any{"access_token": "test"}, Extra: extra,
				}
				repo := &openAIProbeStore{}
				body := compactionTestV2SSESuccessBody
				if mode == providercore.ProviderTestModeLegacyCompact {
					body = `{"id":"legacy_test","status":"completed"}`
				}
				if status != 200 {
					body = fmt.Sprintf(`{"error":{"type":"usage_limit_reached","message":"test failure","resets_at":%d}}`, time.Now().Add(time.Hour).Unix())
				}
				resp := newJSONResponse(status, body)
				upstream := &openAIProbeTransport{resp: resp}
				svc := &provideradapter.OpenAIProviderTest{Store: repo, Transport: upstream}
				c, _ := newTestContext()
				err := executeOpenAIProbe(t, svc, c, provider, "gpt-5.4", "", mode)
				if status == 200 {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
				require.Equal(t, "force_off", provider.Extra["openai_compact_mode"])
				require.Equal(t, "force_on", provider.Extra[providercore.OpenAINativeCompactionV2ModeExtraKey])
				for _, key := range providercore.DeprecatedOpenAIProviderExtraKeys {
					require.NotContains(t, repo.updatedExtra, key)
				}
				require.NotContains(t, repo.updatedExtra, "openai_compact_mode")
				require.NotContains(t, repo.updatedExtra, providercore.OpenAINativeCompactionV2ModeExtraKey)
				if status == 401 {
					require.Equal(t, provider.ID, repo.setErrorID)
				}
				if status == 429 {
					require.Equal(t, provider.ID, repo.rateLimitedID)
				}
			})
		}
	}
}
