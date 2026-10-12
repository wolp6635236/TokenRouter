package billing

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSyncBalanceCacheAfterDeductionInvalidatesExhaustedBalance(t *testing.T) {
	cache := &balanceEligibilityCacheStub{
		balance:                  0.50,
		cacheMissAfterInvalidate: true,
	}
	userRepo := &balanceLoadUserRepoStub{balance: -0.25}
	cfg := &EligibilityOptions{}
	cfg.Billing.MinimumBalanceReserve = 0.01
	svc := newEligibilityForTest(cache, userRepo, nil, cfg)
	svc.Start()
	t.Cleanup(svc.Stop)

	newBalance := -0.25
	(SettlementEffects{Cache: svc}).SyncBalance(context.Background(), 1, &UsageBillingApplyResult{
		NewBalance:       &newBalance,
		BalanceAmountUSD: 0.75,
	})

	require.Equal(t, int64(1), cache.invalidateCalls.Load())
	require.Equal(t, int64(0), cache.deductCalls.Load())

	err := svc.CheckBillingEligibility(context.Background(), &UserSummary{ID: 1}, nil, nil, nil, "")
	require.ErrorIs(t, err, ErrInsufficientBalance)
	require.Equal(t, int64(1), userRepo.calls.Load())
}

func TestSyncBalanceCacheAfterDeductionInvalidatesWhenBalanceFallsBelowReserve(t *testing.T) {
	cache := &balanceEligibilityCacheStub{balance: 0.50}
	cfg := &EligibilityOptions{}
	cfg.Billing.MinimumBalanceReserve = 0.01
	svc := newEligibilityForTest(cache, nil, nil, cfg)
	svc.Start()
	t.Cleanup(svc.Stop)

	newBalance := 0.005
	(SettlementEffects{Cache: svc}).SyncBalance(context.Background(), 1, &UsageBillingApplyResult{
		NewBalance:       &newBalance,
		BalanceAmountUSD: 0.495,
	})

	require.Equal(t, int64(1), cache.invalidateCalls.Load())
	require.Equal(t, int64(0), cache.deductCalls.Load())
}

func TestSyncBalanceCacheAfterDeductionQueuesDeductWhenBalanceStillEligible(t *testing.T) {
	cache := &balanceEligibilityCacheStub{balance: 1}
	cfg := &EligibilityOptions{}
	cfg.Billing.MinimumBalanceReserve = 0.01
	svc := newEligibilityForTest(cache, nil, nil, cfg)
	svc.Start()
	t.Cleanup(svc.Stop)

	newBalance := 0.75
	(SettlementEffects{Cache: svc}).SyncBalance(context.Background(), 1, &UsageBillingApplyResult{
		NewBalance:       &newBalance,
		BalanceAmountUSD: 0.25,
	})

	require.Equal(t, int64(0), cache.invalidateCalls.Load())
	require.Eventually(t, func() bool {
		return cache.deductCalls.Load() == 1
	}, 2*time.Second, 10*time.Millisecond)
}
