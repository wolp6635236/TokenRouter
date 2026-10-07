package egress_test

import (
	"context"
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/egress"
	"github.com/TokenFlux/TokenRouter/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type proxyRepoStubForAdminList struct {
	egress.ProxyRepository

	listWithFiltersCalls    int
	listWithFiltersParams   pagination.PaginationParams
	listWithFiltersProtocol string
	listWithFiltersStatus   string
	listWithFiltersSearch   string
	listWithFiltersProxies  []egress.Proxy
	listWithFiltersResult   *pagination.PaginationResult
	listWithFiltersErr      error

	listWithFiltersAndProviderCountCalls    int
	listWithFiltersAndProviderCountParams   pagination.PaginationParams
	listWithFiltersAndProviderCountProtocol string
	listWithFiltersAndProviderCountStatus   string
	listWithFiltersAndProviderCountSearch   string
	listWithFiltersAndProviderCountProxies  []egress.ProxyWithProviderCount
	listWithFiltersAndProviderCountResult   *pagination.PaginationResult
	listWithFiltersAndProviderCountErr      error
}

func (s *proxyRepoStubForAdminList) ListWithFilters(_ context.Context, params pagination.PaginationParams, protocol, status, search string) ([]egress.Proxy, *pagination.PaginationResult, error) {
	s.listWithFiltersCalls++
	s.listWithFiltersParams = params
	s.listWithFiltersProtocol = protocol
	s.listWithFiltersStatus = status
	s.listWithFiltersSearch = search

	if s.listWithFiltersErr != nil {
		return nil, nil, s.listWithFiltersErr
	}

	result := s.listWithFiltersResult
	if result == nil {
		result = &pagination.PaginationResult{
			Total:    int64(len(s.listWithFiltersProxies)),
			Page:     params.Page,
			PageSize: params.PageSize,
		}
	}

	return s.listWithFiltersProxies, result, nil
}

func (s *proxyRepoStubForAdminList) ListWithFiltersAndProviderCount(_ context.Context, params pagination.PaginationParams, protocol, status, search string) ([]egress.ProxyWithProviderCount, *pagination.PaginationResult, error) {
	s.listWithFiltersAndProviderCountCalls++
	s.listWithFiltersAndProviderCountParams = params
	s.listWithFiltersAndProviderCountProtocol = protocol
	s.listWithFiltersAndProviderCountStatus = status
	s.listWithFiltersAndProviderCountSearch = search

	if s.listWithFiltersAndProviderCountErr != nil {
		return nil, nil, s.listWithFiltersAndProviderCountErr
	}

	result := s.listWithFiltersAndProviderCountResult
	if result == nil {
		result = &pagination.PaginationResult{
			Total:    int64(len(s.listWithFiltersAndProviderCountProxies)),
			Page:     params.Page,
			PageSize: params.PageSize,
		}
	}

	return s.listWithFiltersAndProviderCountProxies, result, nil
}

func TestAdminService_ListProxies_WithSearch(t *testing.T) {
	t.Run("search 参数正常传递到 repository 层", func(t *testing.T) {
		repo := &proxyRepoStubForAdminList{
			listWithFiltersProxies: []egress.Proxy{{ID: 2, Name: "p1"}},
			listWithFiltersResult:  &pagination.PaginationResult{Total: 7},
		}
		svc := egress.NewProxyAdmin(repo, nil, nil, nil, egress.ProxyAdminOptions{})

		proxies, total, err := svc.ListProxies(context.Background(), 3, 50, "http", billing.StatusActive, "p1", "name", "ASC")
		require.NoError(t, err)
		require.Equal(t, int64(7), total)
		require.Equal(t, []egress.Proxy{{ID: 2, Name: "p1"}}, proxies)

		require.Equal(t, 1, repo.listWithFiltersCalls)
		require.Equal(t, pagination.PaginationParams{Page: 3, PageSize: 50, SortBy: "name", SortOrder: "ASC"}, repo.listWithFiltersParams)
		require.Equal(t, "http", repo.listWithFiltersProtocol)
		require.Equal(t, billing.StatusActive, repo.listWithFiltersStatus)
		require.Equal(t, "p1", repo.listWithFiltersSearch)
	})
}

func TestAdminService_ListProxiesWithProviderCount_WithSearch(t *testing.T) {
	t.Run("search 参数正常传递到 repository 层", func(t *testing.T) {
		repo := &proxyRepoStubForAdminList{
			listWithFiltersAndProviderCountProxies: []egress.ProxyWithProviderCount{{Proxy: egress.Proxy{ID: 3, Name: "p2"}, ProviderCount: 5}},
			listWithFiltersAndProviderCountResult:  &pagination.PaginationResult{Total: 9},
		}
		svc := egress.NewProxyAdmin(repo, nil, nil, nil, egress.ProxyAdminOptions{})

		proxies, total, err := svc.ListProxiesWithProviderCount(context.Background(), 2, 10, "socks5", billing.StatusDisabled, "p2", "provider_count", "DESC")
		require.NoError(t, err)
		require.Equal(t, int64(9), total)
		require.Equal(t, []egress.ProxyWithProviderCount{{Proxy: egress.Proxy{ID: 3, Name: "p2"}, ProviderCount: 5}}, proxies)

		require.Equal(t, 1, repo.listWithFiltersAndProviderCountCalls)
		require.Equal(t, pagination.PaginationParams{Page: 2, PageSize: 10, SortBy: "provider_count", SortOrder: "DESC"}, repo.listWithFiltersAndProviderCountParams)
		require.Equal(t, "socks5", repo.listWithFiltersAndProviderCountProtocol)
		require.Equal(t, billing.StatusDisabled, repo.listWithFiltersAndProviderCountStatus)
		require.Equal(t, "p2", repo.listWithFiltersAndProviderCountSearch)
	})
}
