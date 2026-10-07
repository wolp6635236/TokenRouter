package provider_test

import (
	"context"
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

type updateProviderCredsRepoStub struct {
	providercore.AdminStore
	provider    *providercore.Record
	updateCalls int
}

func (r *updateProviderCredsRepoStub) GetByID(ctx context.Context, id int64) (*providercore.Record, error) {
	return providercore.CloneRecord(r.provider), nil
}

func (r *updateProviderCredsRepoStub) Update(ctx context.Context, provider *providercore.Record) error {
	r.updateCalls++
	r.provider = providercore.CloneRecord(provider)
	return nil
}

func TestUpdateProvider_PreservesSensitiveCredsWhenIncomingOmits(t *testing.T) {
	providerID := int64(202)
	repo := &updateProviderCredsRepoStub{
		provider: &providercore.Record{
			ID:       providerID,
			Platform: capability.PlatformAnthropic,
			Type:     capability.ProviderTypeOAuth,
			Status:   billing.StatusActive,
			Credentials: map[string]any{
				"refresh_token": "rt-existing",
				"access_token":  "at-existing",
				"id_token":      "id-existing",
				"base_url":      "https://old.example.com",
			},
		},
	}
	svc := newProviderEditorForTest(repo)

	// 模拟前端编辑：仅修改 base_url，没有传 token（脱敏后前端 spread 拿不到敏感键）
	updated, err := svc.UpdateProvider(context.Background(), providerID, &providercore.UpdateProviderInput{
		Credentials: map[string]any{
			"base_url": "https://new.example.com",
		},
	})

	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Equal(t, 1, repo.updateCalls)

	// 敏感键应保留
	require.Equal(t, "rt-existing", repo.provider.Credentials["refresh_token"])
	require.Equal(t, "at-existing", repo.provider.Credentials["access_token"])
	require.Equal(t, "id-existing", repo.provider.Credentials["id_token"])
	// 非敏感键被替换
	require.Equal(t, "https://new.example.com", repo.provider.Credentials["base_url"])
}

func TestUpdateProvider_ExplicitNewTokenOverwrites(t *testing.T) {
	providerID := int64(203)
	repo := &updateProviderCredsRepoStub{
		provider: &providercore.Record{
			ID:       providerID,
			Platform: capability.PlatformAnthropic,
			Type:     capability.ProviderTypeOAuth,
			Status:   billing.StatusActive,
			Credentials: map[string]any{
				"refresh_token": "rt-old",
				"api_key":       "sk-old",
			},
		},
	}
	svc := newProviderEditorForTest(repo)

	updated, err := svc.UpdateProvider(context.Background(), providerID, &providercore.UpdateProviderInput{
		Credentials: map[string]any{
			"refresh_token": "rt-new",
			// api_key 没传 → 应保留旧值
		},
	})
	require.NoError(t, err)
	require.NotNil(t, updated)

	require.Equal(t, "rt-new", repo.provider.Credentials["refresh_token"])
	require.Equal(t, "sk-old", repo.provider.Credentials["api_key"])
}

func TestUpdateProvider_EmptyCredentialsSkipsUpdate(t *testing.T) {
	providerID := int64(204)
	repo := &updateProviderCredsRepoStub{
		provider: &providercore.Record{
			ID:       providerID,
			Platform: capability.PlatformAnthropic,
			Type:     capability.ProviderTypeOAuth,
			Status:   billing.StatusActive,
			Credentials: map[string]any{
				"refresh_token": "rt-existing",
			},
		},
	}
	svc := newProviderEditorForTest(repo)

	_, err := svc.UpdateProvider(context.Background(), providerID, &providercore.UpdateProviderInput{
		Credentials: map[string]any{}, // len == 0 → 闸门跳过
		Name:        "renamed",
	})
	require.NoError(t, err)

	require.Equal(t, "rt-existing", repo.provider.Credentials["refresh_token"], "空 credentials 不应触碰已有 token")
	require.Equal(t, "renamed", repo.provider.Name)
}
