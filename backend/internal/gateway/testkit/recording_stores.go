package testkit

import (
	"context"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/gateway/completion"
	"github.com/TokenFlux/TokenRouter/internal/identity"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	usagecore "github.com/TokenFlux/TokenRouter/internal/usage"
)

type BestEffortUsageLogStore struct {
	usagecore.UsageLogRepository

	BestEffortErr   error
	CreateErr       error
	BestEffortCalls int
	CreateCalls     int
	LastLog         *usagecore.UsageLog
	LastCtxErr      error
}

type UsageLogStore struct {
	usagecore.UsageLogRepository

	Inserted   bool
	Err        error
	Calls      int
	LastLog    *usagecore.UsageLog
	LastCtxErr error
}

type SettlementStore struct {
	completion.Store

	Result       *billing.UsageBillingApplyResult
	Err          error
	Calls        int
	LastCmd      *billing.UsageBillingCommand
	LastCtxErr   error
	ResolveSub   *billing.UserSubscription
	ResolveCalls int
}
type ProviderLookup struct {
	Provider *providercore.Record
	Calls    int
}

type UserStore struct {
	identity.UserRepository

	DeductCalls int
	DeductErr   error
	LastAmount  float64
	LastCtxErr  error
}

type SubscriptionStore struct {
	billing.UserSubscriptionRepository

	IncrementCalls int
	IncrementErr   error
	LastCtxErr     error
}

type KeyQuotaUpdater struct {
	QuotaCalls          int
	RateLimitCalls      int
	Err                 error
	LastAmount          float64
	LastQuotaCtxErr     error
	LastRateLimitCtxErr error
}

type GroupRateStore struct {
	billing.UserGroupRateRepository

	Rate  *float64
	Err   error
	Calls int
}

func (s *BestEffortUsageLogStore) CreateBestEffort(ctx context.Context, log *usagecore.UsageLog) error {
	s.BestEffortCalls++
	s.LastLog = log
	s.LastCtxErr = ctx.Err()
	return s.BestEffortErr
}

func (s *BestEffortUsageLogStore) Create(ctx context.Context, log *usagecore.UsageLog) (bool, error) {
	s.CreateCalls++
	s.LastLog = log
	s.LastCtxErr = ctx.Err()
	return false, s.CreateErr
}

func (s *UsageLogStore) Create(ctx context.Context, log *usagecore.UsageLog) (bool, error) {
	s.Calls++
	s.LastLog = log
	s.LastCtxErr = ctx.Err()
	return s.Inserted, s.Err
}

func (s *ProviderLookup) GetByID(_ context.Context, _ int64) (*providercore.Record, error) {
	s.Calls++
	return s.Provider, nil
}

func (s *SettlementStore) Apply(ctx context.Context, cmd *billing.UsageBillingCommand) (*billing.UsageBillingApplyResult, error) {
	s.Calls++
	s.LastCmd = cmd
	s.LastCtxErr = ctx.Err()
	if s.Err != nil {
		return nil, s.Err
	}
	if s.Result != nil {
		return s.Result, nil
	}
	result := &billing.UsageBillingApplyResult{Applied: true}
	if cmd != nil {
		switch cmd.BillingType {
		case usagecore.BillingTypeSubscription:
			result.SubscriptionAmountUSD = cmd.BillableAmountUSD
		default:
			result.BalanceAmountUSD = cmd.BillableAmountUSD
		}
	}
	return result, nil
}

func (s *SettlementStore) ResolveUsableSubscriptionForGroup(ctx context.Context, userID, groupID int64) (*billing.UserSubscription, error) {
	s.ResolveCalls++
	return s.ResolveSub, nil
}

func (s *UserStore) DeductBalance(ctx context.Context, id int64, amount float64) (float64, error) {
	s.DeductCalls++
	s.LastAmount = amount
	s.LastCtxErr = ctx.Err()
	if s.DeductErr != nil {
		return 0, s.DeductErr
	}
	return amount, nil
}

func (s *UserStore) AdjustBalance(ctx context.Context, id int64, delta float64) (identity.BalanceChange, error) {
	panic("unexpected AdjustBalance call")
}

func (s *UserStore) SetBalance(ctx context.Context, id int64, value float64) (identity.BalanceChange, error) {
	panic("unexpected SetBalance call")
}

func (s *SubscriptionStore) IncrementUsage(ctx context.Context, id int64, costUSD float64) error {
	s.IncrementCalls++
	s.LastCtxErr = ctx.Err()
	return s.IncrementErr
}

func (s *KeyQuotaUpdater) UpdateQuotaUsed(ctx context.Context, apiKeyID int64, cost float64) error {
	s.QuotaCalls++
	s.LastAmount = cost
	s.LastQuotaCtxErr = ctx.Err()
	return s.Err
}

func (s *KeyQuotaUpdater) UpdateRateLimitUsage(ctx context.Context, apiKeyID int64, cost float64) error {
	s.RateLimitCalls++
	s.LastAmount = cost
	s.LastRateLimitCtxErr = ctx.Err()
	return s.Err
}

func (s *GroupRateStore) GetByUserAndGroup(ctx context.Context, userID, groupID int64) (*float64, error) {
	s.Calls++
	if s.Err != nil {
		return nil, s.Err
	}
	return s.Rate, nil
}
