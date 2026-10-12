package egress

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/pkg/pagination"
)

type proxyRepoStub struct {
	deleteErr     error
	countErr      error
	providerCount int64
	deletedIDs    []int64
}

type proxyRepoStubForAdminList struct {
	ProxyRepository

	listWithFiltersCalls    int
	listWithFiltersParams   pagination.PaginationParams
	listWithFiltersProtocol string
	listWithFiltersStatus   string
	listWithFiltersSearch   string
	listWithFiltersProxies  []Proxy
	listWithFiltersResult   *pagination.PaginationResult
	listWithFiltersErr      error

	listWithFiltersAndProviderCountCalls    int
	listWithFiltersAndProviderCountParams   pagination.PaginationParams
	listWithFiltersAndProviderCountProtocol string
	listWithFiltersAndProviderCountStatus   string
	listWithFiltersAndProviderCountSearch   string
	listWithFiltersAndProviderCountProxies  []ProxyWithProviderCount
	listWithFiltersAndProviderCountResult   *pagination.PaginationResult
	listWithFiltersAndProviderCountErr      error
}

type updatingProxyRepoStub struct {
	*proxyRepoStub
	proxy       *Proxy
	updateCalls int
}

func (s *proxyRepoStub) Create(ctx context.Context, proxy *Proxy) error {
	panic("unexpected Create call")
}

func (s *proxyRepoStub) GetByID(ctx context.Context, id int64) (*Proxy, error) {
	panic("unexpected GetByID call")
}

func (s *proxyRepoStub) ListByIDs(ctx context.Context, ids []int64) ([]Proxy, error) {
	panic("unexpected ListByIDs call")
}

func (s *proxyRepoStub) Update(ctx context.Context, proxy *Proxy) error {
	panic("unexpected Update call")
}

func (s *proxyRepoStub) Delete(ctx context.Context, id int64) error {
	s.deletedIDs = append(s.deletedIDs, id)
	return s.deleteErr
}

func (s *proxyRepoStub) List(ctx context.Context, params pagination.PaginationParams) ([]Proxy, *pagination.PaginationResult, error) {
	panic("unexpected List call")
}

func (s *proxyRepoStub) ListWithFilters(ctx context.Context, params pagination.PaginationParams, protocol, status, search string) ([]Proxy, *pagination.PaginationResult, error) {
	panic("unexpected ListWithFilters call")
}

func (s *proxyRepoStub) ListActive(ctx context.Context) ([]Proxy, error) {
	panic("unexpected ListActive call")
}

func (s *proxyRepoStub) ListActiveWithProviderCount(ctx context.Context) ([]ProxyWithProviderCount, error) {
	panic("unexpected ListActiveWithProviderCount call")
}

func (s *proxyRepoStub) ListWithFiltersAndProviderCount(ctx context.Context, params pagination.PaginationParams, protocol, status, search string) ([]ProxyWithProviderCount, *pagination.PaginationResult, error) {
	panic("unexpected ListWithFiltersAndProviderCount call")
}

func (s *proxyRepoStub) ExistsByHostPortAuth(ctx context.Context, host string, port int, username, password string) (bool, error) {
	panic("unexpected ExistsByHostPortAuth call")
}

func (s *proxyRepoStub) CountProvidersByProxyID(ctx context.Context, proxyID int64) (int64, error) {
	if s.countErr != nil {
		return 0, s.countErr
	}
	return s.providerCount, nil
}

func (s *proxyRepoStub) ListProviderSummariesByProxyID(ctx context.Context, proxyID int64) ([]ProxyProviderSummary, error) {
	panic("unexpected ListProviderSummariesByProxyID call")
}

func (s *proxyRepoStub) SweepExpiredProxies(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}

func (s *proxyRepoStub) ListAllForFallback(_ context.Context) ([]Proxy, error) {
	return nil, nil
}

func (s *proxyRepoStub) CountExpired(_ context.Context) (int64, error) {
	return 0, nil
}

func (s *proxyRepoStub) CountExpiringSoon(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}

func TestAdminService_DeleteProxy_Success(t *testing.T) {
	repo := &proxyRepoStub{}
	svc := NewProxyAdmin(repo, nil, nil, nil, ProxyAdminOptions{})

	err := svc.DeleteProxy(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, []int64{7}, repo.deletedIDs)
}

func TestAdminService_DeleteProxy_Idempotent(t *testing.T) {
	repo := &proxyRepoStub{}
	svc := NewProxyAdmin(repo, nil, nil, nil, ProxyAdminOptions{})

	err := svc.DeleteProxy(context.Background(), 404)
	require.NoError(t, err)
	require.Equal(t, []int64{404}, repo.deletedIDs)
}

func TestAdminService_DeleteProxy_InUse(t *testing.T) {
	repo := &proxyRepoStub{providerCount: 2}
	svc := NewProxyAdmin(repo, nil, nil, nil, ProxyAdminOptions{})

	err := svc.DeleteProxy(context.Background(), 77)
	require.ErrorIs(t, err, ErrProxyInUse)
	require.Empty(t, repo.deletedIDs)
}

