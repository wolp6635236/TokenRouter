package provider_test

import (
	"context"
	"maps"
	"testing"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

type deprecatedProviderExtraRepoStub struct {
	providercore.AdminStore
	provider            *providercore.Record
	updateExtraCalls    int
	lastExtraUpdates    map[string]any
	bulkUpdateCalls     int
	lastBulkExtraUpdate map[string]any
}

func (r *deprecatedProviderExtraRepoStub) GetByID(_ context.Context, _ int64) (*providercore.Record, error) {
	return providercore.CloneRecord(r.provider), nil
}

func (r *deprecatedProviderExtraRepoStub) Update(_ context.Context, provider *providercore.Record) error {
	r.provider = providercore.CloneRecord(provider)
	return nil
}

func (r *deprecatedProviderExtraRepoStub) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	r.updateExtraCalls++
	r.lastExtraUpdates = maps.Clone(updates)
	return nil
}

func (r *deprecatedProviderExtraRepoStub) BulkUpdate(_ context.Context, _ []int64, updates providercore.ProviderBulkUpdate) (int64, error) {
	r.bulkUpdateCalls++
	r.lastBulkExtraUpdate = maps.Clone(updates.Extra)
	return 1, nil
}

func TestDiscardDeprecatedProviderExtra(t *testing.T) {
	extra := map[string]any{
		"openai_long_context_billing_enabled": "malformed",
		"upstream_billing_probe":              map[string]any{"status": "ok"},
		"upstream_billing_probe_enabled":      true,
		"preserved":                           "value",
	}

	providercore.DiscardDeprecatedProviderExtra(extra)

	require.Equal(t, map[string]any{"preserved": "value"}, extra)
}

func TestAdminServiceUpdateProviderDiscardsDeprecatedLongContextBillingExtra(t *testing.T) {
	repo := &deprecatedProviderExtraRepoStub{provider: &providercore.Record{
		ID:       1,
		Platform: capability.PlatformOpenAI,
		Type:     capability.ProviderTypeAPIKey,
		Extra: map[string]any{
			"openai_long_context_billing_enabled": false,
			"old":                                 true,
			"quota_used":                          float64(5),
		},
	}}
	svc := newProviderEditorForTest(repo)

	provider, err := svc.UpdateProvider(context.Background(), 1, &providercore.UpdateProviderInput{Extra: map[string]any{
		"openai_long_context_billing_enabled": []bool{true},
		"privacy_mode":                        "blocked",
		"quota_limit":                         float64(25),
	}})

	require.NoError(t, err)
	require.NotContains(t, provider.Extra, "openai_long_context_billing_enabled")
	require.Equal(t, "blocked", provider.Extra["privacy_mode"])
	require.Equal(t, float64(25), provider.Extra["quota_limit"])
	require.Equal(t, float64(5), provider.Extra["quota_used"])
	require.NotContains(t, provider.Extra, "old")
}

func TestAdminServiceUpdateProviderDeprecatedOnlyPreservesExistingExtra(t *testing.T) {
	tests := []struct {
		name  string
		value any
	}{
		{name: "布尔旧值", value: false},
		{name: "非法类型", value: map[string]any{"malformed": true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &deprecatedProviderExtraRepoStub{provider: &providercore.Record{
				ID:       1,
				Platform: capability.PlatformOpenAI,
				Type:     capability.ProviderTypeAPIKey,
				Extra: map[string]any{
					"openai_long_context_billing_enabled": true,
					"privacy_mode":                        "limited",
					"quota_limit":                         float64(100),
					"quota_daily_limit":                   float64(20),
					"custom":                              "preserved",
				},
			}}
			svc := newProviderEditorForTest(repo)

			provider, err := svc.UpdateProvider(context.Background(), 1, &providercore.UpdateProviderInput{Extra: map[string]any{
				"openai_long_context_billing_enabled": tt.value,
			}})

			require.NoError(t, err)
			require.NotContains(t, provider.Extra, "openai_long_context_billing_enabled")
			require.Equal(t, "limited", provider.Extra["privacy_mode"])
			require.Equal(t, float64(100), provider.Extra["quota_limit"])
			require.Equal(t, float64(20), provider.Extra["quota_daily_limit"])
			require.Equal(t, "preserved", provider.Extra["custom"])
		})
	}
}

func TestAdminServiceUpdateProviderExplicitEmptyExtraStillClearsConfig(t *testing.T) {
	repo := &deprecatedProviderExtraRepoStub{provider: &providercore.Record{
		ID:       1,
		Platform: capability.PlatformOpenAI,
		Type:     capability.ProviderTypeAPIKey,
		Extra: map[string]any{
			"privacy_mode": "limited",
			"quota_limit":  float64(100),
			"quota_used":   float64(7),
		},
	}}
	svc := newProviderEditorForTest(repo)

	provider, err := svc.UpdateProvider(context.Background(), 1, &providercore.UpdateProviderInput{Extra: map[string]any{}})

	require.NoError(t, err)
	require.NotNil(t, provider.Extra)
	require.NotContains(t, provider.Extra, "privacy_mode")
	require.NotContains(t, provider.Extra, "quota_limit")
	require.Equal(t, float64(7), provider.Extra["quota_used"])
}

func TestAdminServiceUpdateProviderExtraIgnoresDeprecatedLongContextBillingExtra(t *testing.T) {
	repo := &deprecatedProviderExtraRepoStub{}
	svc := newProviderEditorForTest(repo)

	err := svc.UpdateProviderExtra(context.Background(), 1, map[string]any{
		"openai_long_context_billing_enabled": 1,
	})

	require.NoError(t, err)
	require.Zero(t, repo.updateExtraCalls)

	err = svc.UpdateProviderExtra(context.Background(), 1, map[string]any{
		"openai_long_context_billing_enabled": "true",
		"preserved":                           true,
	})
	require.NoError(t, err)
	require.Equal(t, 1, repo.updateExtraCalls)
	require.Equal(t, map[string]any{"preserved": true}, repo.lastExtraUpdates)
}

func TestAdminServiceBulkUpdateProvidersIgnoresDeprecatedLongContextBillingExtra(t *testing.T) {
	repo := &deprecatedProviderExtraRepoStub{}
	svc := newProviderEditorForTest(repo)

	result, err := svc.BulkUpdateProviders(context.Background(), &providercore.BulkUpdateProvidersInput{
		ProviderIDs: []int64{1},
		Extra: map[string]any{
			"openai_long_context_billing_enabled": map[string]any{"invalid": true},
			"preserved":                           true,
		},
	})

	require.NoError(t, err)
	require.Equal(t, 1, result.Success)
	require.Equal(t, 1, repo.bulkUpdateCalls)
	require.Equal(t, map[string]any{"preserved": true}, repo.lastBulkExtraUpdate)
}
