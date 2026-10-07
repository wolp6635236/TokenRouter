package httpapi

import (
	"context"
	"errors"
	"time"

	gatewaytestkit "github.com/TokenFlux/TokenRouter/internal/gateway/testkit"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
)

type grokQuotaProviderRepo struct {
	*grokFixtureProviders
	updates               map[int64]map[string]any
	updateCalls           int
	rateLimitedCalls      int
	lastRateLimitedID     int64
	lastRateLimitResetAt  time.Time
	tempUnschedCalls      int
	lastTempUnschedID     int64
	lastTempUnschedUntil  time.Time
	lastTempUnschedReason string
	recoveryClearCalls    int
	recoveryObservedAt    time.Time
	recoveryObservedReset time.Time
	recoveryClearResult   bool
}

func (r *grokQuotaProviderRepo) UpdateExtra(_ context.Context, id int64, updates map[string]any) error {
	r.updateCalls++
	if r.updates == nil {
		r.updates = make(map[int64]map[string]any)
	}
	r.updates[id] = updates
	if r.grokFixtureProviders != nil {
		value := r.providersByID[id]
		if value != nil {
			if value.Record.Extra == nil {
				value.Record.Extra = make(map[string]any)
			}
			for key, v := range updates {
				value.Record.Extra[key] = v
			}
		}
	}

	return nil
}

func (r *grokQuotaProviderRepo) SetRateLimited(_ context.Context, id int64, resetAt time.Time) error {
	r.rateLimitedCalls++
	r.lastRateLimitedID = id
	r.lastRateLimitResetAt = resetAt
	return nil
}

func (r *grokQuotaProviderRepo) SetRateLimitedIfLater(ctx context.Context, id int64, resetAt time.Time) error {
	return r.SetRateLimited(ctx, id, resetAt)
}

func (r *grokQuotaProviderRepo) ClearRateLimitIfObserved(_ context.Context, _ int64, observedLimitedAt, observedResetAt time.Time) (bool, error) {
	r.recoveryClearCalls++
	r.recoveryObservedAt = observedLimitedAt
	r.recoveryObservedReset = observedResetAt
	return r.recoveryClearResult, nil
}

func (r *grokQuotaProviderRepo) SetTempUnschedulable(_ context.Context, id int64, until time.Time, reason string) error {
	r.tempUnschedCalls++
	r.lastTempUnschedID = id
	r.lastTempUnschedUntil = until
	r.lastTempUnschedReason = reason
	return nil
}

// grokFixtureProviders 保存按 ID 回读的指针和计数，其他写入使用基础夹具。
type grokFixtureProviders struct {
	gatewaytestkit.HealthStoreBase
	providersByID map[int64]*gatewayprovider.ExecutionProvider
	getByIDCalls  int
}

func (r *grokFixtureProviders) GetByID(_ context.Context, id int64) (*gatewayprovider.ExecutionProvider, error) {
	r.getByIDCalls++
	if value, ok := r.providersByID[id]; ok {
		return value, nil
	}
	return nil, errors.New("provider not found")
}

func newHTTPGrokTokenFixture(store gatewayprovider.ExecutionProviderStore, cache providercore.AccessTokenCache) *providercore.GrokTokenSource {
	return &providercore.GrokTokenSource{Repository: gatewaytestkit.TokenRepository(store), Cache: cache, Policy: providercore.GrokProviderRefreshPolicy()}
}
