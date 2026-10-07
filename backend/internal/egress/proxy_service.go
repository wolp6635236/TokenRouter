package egress

import (
	"context"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/pkg/apperror"
	"github.com/TokenFlux/TokenRouter/internal/pkg/pagination"
)

var (
	ErrProxyNotFound = apperror.NotFound("PROXY_NOT_FOUND", "proxy not found")
	ErrProxyInUse    = apperror.Conflict("PROXY_IN_USE", "proxy is in use by providers")
)

type ProxyRepository interface {
	Create(ctx context.Context, proxy *Proxy) error
	GetByID(ctx context.Context, id int64) (*Proxy, error)
	ListByIDs(ctx context.Context, ids []int64) ([]Proxy, error)
	Update(ctx context.Context, proxy *Proxy) error
	Delete(ctx context.Context, id int64) error

	List(ctx context.Context, params pagination.PaginationParams) ([]Proxy, *pagination.PaginationResult, error)
	ListWithFilters(ctx context.Context, params pagination.PaginationParams, protocol, status, search string) ([]Proxy, *pagination.PaginationResult, error)
	ListWithFiltersAndProviderCount(ctx context.Context, params pagination.PaginationParams, protocol, status, search string) ([]ProxyWithProviderCount, *pagination.PaginationResult, error)
	ListActive(ctx context.Context) ([]Proxy, error)
	ListActiveWithProviderCount(ctx context.Context) ([]ProxyWithProviderCount, error)

	ExistsByHostPortAuth(ctx context.Context, host string, port int, username, password string) (bool, error)
	CountProvidersByProxyID(ctx context.Context, proxyID int64) (int64, error)
	ListProviderSummariesByProxyID(ctx context.Context, proxyID int64) ([]ProxyProviderSummary, error)

	SweepExpiredProxies(ctx context.Context, now time.Time) (changed int64, err error)
	ListAllForFallback(ctx context.Context) ([]Proxy, error)
	CountExpired(ctx context.Context) (int64, error)
	CountExpiringSoon(ctx context.Context, now time.Time) (int64, error)
}
