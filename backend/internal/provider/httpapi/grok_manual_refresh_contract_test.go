package httpapi

import (
	"context"
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"

	"github.com/stretchr/testify/require"
)

type grokRefreshOAuthStub struct {
	provider *providercore.Record
	info     *providercore.GrokTokenInfo
	calls    int
}

func (s *grokRefreshOAuthStub) RefreshProviderToken(_ context.Context, provider *providercore.Record) (*providercore.GrokTokenInfo, error) {
	s.calls++
	s.provider = provider
	return s.info, nil
}

func (s *grokRefreshOAuthStub) BuildProviderCredentials(info *providercore.GrokTokenInfo) map[string]any {
	return map[string]any{
		"access_token":  info.AccessToken,
		"refresh_token": info.RefreshToken,
		"expires_at":    info.ExpiresAt,
		"base_url":      "https://api.x.ai/v1",
	}
}

type grokRefreshAdminService struct {
	*managementMutationFixture
	updatedCredentials map[string]any
}

func (s *grokRefreshAdminService) UpdateProvider(_ context.Context, id int64, input *providercore.UpdateProviderInput) (*providercore.Record, error) {
	s.updatedCredentials = input.Credentials
	return &providercore.Record{
		ID:          id,
		Platform:    capability.PlatformGrok,
		Type:        capability.ProviderTypeOAuth,
		Status:      billing.StatusActive,
		Credentials: input.Credentials,
	}, nil
}

func TestRefreshSingleProviderRoutesGrokThroughGrokOAuthService(t *testing.T) {
	t.Parallel()

	adminSvc := &grokRefreshAdminService{managementMutationFixture: newManagementMutationFixture()}
	grokOAuth := &grokRefreshOAuthStub{info: &providercore.GrokTokenInfo{
		AccessToken:  "new-access",
		RefreshToken: "new-refresh",
		ExpiresAt:    1_800_000_000,
	}}
	handler := newManagedRefreshFixture(adminSvc, &providercore.ManualCredentialExchange{Grok: grokOAuth})
	provider := &providercore.Record{
		ID:       4227,
		Platform: capability.PlatformGrok,
		Type:     capability.ProviderTypeOAuth,
		Credentials: map[string]any{
			"access_token":       "old-access",
			"refresh_token":      "old-refresh",
			"base_url":           "https://example.invalid/v1",
			"subscription_tier":  "SUPER_GROK",
			"entitlement_status": "ACTIVE",
		},
	}

	updated, warning, err := handler.Refresh(context.Background(), provider)
	require.NoError(t, err)
	require.Empty(t, warning)
	require.Equal(t, 1, grokOAuth.calls)
	// 传入完整的提供商记录，值比较跳过记录中的时钟函数。
	require.Equal(t, provider, grokOAuth.provider)
	require.Equal(t, "new-access", adminSvc.updatedCredentials["access_token"])
	require.Equal(t, "new-refresh", adminSvc.updatedCredentials["refresh_token"])
	require.Equal(t, "https://example.invalid/v1", adminSvc.updatedCredentials["base_url"])
	require.Equal(t, "SUPER_GROK", adminSvc.updatedCredentials["subscription_tier"])
	require.Equal(t, "ACTIVE", adminSvc.updatedCredentials["entitlement_status"])
	require.Equal(t, adminSvc.updatedCredentials, updated.Credentials)
}
