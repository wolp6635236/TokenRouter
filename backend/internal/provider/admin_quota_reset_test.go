package provider_test

import (
	"context"
	"errors"
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/provider"

	"github.com/stretchr/testify/require"
)

type resetProviderQuotaRepoStub struct {
	provider.AdminStore
	provider            *provider.Record
	getByIDErr          error
	resetErr            error
	resetCalls          int
	clearRateLimitCalls int
	callOrder           []string
	overloaded          bool
}

func (r *resetProviderQuotaRepoStub) GetByID(context.Context, int64) (*provider.Record, error) {
	return provider.CloneRecord(r.provider), r.getByIDErr
}

func (r *resetProviderQuotaRepoStub) ResetQuotaUsedAndClearRateLimitCooldown(context.Context, int64) error {
	r.resetCalls++
	r.callOrder = append(r.callOrder, "reset_quota_and_clear_rate_limit_cooldown")
	return r.resetErr
}

func (r *resetProviderQuotaRepoStub) ClearRateLimit(context.Context, int64) error {
	r.clearRateLimitCalls++
	r.callOrder = append(r.callOrder, "clear_rate_limit")
	r.overloaded = false
	return nil
}

func TestResetProviderQuota_ClearsSchedulerRateLimitWithoutClearingOverload(t *testing.T) {
	repo := &resetProviderQuotaRepoStub{provider: &provider.Record{ID: 42}, overloaded: true}
	svc := provider.NewAdmin(repo, provider.AdminOptions{Quotas: repo})

	err := svc.ResetProviderQuota(context.Background(), 42)

	require.NoError(t, err)
	require.Equal(t, 1, repo.resetCalls)
	require.Zero(t, repo.clearRateLimitCalls)
	require.Equal(t, []string{"reset_quota_and_clear_rate_limit_cooldown"}, repo.callOrder)
	require.True(t, repo.overloaded, "quota reset must preserve an unrelated overload block")
}

func TestResetProviderQuota_PreservesLookupAndSparkShadowShortCircuits(t *testing.T) {
	t.Run("lookup failure", func(t *testing.T) {
		getErr := errors.New("get provider failed")
		repo := &resetProviderQuotaRepoStub{getByIDErr: getErr}
		svc := provider.NewAdmin(repo, provider.AdminOptions{Quotas: repo})

		err := svc.ResetProviderQuota(context.Background(), 42)

		require.ErrorIs(t, err, getErr)
		require.Zero(t, repo.resetCalls)
		require.Zero(t, repo.clearRateLimitCalls)
	})

	t.Run("spark shadow", func(t *testing.T) {
		parentID := int64(7)
		repo := &resetProviderQuotaRepoStub{
			provider: &provider.Record{ID: 42, ParentProviderID: &parentID},
		}
		svc := provider.NewAdmin(repo, provider.AdminOptions{Quotas: repo})

		err := svc.ResetProviderQuota(context.Background(), 42)

		require.Error(t, err)
		require.Zero(t, repo.resetCalls)
		require.Zero(t, repo.clearRateLimitCalls)
	})
}

func TestResetProviderQuota_PropagatesAtomicRepositoryFailure(t *testing.T) {
	resetErr := errors.New("atomic reset failed")
	repo := &resetProviderQuotaRepoStub{
		provider: &provider.Record{ID: 42},
		resetErr: resetErr,
	}
	svc := provider.NewAdmin(repo, provider.AdminOptions{Quotas: repo})

	err := svc.ResetProviderQuota(context.Background(), 42)

	require.ErrorIs(t, err, resetErr)
	require.Equal(t, 1, repo.resetCalls)
	require.Zero(t, repo.clearRateLimitCalls)
}
