package provider_test

import (
	"context"
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/provider"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/pkg/pagination"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/upstream/openai"
	"github.com/stretchr/testify/require"
)

type providerRepoStubForAdminList struct {
	provider.AdminStore

	listWithFiltersCalls     int
	listWithFiltersParams    pagination.PaginationParams
	listWithFiltersPlatform  string
	listWithFiltersType      string
	listWithFiltersStatus    string
	listWithFiltersSearch    string
	listWithFiltersPrivacy   string
	listWithFiltersProviders []provider.Record
	listWithFiltersResult    *pagination.PaginationResult
	listWithFiltersErr       error
}

func (s *providerRepoStubForAdminList) ListAllWithFilters(context.Context, string, string, string, string, int64, string) ([]provider.Record, error) {
	return nil, nil
}

func (s *providerRepoStubForAdminList) ListWithFilters(_ context.Context, params pagination.PaginationParams, platform, providerType, status, search string, groupID int64, privacyMode string) ([]provider.Record, *pagination.PaginationResult, error) {
	s.listWithFiltersCalls++
	s.listWithFiltersParams = params
	s.listWithFiltersPlatform = platform
	s.listWithFiltersType = providerType
	s.listWithFiltersStatus = status
	s.listWithFiltersSearch = search
	s.listWithFiltersPrivacy = privacyMode

	if s.listWithFiltersErr != nil {
		return nil, nil, s.listWithFiltersErr
	}

	result := s.listWithFiltersResult
	if result == nil {
		result = &pagination.PaginationResult{
			Total:    int64(len(s.listWithFiltersProviders)),
			Page:     params.Page,
			PageSize: params.PageSize,
		}
	}

	return s.listWithFiltersProviders, result, nil
}

func TestAdminService_ListProviders_WithSearch(t *testing.T) {
	t.Run("search 参数正常传递到 repository 层", func(t *testing.T) {
		repo := &providerRepoStubForAdminList{
			listWithFiltersProviders: []provider.Record{{ID: 1, Name: "acc"}},
			listWithFiltersResult:    &pagination.PaginationResult{Total: 10},
		}
		svc := provider.NewAdmin(repo, provider.AdminOptions{})

		providers, total, err := svc.ListProviders(context.Background(), 1, 20, capability.PlatformGemini, capability.ProviderTypeOAuth, billing.StatusActive, "acc", 0, "", "name", "ASC")
		require.NoError(t, err)
		require.Equal(t, int64(10), total)
		require.Equal(t, []provider.Record{{ID: 1, Name: "acc"}}, providers)

		require.Equal(t, 1, repo.listWithFiltersCalls)
		require.Equal(t, pagination.PaginationParams{Page: 1, PageSize: 20, SortBy: "name", SortOrder: "ASC"}, repo.listWithFiltersParams)
		require.Equal(t, capability.PlatformGemini, repo.listWithFiltersPlatform)
		require.Equal(t, capability.ProviderTypeOAuth, repo.listWithFiltersType)
		require.Equal(t, billing.StatusActive, repo.listWithFiltersStatus)
		require.Equal(t, "acc", repo.listWithFiltersSearch)
	})
}

func TestAdminService_ListProviders_WithPrivacyMode(t *testing.T) {
	t.Run("privacy_mode 参数正常传递到 repository 层", func(t *testing.T) {
		repo := &providerRepoStubForAdminList{
			listWithFiltersProviders: []provider.Record{{ID: 2, Name: "acc2"}},
			listWithFiltersResult:    &pagination.PaginationResult{Total: 1},
		}
		svc := provider.NewAdmin(repo, provider.AdminOptions{})

		providers, total, err := svc.ListProviders(context.Background(), 1, 20, capability.PlatformOpenAI, capability.ProviderTypeOAuth, billing.StatusActive, "acc2", 0, openai.PrivacyModeCFBlocked, "", "")
		require.NoError(t, err)
		require.Equal(t, int64(1), total)
		require.Equal(t, []provider.Record{{ID: 2, Name: "acc2"}}, providers)
		require.Equal(t, openai.PrivacyModeCFBlocked, repo.listWithFiltersPrivacy)
	})
}
