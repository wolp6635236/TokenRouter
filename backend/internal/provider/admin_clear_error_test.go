package provider_test

import (
	"context"
	"testing"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

type providerRepoStubForClearProviderError struct {
	providercore.AdminStore
	provider                 *providercore.Record
	clearErrorCalls          int
	clearRateLimitCalls      int
	clearAntigravityCalls    int
	clearModelRateLimitCalls int
	clearTempUnschedCalls    int
}

func (r *providerRepoStubForClearProviderError) GetByID(ctx context.Context, id int64) (*providercore.Record, error) {
	return providercore.CloneRecord(r.provider), nil
}

func (r *providerRepoStubForClearProviderError) ClearError(ctx context.Context, id int64) error {
	r.clearErrorCalls++
	r.provider.Status = billing.StatusActive
	r.provider.ErrorMessage = ""
	return nil
}

func (r *providerRepoStubForClearProviderError) ClearRateLimit(ctx context.Context, id int64) error {
	r.clearRateLimitCalls++
	r.provider.RateLimitedAt = nil
	r.provider.RateLimitResetAt = nil
	return nil
}

func (r *providerRepoStubForClearProviderError) ClearAntigravityQuotaScopes(ctx context.Context, id int64) error {
	r.clearAntigravityCalls++
	return nil
}

func (r *providerRepoStubForClearProviderError) ClearModelRateLimits(ctx context.Context, id int64) error {
	r.clearModelRateLimitCalls++
	return nil
}

func (r *providerRepoStubForClearProviderError) ClearTempUnschedulable(ctx context.Context, id int64) error {
	r.clearTempUnschedCalls++
	r.provider.TempUnschedulableUntil = nil
	r.provider.TempUnschedulableReason = ""
	return nil
}

func TestAdminService_ClearProviderError_AlsoClearsRecoverableRuntimeState(t *testing.T) {
	until := time.Now().Add(10 * time.Minute)
	resetAt := time.Now().Add(5 * time.Minute)
	repo := &providerRepoStubForClearProviderError{
		provider: &providercore.Record{
			ID:                      31,
			Platform:                capability.PlatformOpenAI,
			Type:                    capability.ProviderTypeOAuth,
			Status:                  providercore.StatusError,
			ErrorMessage:            "refresh failed",
			RateLimitResetAt:        &resetAt,
			TempUnschedulableUntil:  &until,
			TempUnschedulableReason: "missing refresh token",
		},
	}
	blocker := &runtimeBlockRecorder{}
	svc := providercore.NewAdmin(repo, providercore.AdminOptions{RuntimeBlocker: blocker})

	updated, err := svc.ClearProviderError(context.Background(), 31)
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Equal(t, 1, repo.clearErrorCalls)
	require.Equal(t, 1, repo.clearRateLimitCalls)
	require.Equal(t, 1, repo.clearAntigravityCalls)
	require.Equal(t, 1, repo.clearModelRateLimitCalls)
	require.Equal(t, 1, repo.clearTempUnschedCalls)
	require.Nil(t, updated.RateLimitResetAt)
	require.Nil(t, updated.TempUnschedulableUntil)
	require.Empty(t, updated.TempUnschedulableReason)
	require.Equal(t, []int64{31}, blocker.clearedIDs)
}

// runtimeBlockRecorder 记录管理员恢复后的内存阻断清理。
type runtimeBlockRecorder struct{ clearedIDs []int64 }

func (r *runtimeBlockRecorder) ClearProviderSchedulingBlock(id int64) {
	r.clearedIDs = append(r.clearedIDs, id)
}
