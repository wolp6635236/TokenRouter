package egress_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/egress"
	"github.com/TokenFlux/TokenRouter/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type proxyRepoStub struct {
	deleteErr     error
	countErr      error
	providerCount int64
	deletedIDs    []int64
}

func (s *proxyRepoStub) Create(ctx context.Context, proxy *egress.Proxy) error {
	panic("unexpected Create call")
}

func (s *proxyRepoStub) GetByID(ctx context.Context, id int64) (*egress.Proxy, error) {
	panic("unexpected GetByID call")
}

func (s *proxyRepoStub) ListByIDs(ctx context.Context, ids []int64) ([]egress.Proxy, error) {
	panic("unexpected ListByIDs call")
}

func (s *proxyRepoStub) Update(ctx context.Context, proxy *egress.Proxy) error {
	panic("unexpected Update call")
}

func (s *proxyRepoStub) Delete(ctx context.Context, id int64) error {
	s.deletedIDs = append(s.deletedIDs, id)
	return s.deleteErr
}

func (s *proxyRepoStub) List(ctx context.Context, params pagination.PaginationParams) ([]egress.Proxy, *pagination.PaginationResult, error) {
	panic("unexpected List call")
}

func (s *proxyRepoStub) ListWithFilters(ctx context.Context, params pagination.PaginationParams, protocol, status, search string) ([]egress.Proxy, *pagination.PaginationResult, error) {
	panic("unexpected ListWithFilters call")
}

func (s *proxyRepoStub) ListActive(ctx context.Context) ([]egress.Proxy, error) {
	panic("unexpected ListActive call")
}

func (s *proxyRepoStub) ListActiveWithProviderCount(ctx context.Context) ([]egress.ProxyWithProviderCount, error) {
	panic("unexpected ListActiveWithProviderCount call")
}

func (s *proxyRepoStub) ListWithFiltersAndProviderCount(ctx context.Context, params pagination.PaginationParams, protocol, status, search string) ([]egress.ProxyWithProviderCount, *pagination.PaginationResult, error) {
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

func (s *proxyRepoStub) ListProviderSummariesByProxyID(ctx context.Context, proxyID int64) ([]egress.ProxyProviderSummary, error) {
	panic("unexpected ListProviderSummariesByProxyID call")
}

func (s *proxyRepoStub) SweepExpiredProxies(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}

func (s *proxyRepoStub) ListAllForFallback(_ context.Context) ([]egress.Proxy, error) {
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
	svc := egress.NewProxyAdmin(repo, nil, nil, nil, egress.ProxyAdminOptions{})

	err := svc.DeleteProxy(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, []int64{7}, repo.deletedIDs)
}

func TestAdminService_DeleteProxy_Idempotent(t *testing.T) {
	repo := &proxyRepoStub{}
	svc := egress.NewProxyAdmin(repo, nil, nil, nil, egress.ProxyAdminOptions{})

	err := svc.DeleteProxy(context.Background(), 404)
	require.NoError(t, err)
	require.Equal(t, []int64{404}, repo.deletedIDs)
}

func TestAdminService_DeleteProxy_InUse(t *testing.T) {
	repo := &proxyRepoStub{providerCount: 2}
	svc := egress.NewProxyAdmin(repo, nil, nil, nil, egress.ProxyAdminOptions{})

	err := svc.DeleteProxy(context.Background(), 77)
	require.ErrorIs(t, err, egress.ErrProxyInUse)
	require.Empty(t, repo.deletedIDs)
}

func TestAdminService_DeleteProxy_Error(t *testing.T) {
	deleteErr := errors.New("delete failed")
	repo := &proxyRepoStub{deleteErr: deleteErr}
	svc := egress.NewProxyAdmin(repo, nil, nil, nil, egress.ProxyAdminOptions{})

	err := svc.DeleteProxy(context.Background(), 33)
	require.ErrorIs(t, err, deleteErr)
}
