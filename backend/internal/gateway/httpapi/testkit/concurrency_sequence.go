package testkit

import (
	"context"
	"sync"

	"github.com/TokenFlux/TokenRouter/internal/scheduler"
)

type ConcurrencySequence struct {
	Mu sync.Mutex

	ProviderSeq []bool
	UserSeq     []bool

	ProviderAcquireCalls int
	UserAcquireCalls     int
	ProviderReleaseCalls int
	UserReleaseCalls     int
	WaitAllowed          bool
	WaitIncrementCalls   int
	WaitDecrementCalls   int
	WaitMaxWait          int
	WaitIncrementHook    func()
	APIKeyTrackCalls     int
	APIKeyReleaseCalls   int
	APIKeyTrackIDs       []int64
}

type ConcurrencySequenceWithError struct {
	ConcurrencySequence
	Err error
}

func (s *ConcurrencySequence) AcquireProviderSlot(ctx context.Context, providerID int64, maxConcurrency int, requestID string) (bool, error) {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	s.ProviderAcquireCalls++
	if len(s.ProviderSeq) == 0 {
		return false, nil
	}
	v := s.ProviderSeq[0]
	s.ProviderSeq = s.ProviderSeq[1:]
	return v, nil
}

func (s *ConcurrencySequence) ReleaseProviderSlot(ctx context.Context, providerID int64, requestID string) error {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	s.ProviderReleaseCalls++
	return nil
}

func (s *ConcurrencySequence) GetProviderConcurrency(ctx context.Context, providerID int64) (int, error) {
	return 0, nil
}

func (s *ConcurrencySequence) GetProviderConcurrencyBatch(ctx context.Context, providerIDs []int64) (map[int64]int, error) {
	out := make(map[int64]int, len(providerIDs))
	for _, providerID := range providerIDs {
		out[providerID] = 0
	}
	return out, nil
}

func (s *ConcurrencySequence) IncrementProviderWaitCount(ctx context.Context, providerID int64, maxWait int) (bool, error) {
	return true, nil
}

func (s *ConcurrencySequence) DecrementProviderWaitCount(ctx context.Context, providerID int64) error {
	return nil
}

func (s *ConcurrencySequence) GetProviderWaitingCount(ctx context.Context, providerID int64) (int, error) {
	return 0, nil
}

func (s *ConcurrencySequence) AcquireUserSlot(ctx context.Context, userID int64, maxConcurrency int, requestID string) (bool, error) {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	s.UserAcquireCalls++
	if len(s.UserSeq) == 0 {
		return false, nil
	}
	v := s.UserSeq[0]
	s.UserSeq = s.UserSeq[1:]
	return v, nil
}

func (s *ConcurrencySequence) ReleaseUserSlot(ctx context.Context, userID int64, requestID string) error {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	s.UserReleaseCalls++
	return nil
}

func (s *ConcurrencySequence) GetUserConcurrency(ctx context.Context, userID int64) (int, error) {
	return 0, nil
}

func (s *ConcurrencySequence) TrackAPIKeySlot(ctx context.Context, apiKeyID int64, requestID string) error {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	s.APIKeyTrackCalls++
	s.APIKeyTrackIDs = append(s.APIKeyTrackIDs, apiKeyID)
	return nil
}

func (s *ConcurrencySequence) ReleaseAPIKeySlot(ctx context.Context, apiKeyID int64, requestID string) error {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	s.APIKeyReleaseCalls++
	return nil
}

func (s *ConcurrencySequence) GetAPIKeyConcurrencyBatch(ctx context.Context, apiKeyIDs []int64) (map[int64]int, error) {
	out := make(map[int64]int, len(apiKeyIDs))
	for _, apiKeyID := range apiKeyIDs {
		out[apiKeyID] = 0
	}
	return out, nil
}

func (s *ConcurrencySequence) IncrementWaitCount(ctx context.Context, userID int64, maxWait int) (bool, error) {
	s.Mu.Lock()
	s.WaitIncrementCalls++
	s.WaitMaxWait = maxWait
	waitAllowed := s.WaitAllowed
	hook := s.WaitIncrementHook
	s.Mu.Unlock()

	if hook != nil {
		hook()
	}
	if !waitAllowed {
		return false, nil
	}
	return true, nil
}

func (s *ConcurrencySequence) DecrementWaitCount(ctx context.Context, userID int64) error {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	s.WaitDecrementCalls++
	return nil
}

func (s *ConcurrencySequence) GetProvidersLoadBatch(ctx context.Context, providers []scheduler.ProviderWithConcurrency) (map[int64]*scheduler.ProviderLoadInfo, error) {
	out := make(map[int64]*scheduler.ProviderLoadInfo, len(providers))
	for _, acc := range providers {
		out[acc.ID] = &scheduler.ProviderLoadInfo{ProviderID: acc.ID}
	}
	return out, nil
}

func (s *ConcurrencySequence) GetUsersLoadBatch(ctx context.Context, users []scheduler.UserWithConcurrency) (map[int64]*scheduler.UserLoadInfo, error) {
	out := make(map[int64]*scheduler.UserLoadInfo, len(users))
	for _, user := range users {
		out[user.ID] = &scheduler.UserLoadInfo{UserID: user.ID}
	}
	return out, nil
}

func (s *ConcurrencySequence) CleanupExpiredProviderSlots(ctx context.Context, providerID int64) error {
	return nil
}

func (s *ConcurrencySequence) CleanupExpiredProviderSlotKeys(ctx context.Context) error {
	return nil
}

func (s *ConcurrencySequence) CleanupStaleProcessSlots(ctx context.Context, activeRequestPrefix string) error {
	return nil
}

func (s *ConcurrencySequenceWithError) AcquireProviderSlot(ctx context.Context, providerID int64, maxConcurrency int, requestID string) (bool, error) {
	return false, s.Err
}