func TestAdminService_DeleteProxy_Error(t *testing.T) {
	deleteErr := errors.New("delete failed")
	repo := &proxyRepoStub{deleteErr: deleteErr}
	svc := NewProxyAdmin(repo, nil, nil, nil, ProxyAdminOptions{})

	err := svc.DeleteProxy(context.Background(), 33)
	require.ErrorIs(t, err, deleteErr)
}

func (s *proxyRepoStubForAdminList) ListWithFilters(_ context.Context, params pagination.PaginationParams, protocol, status, search string) ([]Proxy, *pagination.PaginationResult, error) {
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

func (s *proxyRepoStubForAdminList) ListWithFiltersAndProviderCount(_ context.Context, params pagination.PaginationParams, protocol, status, search string) ([]ProxyWithProviderCount, *pagination.PaginationResult, error) {
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
			listWithFiltersProxies: []Proxy{{ID: 2, Name: "p1"}},
			listWithFiltersResult:  &pagination.PaginationResult{Total: 7},
		}
		svc := NewProxyAdmin(repo, nil, nil, nil, ProxyAdminOptions{})

		proxies, total, err := svc.ListProxies(context.Background(), 3, 50, "http", billing.StatusActive, "p1", "name", "ASC")
		require.NoError(t, err)
		require.Equal(t, int64(7), total)
		require.Equal(t, []Proxy{{ID: 2, Name: "p1"}}, proxies)

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
			listWithFiltersAndProviderCountProxies: []ProxyWithProviderCount{{Proxy: Proxy{ID: 3, Name: "p2"}, ProviderCount: 5}},
			listWithFiltersAndProviderCountResult:  &pagination.PaginationResult{Total: 9},
		}
		svc := NewProxyAdmin(repo, nil, nil, nil, ProxyAdminOptions{})

		proxies, total, err := svc.ListProxiesWithProviderCount(context.Background(), 2, 10, "socks5", billing.StatusDisabled, "p2", "provider_count", "DESC")
		require.NoError(t, err)
		require.Equal(t, int64(9), total)
		require.Equal(t, []ProxyWithProviderCount{{Proxy: Proxy{ID: 3, Name: "p2"}, ProviderCount: 5}}, proxies)

		require.Equal(t, 1, repo.listWithFiltersAndProviderCountCalls)
		require.Equal(t, pagination.PaginationParams{Page: 2, PageSize: 10, SortBy: "provider_count", SortOrder: "DESC"}, repo.listWithFiltersAndProviderCountParams)
		require.Equal(t, "socks5", repo.listWithFiltersAndProviderCountProtocol)
		require.Equal(t, billing.StatusDisabled, repo.listWithFiltersAndProviderCountStatus)
		require.Equal(t, "p2", repo.listWithFiltersAndProviderCountSearch)
	})
}

func TestFinalizeProxyQualityResult_ScoreAndGrade(t *testing.T) {
	result := &ProxyQualityCheckResult{
		PassedCount:    2,
		WarnCount:      1,
		FailedCount:    1,
		ChallengeCount: 1,
	}

	finalizeProxyQualityResult(result)

	require.Equal(t, 38, result.Score)
	require.Equal(t, "F", result.Grade)
	require.Contains(t, result.Summary, "通过 2 项")
	require.Contains(t, result.Summary, "告警 1 项")
	require.Contains(t, result.Summary, "失败 1 项")
	require.Contains(t, result.Summary, "挑战 1 项")
}

func (s *updatingProxyRepoStub) GetByID(context.Context, int64) (*Proxy, error) {
	copy := *s.proxy
	return &copy, nil
}

func (s *updatingProxyRepoStub) Update(_ context.Context, proxy *Proxy) error {
	s.updateCalls++
	copy := *proxy
	s.proxy = &copy
	return nil
}

func TestProxyAdminUpdateUsesRepositoryUpdate(t *testing.T) {
	t.Run("adminService", func(t *testing.T) {
		repo := &updatingProxyRepoStub{
			proxyRepoStub: &proxyRepoStub{},
			proxy: &Proxy{
				ID:             9,
				Protocol:       "http",
				Host:           "old.example",
				Port:           8080,
				Status:         billing.StatusActive,
				FallbackMode:   FallbackModeNone,
				ExpiryWarnDays: 7,
			},
		}
		svc := NewProxyAdmin(repo, nil, nil, nil, ProxyAdminOptions{})

		_, err := svc.UpdateProxy(context.Background(), 9, &UpdateProxyInput{
			Host:           "new.example",
			FallbackMode:   FallbackModeNone,
			ExpiryWarnDays: 7,
		})

		require.NoError(t, err)
		require.Equal(t, 1, repo.updateCalls)
		require.Equal(t, "new.example", repo.proxy.Host)
	})
}
