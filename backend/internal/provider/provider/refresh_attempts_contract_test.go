package provider

import (
	"context"
	"errors"
	"maps"
	"reflect"
	"testing"
	"time"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

type tokenRefreshProviderRepo struct {
	refreshRecordFixture
	updateCalls                  int
	fullUpdateCalls              int
	updateCredentialsCalls       int
	setErrorCalls                int
	clearTempCalls               int
	setTempUnschedCalls          int
	updateExtraCalls             int
	lastErrorMessage             string
	lastTempUnschedReason        string
	lastExtraUpdates             map[string]any
	lastProvider                 *providercore.Record
	updateErr                    error
	cancelOnUpdate               context.CancelFunc
	conditionalErrorCalls        int
	conditionalTempCalls         int
	conditionalSuccessCalls      int
	conditionalErrorErr          error
	conditionalTempErr           error
	conditionalSuccessErr        error
	snapshotReads                bool
	respectReadContext           bool
	getByIDCalls                 int
	durableReadDelay             time.Duration
	mutateSchedulingOnSuccessCAS bool
	reauthorizeOnErrorCAS        bool
	reauthorizeOnTempCAS         bool
	repairProxyOnErrorCAS        bool
	repairProxyOnTempCAS         bool
	setErrorErr                  error
	setTempUnschedErr            error
	beforeConditionalState       func()
}

func (r *tokenRefreshProviderRepo) Update(ctx context.Context, provider *providercore.Record) error {
	r.updateCalls++
	r.fullUpdateCalls++
	r.lastProvider = provider
	return r.updateErr
}

func (r *tokenRefreshProviderRepo) UpdateCredentials(ctx context.Context, id int64, credentials map[string]any) error {
	r.updateCalls++
	r.updateCredentialsCalls++
	if r.updateErr != nil {
		return r.updateErr
	}
	cloned := maps.Clone(credentials)
	if r.providersByID != nil {
		if acc, ok := r.providersByID[id]; ok && acc != nil {
			acc.Credentials = cloned
			r.lastProvider = acc
			if r.cancelOnUpdate != nil {
				r.cancelOnUpdate()
			}
			return nil
		}
	}
	r.lastProvider = &providercore.Record{ID: id, Credentials: cloned}
	if r.cancelOnUpdate != nil {
		r.cancelOnUpdate()
	}
	return nil
}

func (r *tokenRefreshProviderRepo) GetByID(ctx context.Context, id int64) (*providercore.Record, error) {
	if r.respectReadContext && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	r.getByIDCalls++
	if r.getByIDCalls > 1 && r.durableReadDelay > 0 {
		timer := time.NewTimer(r.durableReadDelay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	provider, err := r.refreshRecordFixture.GetByID(ctx, id)
	if err != nil || !r.snapshotReads {
		return provider, err
	}
	return providercore.CloneRecord(provider), nil
}

func (r *tokenRefreshProviderRepo) SetError(ctx context.Context, id int64, errorMsg string) error {
	r.setErrorCalls++
	r.lastErrorMessage = errorMsg
	return r.setErrorErr
}

func (r *tokenRefreshProviderRepo) ClearTempUnschedulable(ctx context.Context, id int64) error {
	r.clearTempCalls++
	return nil
}

func (r *tokenRefreshProviderRepo) SetTempUnschedulable(ctx context.Context, id int64, until time.Time, reason string) error {
	r.setTempUnschedCalls++
	r.lastTempUnschedReason = reason
	return r.setTempUnschedErr
}

func (r *tokenRefreshProviderRepo) SetGrokCredentialErrorIfMatch(
	_ context.Context,
	id int64,
	snapshot providercore.CredentialMutationSnapshot,
	errorMsg string,
) (bool, error) {
	if r.beforeConditionalState != nil {
		hook := r.beforeConditionalState
		r.beforeConditionalState = nil
		hook()
	}
	provider := r.providersByID[id]
	if !grokCredentialSnapshotMatchesProvider(provider, snapshot) ||
		(errorMsg == "grok_oauth_proxy_invalid" && provider.Proxy != nil) {
		return false, nil
	}
	r.setErrorCalls++
	r.lastErrorMessage = errorMsg
	if r.setErrorErr != nil {
		return false, r.setErrorErr
	}
	provider.Status = providercore.StatusError
	provider.Schedulable = false
	provider.ErrorMessage = errorMsg
	return true, nil
}

func (r *tokenRefreshProviderRepo) SetGrokCredentialTempUnschedulableIfMatch(
	_ context.Context,
	id int64,
	snapshot providercore.CredentialMutationSnapshot,
	until time.Time,
	reason string,
) (bool, error) {
	if r.beforeConditionalState != nil {
		hook := r.beforeConditionalState
		r.beforeConditionalState = nil
		hook()
	}
	provider := r.providersByID[id]
	if !grokCredentialSnapshotMatchesProvider(provider, snapshot) {
		return false, nil
	}
	r.setTempUnschedCalls++
	r.lastTempUnschedReason = reason
	if r.setTempUnschedErr != nil {
		return false, r.setTempUnschedErr
	}
	value := until
	provider.TempUnschedulableUntil = &value
	return true, nil
}

func grokCredentialSnapshotMatchesProvider(provider *providercore.Record, snapshot providercore.CredentialMutationSnapshot) bool {
	return provider != nil && provider.IsGrokOAuth() && provider.IsSchedulable() &&
		providercore.GrokCredentialMutationSnapshot(provider).CredentialsJSON == snapshot.CredentialsJSON &&
		providercore.GrokCredentialProxyIDsEqual(provider.ProxyID, snapshot.ProxyID)
}

func (r *tokenRefreshProviderRepo) SetGrokOAuthRefreshErrorIfCredentialsUnchanged(
	_ context.Context,
	id int64,
	expectedCredentials map[string]any,
	expectedProxyID *int64,
	errorMsg string,
) (bool, error) {
	r.conditionalErrorCalls++
	if r.conditionalErrorErr != nil {
		return false, r.conditionalErrorErr
	}
	provider := r.providersByID[id]
	if provider == nil {
		return false, nil
	}
	if r.reauthorizeOnErrorCAS {
		r.reauthorizeOnErrorCAS = false
		provider.Credentials = map[string]any{
			"access_token":   "fresh-access",
			"refresh_token":  "fresh-refresh",
			"_token_version": int64(2),
		}
		provider.Status = providercore.StatusActive
		provider.Schedulable = true
	}
	if r.repairProxyOnErrorCAS {
		r.repairProxyOnErrorCAS = false
		proxyID := int64(902)
		provider.ProxyID = &proxyID
	}
	if provider.Status != providercore.StatusActive || provider.Platform != capability.PlatformGrok || provider.Type != capability.ProviderTypeOAuth ||
		!reflect.DeepEqual(provider.Credentials, expectedCredentials) || !reflect.DeepEqual(provider.ProxyID, expectedProxyID) {
		return false, nil
	}
	r.setErrorCalls++
	r.lastErrorMessage = errorMsg
	provider.Status = providercore.StatusError
	provider.Schedulable = false
	provider.ErrorMessage = errorMsg
	return true, nil
}

func (r *tokenRefreshProviderRepo) UpdateGrokOAuthCredentialsIfUnchanged(
	_ context.Context,
	id int64,
	expectedCredentials map[string]any,
	expectedProxyID *int64,
	credentials map[string]any,
) (bool, error) {
	r.conditionalSuccessCalls++
	if r.conditionalSuccessErr != nil {
		return false, r.conditionalSuccessErr
	}
	provider := r.providersByID[id]
	if provider != nil && r.mutateSchedulingOnSuccessCAS {
		r.mutateSchedulingOnSuccessCAS = false
		provider.Status = providercore.StatusDisabled
		provider.Schedulable = false
		resetAt := time.Now().Add(30 * time.Minute)
		provider.RateLimitResetAt = &resetAt
	}
	if provider == nil || provider.Platform != capability.PlatformGrok ||
		provider.Type != capability.ProviderTypeOAuth || !reflect.DeepEqual(provider.Credentials, expectedCredentials) ||
		!reflect.DeepEqual(provider.ProxyID, expectedProxyID) {
		return false, nil
	}
	r.updateCalls++
	r.updateCredentialsCalls++
	provider.Credentials = maps.Clone(credentials)
	r.lastProvider = provider
	if r.cancelOnUpdate != nil {
		r.cancelOnUpdate()
	}
	return true, nil
}

func (r *tokenRefreshProviderRepo) SetGrokOAuthRefreshTempUnschedulableIfCredentialsUnchanged(
	_ context.Context,
	id int64,
	expectedCredentials map[string]any,
	expectedProxyID *int64,
	until time.Time,
	reason string,
) (bool, error) {
	r.conditionalTempCalls++
	if r.conditionalTempErr != nil {
		return false, r.conditionalTempErr
	}
	provider := r.providersByID[id]
	if provider == nil {
		return false, nil
	}
	if r.reauthorizeOnTempCAS {
		r.reauthorizeOnTempCAS = false
		provider.Credentials = map[string]any{
			"access_token":   "fresh-access",
			"refresh_token":  "fresh-refresh",
			"_token_version": int64(2),
		}
		provider.Status = providercore.StatusActive
		provider.Schedulable = true
	}
	if r.repairProxyOnTempCAS {
		r.repairProxyOnTempCAS = false
		proxyID := int64(902)
		provider.ProxyID = &proxyID
	}
	if provider.Status != providercore.StatusActive || provider.Platform != capability.PlatformGrok || provider.Type != capability.ProviderTypeOAuth ||
		!reflect.DeepEqual(provider.Credentials, expectedCredentials) || !reflect.DeepEqual(provider.ProxyID, expectedProxyID) {
		return false, nil
	}
	r.setTempUnschedCalls++
	r.lastTempUnschedReason = reason
	provider.TempUnschedulableUntil = &until
	provider.TempUnschedulableReason = reason
	return true, nil
}

func (r *tokenRefreshProviderRepo) UpdateExtra(ctx context.Context, id int64, updates map[string]any) error {
	r.updateExtraCalls++
	r.lastExtraUpdates = maps.Clone(updates)
	if r.providersByID != nil {
		if acc, ok := r.providersByID[id]; ok && acc != nil {
			if acc.Extra == nil {
				acc.Extra = make(map[string]any, len(updates))
			}
			for k, v := range updates {
				acc.Extra[k] = v
			}
		}
	}
	return nil
}

type tokenCacheInvalidatorStub struct {
	calls        int
	err          error
	ctxErr       error
	lastProvider *providercore.Record
}

type tokenRefreshRuntimeBlocker struct {
	blockCalls int
	clearCalls int
}

func (b *tokenRefreshRuntimeBlocker) BlockProviderScheduling(*providercore.Record, time.Time, string) {
	b.blockCalls++
}

func (b *tokenRefreshRuntimeBlocker) ClearProviderSchedulingBlock(int64) {
	b.clearCalls++
}

func (s *tokenCacheInvalidatorStub) InvalidateToken(ctx context.Context, provider *providercore.Record) error {
	s.calls++
	s.ctxErr = ctx.Err()
	s.lastProvider = providercore.CloneRecord(provider)
	return s.err
}

type tokenRefreshSchedulerCache struct {
	setProviderCalls int
	ctxErr           error
	lastProvider     *providercore.Record
}

func (s *tokenRefreshSchedulerCache) SetProvider(ctx context.Context, provider *providercore.Record) error {
	s.setProviderCalls++
	s.ctxErr = ctx.Err()
	s.lastProvider = providercore.CloneRecord(provider)
	return nil
}

type tempUnschedCacheStub struct {
	deleteCalls int
	setCalls    int
	lastState   *providercore.TempUnschedState
}

func (s *tempUnschedCacheStub) SetTempUnsched(ctx context.Context, providerID int64, state *providercore.TempUnschedState) error {
	s.setCalls++
	s.lastState = state
	return nil
}

func (s *tempUnschedCacheStub) GetTempUnsched(ctx context.Context, providerID int64) (*providercore.TempUnschedState, error) {
	return nil, nil
}

func (s *tempUnschedCacheStub) DeleteTempUnsched(ctx context.Context, providerID int64) error {
	s.deleteCalls++
	return nil
}

type tokenRefresherStub struct {
	credentials map[string]any
	err         error
	calls       int
}

func (r *tokenRefresherStub) CanRefresh(provider *providercore.Record) bool {
	return true
}

func (r *tokenRefresherStub) NeedsRefresh(provider *providercore.Record, refreshWindowDuration time.Duration) bool {
	return true
}

func (r *tokenRefresherStub) Refresh(ctx context.Context, provider *providercore.Record) (map[string]any, error) {
	r.calls++
	if r.err != nil {
		return nil, r.err
	}
	return r.credentials, nil
}

func (r *tokenRefresherStub) CacheKey(provider *providercore.Record) string {
	return "test:stub:" + provider.Platform
}

func TestTokenRefreshService_RefreshWithRetry_InvalidatesCache(t *testing.T) {
	repo := &tokenRefreshProviderRepo{}
	invalidator := &tokenCacheInvalidatorStub{}
	cfg := &providercore.RefreshTuning{
		MaxRetries:          1,
		RetryBackoffSeconds: 0,
	}
	service := newRefreshAttemptFixture(repo, cfg, invalidator, nil, nil)
	provider := &providercore.Record{
		ID:       5,
		Platform: capability.PlatformGemini,
		Type:     capability.ProviderTypeOAuth,
	}
	refresher := &tokenRefresherStub{
		credentials: map[string]any{
			"access_token": "new-token",
		},
	}

	err := service.Attempts.Run(context.Background(), provider, refresher, refresher, time.Hour, nil)
	require.NoError(t, err)
	require.Equal(t, 1, repo.updateCalls)
	require.Equal(t, 1, repo.updateCredentialsCalls)
	require.Equal(t, 0, repo.fullUpdateCalls)
	require.Equal(t, 1, invalidator.calls)
	require.Equal(t, "new-token", provider.GetCredential("access_token"))
}

func TestTokenRefreshService_RefreshWithRetry_InvalidatorErrorIgnored(t *testing.T) {
	repo := &tokenRefreshProviderRepo{}
	invalidator := &tokenCacheInvalidatorStub{err: errors.New("invalidate failed")}
	cfg := &providercore.RefreshTuning{
		MaxRetries:          1,
		RetryBackoffSeconds: 0,
	}
	service := newRefreshAttemptFixture(repo, cfg, invalidator, nil, nil)
	provider := &providercore.Record{
		ID:       6,
		Platform: capability.PlatformGemini,
		Type:     capability.ProviderTypeOAuth,
	}
	refresher := &tokenRefresherStub{
		credentials: map[string]any{
			"access_token": "token",
		},
	}

	err := service.Attempts.Run(context.Background(), provider, refresher, refresher, time.Hour, nil)
	require.NoError(t, err)
	require.Equal(t, 1, repo.updateCalls)
	require.Equal(t, 1, invalidator.calls)
}

func TestTokenRefreshService_RefreshWithRetry_NilInvalidator(t *testing.T) {
	repo := &tokenRefreshProviderRepo{}
	cfg := &providercore.RefreshTuning{
		MaxRetries:          1,
		RetryBackoffSeconds: 0,
	}
	service := newRefreshAttemptFixture(repo, cfg, nil, nil, nil)
	provider := &providercore.Record{
		ID:       7,
		Platform: capability.PlatformGemini,
		Type:     capability.ProviderTypeOAuth,
	}
	refresher := &tokenRefresherStub{
		credentials: map[string]any{
			"access_token": "token",
		},
	}

	err := service.Attempts.Run(context.Background(), provider, refresher, refresher, time.Hour, nil)
	require.NoError(t, err)
	require.Equal(t, 1, repo.updateCalls)
}

func TestTokenRefreshService_RefreshWithRetry_QoderInvalidatesCache(t *testing.T) {
	repo := &tokenRefreshProviderRepo{}
	invalidator := &tokenCacheInvalidatorStub{}
	cfg := &providercore.RefreshTuning{
		MaxRetries:          1,
		RetryBackoffSeconds: 0,
	}
	service := newRefreshAttemptFixture(repo, cfg, invalidator, nil, nil)
	provider := &providercore.Record{
		ID:       17,
		Platform: capability.PlatformQoder,
		Type:     capability.ProviderTypeCosy,
	}
	refresher := &tokenRefresherStub{
		credentials: map[string]any{
			"security_oauth_token": "new-token",
		},
	}

	err := service.Attempts.Run(context.Background(), provider, refresher, refresher, time.Hour, nil)

	require.NoError(t, err)
	require.Equal(t, 1, invalidator.calls)
}

// TestTokenRefreshService_RefreshWithRetry_Antigravity 测试 Antigravity 平台的缓存失效
func TestTokenRefreshService_RefreshWithRetry_Antigravity(t *testing.T) {
	repo := &tokenRefreshProviderRepo{}
	invalidator := &tokenCacheInvalidatorStub{}
	cfg := &providercore.RefreshTuning{
		MaxRetries:          1,
		RetryBackoffSeconds: 0,
	}
	service := newRefreshAttemptFixture(repo, cfg, invalidator, nil, nil)
	provider := &providercore.Record{
		ID:       8,
		Platform: capability.PlatformAntigravity,
		Type:     capability.ProviderTypeOAuth,
	}
	refresher := &tokenRefresherStub{
		credentials: map[string]any{
			"access_token": "ag-token",
		},
	}

	err := service.Attempts.Run(context.Background(), provider, refresher, refresher, time.Hour, nil)
	require.NoError(t, err)
	require.Equal(t, 1, repo.updateCalls)
	require.Equal(t, 1, invalidator.calls) // Antigravity 也应触发缓存失效
}

func TestAntigravityTokenRefresher_NeedsRefresh_ForceRefreshMarker(t *testing.T) {
	refresher := &providercore.AntigravityRefreshRules{Printf: func(string, ...any) {}}
	provider := &providercore.Record{
		ID:       3675,
		Platform: capability.PlatformAntigravity,
		Type:     capability.ProviderTypeOAuth,
		Credentials: map[string]any{
			"expires_at": time.Now().Add(time.Hour).Format(time.RFC3339),
		},
		Extra: map[string]any{
			providercore.AntigravityForceTokenRefreshExtraKey: true,
		},
	}

	require.True(t, refresher.NeedsRefresh(provider, 0), "server-invalidated token must refresh even before expires_at")
}

func TestAntigravityTokenRefresher_NeedsRefresh_NormalExpiryRulesUnchanged(t *testing.T) {
	refresher := &providercore.AntigravityRefreshRules{Printf: func(string, ...any) {}}

	t.Run("normal_unexpired_without_marker_does_not_refresh", func(t *testing.T) {
		provider := &providercore.Record{
			ID:       3707,
			Platform: capability.PlatformAntigravity,
			Type:     capability.ProviderTypeOAuth,
			Credentials: map[string]any{
				"expires_at": time.Now().Add(time.Hour).Format(time.RFC3339),
			},
		}

		require.False(t, refresher.NeedsRefresh(provider, 0))
	})

	t.Run("normal_expiring_refreshes", func(t *testing.T) {
		provider := &providercore.Record{
			ID:       3708,
			Platform: capability.PlatformAntigravity,
			Type:     capability.ProviderTypeOAuth,
			Credentials: map[string]any{
				"expires_at": time.Now().Add(5 * time.Minute).Format(time.RFC3339),
			},
		}

		require.True(t, refresher.NeedsRefresh(provider, 0))
	})
}

func TestTokenRefreshService_RefreshWithRetry_AntigravityClearsForceRefreshOnSuccess(t *testing.T) {
	repo := &tokenRefreshProviderRepo{}
	cfg := &providercore.RefreshTuning{
		MaxRetries:          1,
		RetryBackoffSeconds: 0,
	}
	service := newRefreshAttemptFixture(repo, cfg, nil, nil, nil)
	until := time.Now().Add(10 * time.Minute)
	provider := &providercore.Record{
		ID:                     3709,
		Platform:               capability.PlatformAntigravity,
		Type:                   capability.ProviderTypeOAuth,
		TempUnschedulableUntil: &until,
		Extra: map[string]any{
			providercore.AntigravityForceTokenRefreshExtraKey:       true,
			providercore.AntigravityForceTokenRefreshReasonExtraKey: "401_invalid",
			"privacy_mode": providercore.AntigravityPrivacySet,
		},
	}
	refresher := &tokenRefresherStub{
		credentials: map[string]any{
			"access_token": "new-ag-token",
		},
	}

	err := service.Attempts.Run(context.Background(), provider, refresher, refresher, time.Hour, nil)
	require.NoError(t, err)
	require.Equal(t, 1, repo.updateCredentialsCalls)
	require.Equal(t, 1, repo.updateExtraCalls)
	require.Equal(t, false, repo.lastExtraUpdates[providercore.AntigravityForceTokenRefreshExtraKey])
	require.Equal(t, "", repo.lastExtraUpdates[providercore.AntigravityForceTokenRefreshReasonExtraKey])
	require.Equal(t, false, provider.Extra[providercore.AntigravityForceTokenRefreshExtraKey])
	require.Equal(t, 1, repo.clearTempCalls, "successful refresh should restore schedulability")
}

func TestTokenRefreshService_RefreshWithRetry_AntigravityForceRefreshInvalidGrantSetsError(t *testing.T) {
	repo := &tokenRefreshProviderRepo{}
	cfg := &providercore.RefreshTuning{
		MaxRetries:          3,
		RetryBackoffSeconds: 0,
	}
	service := newRefreshAttemptFixture(repo, cfg, nil, nil, nil)
	provider := &providercore.Record{
		ID:       3710,
		Platform: capability.PlatformAntigravity,
		Type:     capability.ProviderTypeOAuth,
		Extra: map[string]any{
			providercore.AntigravityForceTokenRefreshExtraKey:       true,
			providercore.AntigravityForceTokenRefreshReasonExtraKey: "401_invalid",
		},
	}
	refresher := &tokenRefresherStub{
		err: errors.New("invalid_grant: token revoked"),
	}

	err := service.Attempts.Run(context.Background(), provider, refresher, refresher, time.Hour, nil)
	require.Error(t, err)
	require.Equal(t, 1, repo.setErrorCalls)
	require.Equal(t, 0, repo.setTempUnschedCalls)
	require.Equal(t, 1, repo.updateExtraCalls)
	require.Equal(t, false, repo.lastExtraUpdates[providercore.AntigravityForceTokenRefreshExtraKey])
	require.Contains(t, repo.lastErrorMessage, "non-retryable")
}

// TestTokenRefreshService_RefreshWithRetry_NonOAuthProvider 测试非 OAuth 提供商不触发缓存失效
func TestTokenRefreshService_RefreshWithRetry_NonOAuthProvider(t *testing.T) {
	repo := &tokenRefreshProviderRepo{}
	invalidator := &tokenCacheInvalidatorStub{}
	cfg := &providercore.RefreshTuning{
		MaxRetries:          1,
		RetryBackoffSeconds: 0,
	}
	service := newRefreshAttemptFixture(repo, cfg, invalidator, nil, nil)
	provider := &providercore.Record{
		ID:       9,
		Platform: capability.PlatformGemini,
		Type:     capability.ProviderTypeAPIKey, // 非 OAuth
	}
	refresher := &tokenRefresherStub{
		credentials: map[string]any{
			"access_token": "token",
		},
	}

	err := service.Attempts.Run(context.Background(), provider, refresher, refresher, time.Hour, nil)
	require.NoError(t, err)
	require.Equal(t, 1, repo.updateCalls)
	require.Equal(t, 0, invalidator.calls) // 非 OAuth 不触发缓存失效
}

// TestTokenRefreshService_RefreshWithRetry_OtherPlatformOAuth 测试所有 OAuth 平台都触发缓存失效
func TestTokenRefreshService_RefreshWithRetry_OtherPlatformOAuth(t *testing.T) {
	repo := &tokenRefreshProviderRepo{}
	invalidator := &tokenCacheInvalidatorStub{}
	cfg := &providercore.RefreshTuning{
		MaxRetries:          1,
		RetryBackoffSeconds: 0,
	}
	service := newRefreshAttemptFixture(repo, cfg, invalidator, nil, nil)
	provider := &providercore.Record{
		ID:       10,
		Platform: capability.PlatformOpenAI, // OpenAI OAuth 提供商
		Type:     capability.ProviderTypeOAuth,
	}
	refresher := &tokenRefresherStub{
		credentials: map[string]any{
			"access_token": "token",
		},
	}

	err := service.Attempts.Run(context.Background(), provider, refresher, refresher, time.Hour, nil)
	require.NoError(t, err)
	require.Equal(t, 1, repo.updateCalls)
	require.Equal(t, 1, repo.updateCredentialsCalls)
	require.Equal(t, 1, invalidator.calls) // 所有 OAuth 提供商刷新后触发缓存失效
}

func TestTokenRefreshService_RefreshWithRetry_UsesCredentialsUpdater(t *testing.T) {
	repo := &tokenRefreshProviderRepo{}
	cfg := &providercore.RefreshTuning{
		MaxRetries:          1,
		RetryBackoffSeconds: 0,
	}
	service := newRefreshAttemptFixture(repo, cfg, nil, nil, nil)
	resetAt := time.Now().Add(30 * time.Minute)
	provider := &providercore.Record{
		ID:               17,
		Platform:         capability.PlatformOpenAI,
		Type:             capability.ProviderTypeOAuth,
		RateLimitResetAt: &resetAt,
		Credentials: map[string]any{
			"access_token": "old-token",
		},
	}
	refresher := &tokenRefresherStub{
		credentials: map[string]any{
			"access_token": "new-token",
		},
	}

	err := service.Attempts.Run(context.Background(), provider, refresher, refresher, time.Hour, nil)
	require.NoError(t, err)
	require.Equal(t, 1, repo.updateCredentialsCalls)
	require.Equal(t, 0, repo.fullUpdateCalls)
	require.NotNil(t, provider.RateLimitResetAt)
	require.WithinDuration(t, resetAt, *provider.RateLimitResetAt, time.Second)
}

// TestTokenRefreshService_RefreshWithRetry_UpdateFailed 测试更新失败的情况
func TestTokenRefreshService_RefreshWithRetry_UpdateFailed(t *testing.T) {
	repo := &tokenRefreshProviderRepo{updateErr: errors.New("update failed")}
	invalidator := &tokenCacheInvalidatorStub{}
	cfg := &providercore.RefreshTuning{
		MaxRetries:          1,
		RetryBackoffSeconds: 0,
	}
	service := newRefreshAttemptFixture(repo, cfg, invalidator, nil, nil)
	provider := &providercore.Record{
		ID:       11,
		Platform: capability.PlatformGemini,
		Type:     capability.ProviderTypeOAuth,
	}
	refresher := &tokenRefresherStub{
		credentials: map[string]any{
			"access_token": "token",
		},
	}

	err := service.Attempts.Run(context.Background(), provider, refresher, refresher, time.Hour, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to save credentials")
	require.Equal(t, 1, repo.updateCalls)
	require.Equal(t, 0, invalidator.calls) // 更新失败时不应触发缓存失效
}

// TestTokenRefreshService_RefreshWithRetry_RefreshFailed 测试可重试错误耗尽不标记 error
func TestTokenRefreshService_RefreshWithRetry_RefreshFailed(t *testing.T) {
	repo := &tokenRefreshProviderRepo{}
	invalidator := &tokenCacheInvalidatorStub{}
	cfg := &providercore.RefreshTuning{
		MaxRetries:          2,
		RetryBackoffSeconds: 0,
	}
	service := newRefreshAttemptFixture(repo, cfg, invalidator, nil, nil)
	provider := &providercore.Record{
		ID:       12,
		Platform: capability.PlatformGemini,
		Type:     capability.ProviderTypeOAuth,
	}
	refresher := &tokenRefresherStub{
		err: errors.New("refresh failed"),
	}

	err := service.Attempts.Run(context.Background(), provider, refresher, refresher, time.Hour, nil)
	require.Error(t, err)
	require.Equal(t, 0, repo.updateCalls)   // 刷新失败不应更新
	require.Equal(t, 0, invalidator.calls)  // 刷新失败不应触发缓存失效
	require.Equal(t, 0, repo.setErrorCalls) // 可重试错误耗尽不标记 error，下个周期继续重试
}

// TestTokenRefreshService_RefreshWithRetry_AntigravityRefreshFailed 测试 Antigravity 刷新失败不设置错误状态
func TestTokenRefreshService_RefreshWithRetry_AntigravityRefreshFailed(t *testing.T) {
	repo := &tokenRefreshProviderRepo{}
	invalidator := &tokenCacheInvalidatorStub{}
	cfg := &providercore.RefreshTuning{
		MaxRetries:          1,
		RetryBackoffSeconds: 0,
	}
	service := newRefreshAttemptFixture(repo, cfg, invalidator, nil, nil)
	provider := &providercore.Record{
		ID:       13,
		Platform: capability.PlatformAntigravity,
		Type:     capability.ProviderTypeOAuth,
	}
	refresher := &tokenRefresherStub{
		err: errors.New("network error"), // 可重试错误
	}

	err := service.Attempts.Run(context.Background(), provider, refresher, refresher, time.Hour, nil)
	require.Error(t, err)
	require.Equal(t, 0, repo.updateCalls)
	require.Equal(t, 0, invalidator.calls)
	require.Equal(t, 0, repo.setErrorCalls) // Antigravity 可重试错误不设置错误状态
}

// TestTokenRefreshService_RefreshWithRetry_AntigravityNonRetryableError 测试 Antigravity 不可重试错误
func TestTokenRefreshService_RefreshWithRetry_AntigravityNonRetryableError(t *testing.T) {
	repo := &tokenRefreshProviderRepo{}
	invalidator := &tokenCacheInvalidatorStub{}
	cfg := &providercore.RefreshTuning{
		MaxRetries:          3,
		RetryBackoffSeconds: 0,
	}
	service := newRefreshAttemptFixture(repo, cfg, invalidator, nil, nil)
	provider := &providercore.Record{
		ID:       14,
		Platform: capability.PlatformAntigravity,
		Type:     capability.ProviderTypeOAuth,
	}
	refresher := &tokenRefresherStub{
		err: errors.New("invalid_grant: token revoked"), // 不可重试错误
	}

	err := service.Attempts.Run(context.Background(), provider, refresher, refresher, time.Hour, nil)
	require.Error(t, err)
	require.Equal(t, 0, repo.updateCalls)
	require.Equal(t, 1, invalidator.calls)
	require.Equal(t, 1, repo.setErrorCalls) // 不可重试错误应设置错误状态
}

// TestTokenRefreshService_RefreshWithRetry_ClearsTempUnschedulable 测试刷新成功后清除临时不可调度（DB + Redis）
func TestTokenRefreshService_RefreshWithRetry_ClearsTempUnschedulable(t *testing.T) {
	repo := &tokenRefreshProviderRepo{}
	invalidator := &tokenCacheInvalidatorStub{}
	tempCache := &tempUnschedCacheStub{}
	cfg := &providercore.RefreshTuning{
		MaxRetries:          1,
		RetryBackoffSeconds: 0,
	}
	service := newRefreshAttemptFixture(repo, cfg, invalidator, nil, tempCache)
	until := time.Now().Add(10 * time.Minute)
	provider := &providercore.Record{
		ID:                     15,
		Platform:               capability.PlatformGemini,
		Type:                   capability.ProviderTypeOAuth,
		TempUnschedulableUntil: &until,
	}
	refresher := &tokenRefresherStub{
		credentials: map[string]any{
			"access_token": "new-token",
		},
	}

	err := service.Attempts.Run(context.Background(), provider, refresher, refresher, time.Hour, nil)
	require.NoError(t, err)
	require.Equal(t, 1, repo.updateCalls)
	require.Equal(t, 1, repo.clearTempCalls)   // DB 清除
	require.Equal(t, 1, tempCache.deleteCalls) // Redis 缓存也应清除
}

// TestTokenRefreshService_RefreshWithRetry_NonRetryableErrorAllPlatforms 测试所有平台不可重试错误都 SetError
func TestTokenRefreshService_RefreshWithRetry_NonRetryableErrorAllPlatforms(t *testing.T) {
	tests := []struct {
		name     string
		platform string
	}{
		{name: "gemini", platform: capability.PlatformGemini},
		{name: "anthropic", platform: capability.PlatformAnthropic},
		{name: "openai", platform: capability.PlatformOpenAI},
		{name: "antigravity", platform: capability.PlatformAntigravity},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &tokenRefreshProviderRepo{}
			invalidator := &tokenCacheInvalidatorStub{}
			cfg := &providercore.RefreshTuning{
				MaxRetries:          3,
				RetryBackoffSeconds: 0,
			}
			service := newRefreshAttemptFixture(repo, cfg, invalidator, nil, nil)
			provider := &providercore.Record{
				ID:       16,
				Platform: tt.platform,
				Type:     capability.ProviderTypeOAuth,
			}
			refresher := &tokenRefresherStub{
				err: errors.New("invalid_grant: token revoked"),
			}

			err := service.Attempts.Run(context.Background(), provider, refresher, refresher, time.Hour, nil)
			require.Error(t, err)
			require.Equal(t, 1, repo.setErrorCalls) // 所有平台不可重试错误都应 SetError
		})
	}
}

func TestTokenRefreshService_RefreshWithRetry_NoRefreshTokenDoesNotTempUnschedule(t *testing.T) {
	repo := &tokenRefreshProviderRepo{}
	cfg := &providercore.RefreshTuning{
		MaxRetries:          2,
		RetryBackoffSeconds: 0,
	}
	service := newRefreshAttemptFixture(repo, cfg, nil, nil, nil)
	provider := &providercore.Record{
		ID:       18,
		Platform: capability.PlatformOpenAI,
		Type:     capability.ProviderTypeOAuth,
	}
	refresher := &tokenRefresherStub{
		err: errors.New("no refresh token available"),
	}

	err := service.Attempts.Run(context.Background(), provider, refresher, refresher, time.Hour, nil)
	require.Error(t, err)
	require.Equal(t, 0, repo.updateCalls)
	require.Equal(t, 0, repo.setTempUnschedCalls, "missing refresh token should not mark the provider temp unschedulable")
	require.Equal(t, 1, repo.setErrorCalls, "missing refresh token should be treated as a non-retryable credential state")
}

// ========== Path A (refreshAPI) 测试用例 ==========

// mockTokenCacheForRefreshAPI 用于 Path A 测试的 GeminiTokenCache mock
type mockTokenCacheForRefreshAPI struct {
	lockResult   bool
	lockErr      error
	releaseCalls int
	deleteCalls  int
	deleteCtxErr error
}

func (m *mockTokenCacheForRefreshAPI) GetAccessToken(_ context.Context, _ string) (string, error) {
	return "", errors.New("not cached")
}

func (m *mockTokenCacheForRefreshAPI) SetAccessToken(_ context.Context, _ string, _ string, _ time.Duration) error {
	return nil
}

func (m *mockTokenCacheForRefreshAPI) DeleteAccessToken(ctx context.Context, _ string) error {
	m.deleteCalls++
	m.deleteCtxErr = ctx.Err()
	return nil
}

func (m *mockTokenCacheForRefreshAPI) AcquireRefreshLock(_ context.Context, _ string, _ time.Duration) (bool, error) {
	return m.lockResult, m.lockErr
}

func (m *mockTokenCacheForRefreshAPI) ReleaseRefreshLock(_ context.Context, _ string) error {
	m.releaseCalls++
	return nil
}

// buildPathAService 构建注入了 refreshAPI 的 service（Path A 测试辅助）
func buildPathAService(repo *tokenRefreshProviderRepo, cache providercore.AccessTokenCache, invalidator providercore.TokenCacheInvalidator) (*refreshAttemptFixture, *tokenRefresherStub) {
	for _, provider := range repo.providersByID {
		if provider != nil && provider.Status == "" {
			provider.Status = providercore.StatusActive
		}
	}
	cfg := &providercore.RefreshTuning{
		MaxRetries:          1,
		RetryBackoffSeconds: 0,
	}
	service := newRefreshAttemptFixture(repo, cfg, invalidator, nil, nil)
	refreshAPI := newRefreshAPI(repo, cache)
	service.Attempts.API = refreshAPI

	refresher := &tokenRefresherStub{
		credentials: map[string]any{
			"access_token": "refreshed-token",
		},
	}
	return service, refresher
}

// TestPathA_Success 统一 API 路径正常成功：刷新 + DB 更新 + postRefreshActions
func TestPathA_Success(t *testing.T) {
	provider := &providercore.Record{
		ID:       100,
		Platform: capability.PlatformGemini,
		Type:     capability.ProviderTypeOAuth,
		Status:   providercore.StatusActive,
	}
	repo := &tokenRefreshProviderRepo{}
	repo.providersByID = map[int64]*providercore.Record{provider.ID: provider}
	invalidator := &tokenCacheInvalidatorStub{}
	cache := &mockTokenCacheForRefreshAPI{lockResult: true}

	service, refresher := buildPathAService(repo, cache, invalidator)

	err := service.Attempts.Run(context.Background(), provider, refresher, refresher, time.Hour, nil)
	require.NoError(t, err)
	require.Equal(t, 1, repo.updateCalls)   // DB 更新被调用
	require.Equal(t, 1, invalidator.calls)  // 缓存失效被调用
	require.Equal(t, 1, cache.releaseCalls) // 锁被释放
}

func TestPathA_GrokSuccessPersistenceFailureContainsProviderWithoutRetryOrMutation(t *testing.T) {
	provider := &providercore.Record{
		ID:       110,
		Platform: capability.PlatformGrok,
		Type:     capability.ProviderTypeOAuth,
		Status:   providercore.StatusActive,
		Credentials: map[string]any{
			"access_token":  "attempted-access",
			"refresh_token": "attempted-refresh",
		},
	}
	repo := &tokenRefreshProviderRepo{
		conditionalSuccessErr: errors.New("database unavailable after provider success"),
	}
	repo.providersByID = map[int64]*providercore.Record{provider.ID: provider}
	cfg := &providercore.RefreshTuning{
		MaxRetries:          3,
		RetryBackoffSeconds: 0,
	}
	svc := newRefreshAttemptFixture(repo, cfg, nil, nil, nil)
	svc.Attempts.API = newRefreshAPI(repo, nil)
	refresher := &tokenRefresherStub{credentials: map[string]any{
		"access_token":  "provider-access",
		"refresh_token": "provider-refresh",
	}}

	err := svc.Attempts.Run(context.Background(), provider, refresher, refresher, time.Hour, nil)

	var containmentErr *providercore.ProviderCycleContainmentRefreshError
	require.ErrorAs(t, err, &containmentErr)
	require.Equal(t, 1, refresher.calls, "a provider-issued rotated token must never be retried after persistence fails")
	require.Equal(t, 1, repo.conditionalSuccessCalls)
	require.Zero(t, repo.conditionalErrorCalls)
	require.Zero(t, repo.conditionalTempCalls)
	require.Equal(t, providercore.StatusActive, provider.Status)
	require.Equal(t, "attempted-refresh", provider.GetGrokRefreshToken())
}

func TestPathA_GrokSuccessPublishesDurableSchedulingState(t *testing.T) {
	provider := &providercore.Record{
		ID:          111,
		Platform:    capability.PlatformGrok,
		Type:        capability.ProviderTypeOAuth,
		Status:      providercore.StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"access_token":  "attempted-access",
			"refresh_token": "attempted-refresh",
		},
	}
	repo := &tokenRefreshProviderRepo{
		snapshotReads:                true,
		mutateSchedulingOnSuccessCAS: true,
	}
	repo.providersByID = map[int64]*providercore.Record{provider.ID: provider}
	scheduler := &tokenRefreshSchedulerCache{}
	cfg := &providercore.RefreshTuning{MaxRetries: 1}
	svc := newRefreshAttemptFixture(repo, cfg, nil, scheduler, nil)
	svc.Attempts.API = newRefreshAPI(repo, nil)
	refresher := &tokenRefresherStub{credentials: map[string]any{
		"access_token":  "provider-access",
		"refresh_token": "provider-refresh",
	}}

	err := svc.Attempts.Run(context.Background(), provider, refresher, refresher, time.Hour, nil)

	require.NoError(t, err)
	require.Equal(t, providercore.StatusDisabled, repo.providersByID[provider.ID].Status)
	require.False(t, repo.providersByID[provider.ID].Schedulable)
	require.NotNil(t, repo.providersByID[provider.ID].RateLimitResetAt)
	require.Equal(t, 1, scheduler.setProviderCalls)
	require.NotNil(t, scheduler.lastProvider)
	require.Equal(t, providercore.StatusDisabled, scheduler.lastProvider.Status)
	require.False(t, scheduler.lastProvider.Schedulable)
	require.NotNil(t, scheduler.lastProvider.RateLimitResetAt,
		"post-refresh cache publication must preserve the durable concurrent exclusion state")
}

func TestPathA_GrokCancelAfterSuccessCASUsesDetachedDurableStateAndInvalidatesCache(t *testing.T) {
	provider := &providercore.Record{
		ID:          112,
		Platform:    capability.PlatformGrok,
		Type:        capability.ProviderTypeOAuth,
		Status:      providercore.StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"access_token":  "attempted-access",
			"refresh_token": "attempted-refresh",
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	repo := &tokenRefreshProviderRepo{
		cancelOnUpdate:               cancel,
		snapshotReads:                true,
		respectReadContext:           true,
		mutateSchedulingOnSuccessCAS: true,
	}
	repo.providersByID = map[int64]*providercore.Record{provider.ID: provider}
	invalidator := &tokenCacheInvalidatorStub{}
	scheduler := &tokenRefreshSchedulerCache{}
	cache := &mockTokenCacheForRefreshAPI{lockResult: true}
	cfg := &providercore.RefreshTuning{MaxRetries: 1}
	svc := newRefreshAttemptFixture(repo, cfg, invalidator, scheduler, nil)
	svc.Attempts.API = newRefreshAPI(repo, cache)
	refresher := &tokenRefresherStub{credentials: map[string]any{
		"access_token":  "provider-access",
		"refresh_token": "provider-refresh",
	}}

	err := svc.Attempts.Run(ctx, provider, refresher, refresher, time.Hour, nil)

	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, repo.conditionalSuccessCalls)
	require.Equal(t, "provider-refresh", repo.providersByID[provider.ID].GetGrokRefreshToken())
	require.Equal(t, 1, cache.deleteCalls)
	require.NoError(t, cache.deleteCtxErr)
	require.Equal(t, 1, invalidator.calls, "the pre-rotation access-token cache must be invalidated after committed CAS")
	require.NoError(t, invalidator.ctxErr)
	require.NotNil(t, invalidator.lastProvider)
	require.Equal(t, "provider-refresh", invalidator.lastProvider.GetGrokRefreshToken())
	require.Equal(t, providercore.StatusDisabled, invalidator.lastProvider.Status)
	require.Equal(t, 1, scheduler.setProviderCalls)
	require.NoError(t, scheduler.ctxErr)
	require.NotNil(t, scheduler.lastProvider)
	require.Equal(t, providercore.StatusDisabled, scheduler.lastProvider.Status)
	require.False(t, scheduler.lastProvider.Schedulable)
	require.NotNil(t, scheduler.lastProvider.RateLimitResetAt)
}

func TestTokenRefreshService_PersistedSuccessCrossingAttemptDeadlineStaysSuccessful(t *testing.T) {
	provider := &providercore.Record{
		ID:          113,
		Platform:    capability.PlatformGrok,
		Type:        capability.ProviderTypeOAuth,
		Status:      providercore.StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"access_token":  "attempted-access",
			"refresh_token": "attempted-refresh",
		},
	}
	repo := &tokenRefreshProviderRepo{
		snapshotReads:    true,
		durableReadDelay: 30 * time.Millisecond,
	}
	repo.providersByID = map[int64]*providercore.Record{provider.ID: provider}
	scheduler := &tokenRefreshSchedulerCache{}
	svc := newRefreshAttemptFixture(repo, &providercore.RefreshTuning{MaxRetries: 1, ProviderFailureThreshold: 1}, nil, scheduler, nil)
	svc.Attempts.API = newRefreshAPI(repo, nil)
	svc.Attempts.AttemptTimeout = 10 * time.Millisecond
	refresher := &tokenRefresherStub{credentials: map[string]any{
		"access_token":  "provider-access",
		"refresh_token": "provider-refresh",
	}}
	state := providercore.NewRefreshProviderState(providercore.NewRefreshRateGate(10000), providercore.NewRefreshConcurrencyGate(1), 1, IsNonRetryableRefreshError)

	err := svc.Attempts.Run(context.Background(), provider, refresher, refresher, time.Hour, state)
	state.RecordResult(err)

	require.NoError(t, err)
	require.Equal(t, 1, refresher.calls, "durably persisted success must not retry after only the internal attempt deadline elapsed")
	require.Equal(t, 1, repo.conditionalSuccessCalls)
	require.Zero(t, repo.conditionalTempCalls)
	require.Zero(t, repo.setTempUnschedCalls)
	require.False(t, state.IsTripped(), "a durable success must not count toward the provider breaker")
	require.Equal(t, "provider-refresh", repo.providersByID[provider.ID].GetGrokRefreshToken())
	require.Equal(t, 1, scheduler.setProviderCalls)
}

func TestPathA_ParentCancellationAfterPersistStillSynchronizesCacheState(t *testing.T) {
	provider := &providercore.Record{
		ID:       109,
		Platform: capability.PlatformGemini,
		Type:     capability.ProviderTypeOAuth,
		Status:   providercore.StatusActive,
	}
	ctx, cancel := context.WithCancel(context.Background())
	repo := &tokenRefreshProviderRepo{cancelOnUpdate: cancel}
	repo.providersByID = map[int64]*providercore.Record{provider.ID: provider}
	invalidator := &tokenCacheInvalidatorStub{}
	scheduler := &tokenRefreshSchedulerCache{}
	cache := &mockTokenCacheForRefreshAPI{lockResult: true}
	service, refresher := buildPathAService(repo, cache, invalidator)
	service.Post.SyncProvider = scheduler.SetProvider

	err := service.Attempts.Run(ctx, provider, refresher, refresher, time.Hour, nil)

	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, repo.updateCredentialsCalls, "credentials were durably persisted before cancellation")
	require.Equal(t, 1, invalidator.calls)
	require.NoError(t, invalidator.ctxErr, "post-persist invalidation must use bounded cleanup context")
	require.Equal(t, 1, scheduler.setProviderCalls)
	require.NoError(t, scheduler.ctxErr, "scheduler sync must use bounded cleanup context")
}

// TestPathA_LockHeld 锁被其他 worker 持有 → 返回 providercore.ErrRefreshSkipped
func TestPathA_LockHeld(t *testing.T) {
	provider := &providercore.Record{
		ID:       101,
		Platform: capability.PlatformGemini,
		Type:     capability.ProviderTypeOAuth,
		Status:   providercore.StatusActive,
	}
	repo := &tokenRefreshProviderRepo{}
	invalidator := &tokenCacheInvalidatorStub{}
	cache := &mockTokenCacheForRefreshAPI{lockResult: false} // 锁获取失败（被占）

	service, refresher := buildPathAService(repo, cache, invalidator)

	err := service.Attempts.Run(context.Background(), provider, refresher, refresher, time.Hour, nil)
	require.ErrorIs(t, err, providercore.ErrRefreshSkipped)
	require.Equal(t, 0, repo.updateCalls)  // 不应更新 DB
	require.Equal(t, 0, invalidator.calls) // 不应触发缓存失效
}

// TestPathA_AlreadyRefreshed 二次检查发现已被其他路径刷新 → 返回 providercore.ErrRefreshSkipped
func TestPathA_AlreadyRefreshed(t *testing.T) {
	// NeedsRefresh 返回 false → RefreshIfNeeded 返回 {Refreshed: false}
	provider := &providercore.Record{
		ID:       102,
		Platform: capability.PlatformGemini,
		Type:     capability.ProviderTypeOAuth,
		Status:   providercore.StatusActive,
	}
	repo := &tokenRefreshProviderRepo{}
	repo.providersByID = map[int64]*providercore.Record{provider.ID: provider}
	invalidator := &tokenCacheInvalidatorStub{}
	cache := &mockTokenCacheForRefreshAPI{lockResult: true}

	service, _ := buildPathAService(repo, cache, invalidator)

	// 使用一个 NeedsRefresh 返回 false 的 stub
	noRefreshNeeded := &tokenRefresherStub{
		credentials: map[string]any{"access_token": "token"},
	}
	// 使用单独的 stub 实现 NeedsRefresh。
	alwaysFreshStub := &alwaysFreshRefresherStub{}

	err := service.Attempts.Run(context.Background(), provider, noRefreshNeeded, alwaysFreshStub, time.Hour, nil)
	require.ErrorIs(t, err, providercore.ErrRefreshSkipped)
	require.Equal(t, 0, repo.updateCalls)
	require.Equal(t, 0, invalidator.calls)
}

// alwaysFreshRefresherStub 二次检查时认为不需要刷新（模拟已被其他路径刷新）
type alwaysFreshRefresherStub struct{}

func (r *alwaysFreshRefresherStub) CanRefresh(_ *providercore.Record) bool { return true }
func (r *alwaysFreshRefresherStub) NeedsRefresh(_ *providercore.Record, _ time.Duration) bool {
	return false
}

func (r *alwaysFreshRefresherStub) Refresh(_ context.Context, _ *providercore.Record) (map[string]any, error) {
	return nil, errors.New("should not be called")
}

func (r *alwaysFreshRefresherStub) CacheKey(provider *providercore.Record) string {
	return "test:fresh:" + provider.Platform
}

// TestPathA_NonRetryableError 统一 API 路径返回不可重试错误 → SetError
func TestPathA_NonRetryableError(t *testing.T) {
	provider := &providercore.Record{
		ID:       103,
		Platform: capability.PlatformGemini,
		Type:     capability.ProviderTypeOAuth,
		Status:   providercore.StatusActive,
	}
	repo := &tokenRefreshProviderRepo{}
	repo.providersByID = map[int64]*providercore.Record{provider.ID: provider}
	invalidator := &tokenCacheInvalidatorStub{}
	cache := &mockTokenCacheForRefreshAPI{lockResult: true}

	service, _ := buildPathAService(repo, cache, invalidator)

	refresher := &tokenRefresherStub{
		err: errors.New("invalid_grant: token revoked"),
	}

	err := service.Attempts.Run(context.Background(), provider, refresher, refresher, time.Hour, nil)
	require.Error(t, err)
	require.Equal(t, 1, repo.setErrorCalls) // 应标记 error 状态
	require.Equal(t, 0, repo.updateCalls)   // 不应更新 credentials
	require.Equal(t, 1, invalidator.calls)  // 永久凭证失败后必须失效旧 token 缓存
}

// TestPathA_RetryableErrorExhausted 统一 API 路径可重试错误耗尽 → 不标记 error
func TestPathA_RetryableErrorExhausted(t *testing.T) {
	provider := &providercore.Record{
		ID:       104,
		Platform: capability.PlatformGemini,
		Type:     capability.ProviderTypeOAuth,
		Status:   providercore.StatusActive,
	}
	repo := &tokenRefreshProviderRepo{}
	repo.providersByID = map[int64]*providercore.Record{provider.ID: provider}
	invalidator := &tokenCacheInvalidatorStub{}
	cache := &mockTokenCacheForRefreshAPI{lockResult: true}

	cfg := &providercore.RefreshTuning{
		MaxRetries:          2,
		RetryBackoffSeconds: 0,
	}
	service := newRefreshAttemptFixture(repo, cfg, invalidator, nil, nil)
	refreshAPI := newRefreshAPI(repo, cache)
	service.Attempts.API = refreshAPI

	refresher := &tokenRefresherStub{
		err: errors.New("network timeout"),
	}

	err := service.Attempts.Run(context.Background(), provider, refresher, refresher, time.Hour, nil)
	require.Error(t, err)
	require.Equal(t, 0, repo.setErrorCalls) // 可重试错误不标记 error
	require.Equal(t, 0, repo.updateCalls)   // 刷新失败不应更新
	require.Equal(t, 0, invalidator.calls)  // 不应触发缓存失效
}

func TestPathA_GrokPermanentFailureCASLetsConcurrentProviderRepairWin(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*tokenRefreshProviderRepo)
		assert    func(*testing.T, *providercore.Record)
	}{
		{
			name: "credential reauthorization",
			configure: func(repo *tokenRefreshProviderRepo) {
				repo.reauthorizeOnErrorCAS = true
			},
			assert: func(t *testing.T, provider *providercore.Record) {
				require.Equal(t, "fresh-refresh", provider.GetGrokRefreshToken())
			},
		},
		{
			name: "proxy repair",
			configure: func(repo *tokenRefreshProviderRepo) {
				repo.repairProxyOnErrorCAS = true
			},
			assert: func(t *testing.T, provider *providercore.Record) {
				require.NotNil(t, provider.ProxyID)
				require.Equal(t, int64(902), *provider.ProxyID)
				require.Equal(t, "attempted-refresh", provider.GetGrokRefreshToken(),
					"proxy-only repair must prove the proxy fingerprint independently of credentials")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proxyID := int64(901)
			provider := &providercore.Record{
				ID:          120,
				Platform:    capability.PlatformGrok,
				Type:        capability.ProviderTypeOAuth,
				Status:      providercore.StatusActive,
				Schedulable: true,
				ProxyID:     &proxyID,
				Credentials: map[string]any{
					"access_token":   "attempted-access",
					"refresh_token":  "attempted-refresh",
					"_token_version": int64(1),
				},
			}
			repo := &tokenRefreshProviderRepo{}
			repo.providersByID = map[int64]*providercore.Record{provider.ID: provider}
			tt.configure(repo)
			invalidator := &tokenCacheInvalidatorStub{}
			cache := &mockTokenCacheForRefreshAPI{lockResult: true}
			service, _ := buildPathAService(repo, cache, invalidator)
			blocker := &tokenRefreshRuntimeBlocker{}
			service.Attempts.PrepareFailure = func(v *providercore.Record) func(time.Time, string) {
				return providercore.PrepareRefreshFailureNotice(blocker, v)
			}
			refresher := &tokenRefresherStub{err: errors.New("invalid_grant: revoked")}

			err := service.Attempts.Run(context.Background(), provider, refresher, refresher, time.Hour, nil)

			require.ErrorIs(t, err, providercore.ErrRefreshSkipped)
			require.Equal(t, 1, repo.conditionalErrorCalls)
			require.Zero(t, repo.setErrorCalls)
			require.Zero(t, blocker.blockCalls)
			require.Zero(t, invalidator.calls, "a stale permanent failure must not invalidate newly repaired credentials")
			require.Equal(t, providercore.StatusActive, provider.Status)
			require.True(t, provider.Schedulable)
			tt.assert(t, provider)
		})
	}
}

func TestPathA_GrokTransientFailureCASLetsConcurrentProviderRepairWin(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*tokenRefreshProviderRepo)
		assert    func(*testing.T, *providercore.Record)
	}{
		{
			name: "credential reauthorization",
			configure: func(repo *tokenRefreshProviderRepo) {
				repo.reauthorizeOnTempCAS = true
			},
			assert: func(t *testing.T, provider *providercore.Record) {
				require.Equal(t, "fresh-refresh", provider.GetGrokRefreshToken())
			},
		},
		{
			name: "proxy repair",
			configure: func(repo *tokenRefreshProviderRepo) {
				repo.repairProxyOnTempCAS = true
			},
			assert: func(t *testing.T, provider *providercore.Record) {
				require.NotNil(t, provider.ProxyID)
				require.Equal(t, int64(902), *provider.ProxyID)
				require.Equal(t, "attempted-refresh", provider.GetGrokRefreshToken(),
					"proxy-only repair must prove the proxy fingerprint independently of credentials")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proxyID := int64(901)
			provider := &providercore.Record{
				ID:          121,
				Platform:    capability.PlatformGrok,
				Type:        capability.ProviderTypeOAuth,
				Status:      providercore.StatusActive,
				Schedulable: true,
				ProxyID:     &proxyID,
				Credentials: map[string]any{
					"access_token":   "attempted-access",
					"refresh_token":  "attempted-refresh",
					"_token_version": int64(1),
				},
			}
			repo := &tokenRefreshProviderRepo{}
			repo.providersByID = map[int64]*providercore.Record{provider.ID: provider}
			tt.configure(repo)
			invalidator := &tokenCacheInvalidatorStub{}
			cache := &mockTokenCacheForRefreshAPI{lockResult: true}
			service, _ := buildPathAService(repo, cache, invalidator)
			blocker := &tokenRefreshRuntimeBlocker{}
			service.Attempts.PrepareFailure = func(v *providercore.Record) func(time.Time, string) {
				return providercore.PrepareRefreshFailureNotice(blocker, v)
			}
			refresher := &tokenRefresherStub{err: errors.New("temporary provider timeout")}

			err := service.Attempts.Run(context.Background(), provider, refresher, refresher, time.Hour, nil)

			require.ErrorIs(t, err, providercore.ErrRefreshSkipped)
			require.Equal(t, 1, repo.conditionalTempCalls)
			require.Zero(t, repo.setTempUnschedCalls)
			require.Zero(t, blocker.blockCalls)
			require.Equal(t, providercore.StatusActive, provider.Status)
			require.True(t, provider.Schedulable)
			require.Nil(t, provider.TempUnschedulableUntil)
			tt.assert(t, provider)
		})
	}
}

func TestTokenRefreshService_GrokMissingConditionalMutationContractContainsProviderCycle(t *testing.T) {
	tests := []struct {
		name       string
		refreshErr error
	}{
		{name: "permanent failure", refreshErr: errors.New("invalid_grant: revoked")},
		{name: "transient failure", refreshErr: errors.New("temporary provider timeout")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newRefreshAttemptFixture(&tokenRefreshProviderRepo{}, &providercore.RefreshTuning{MaxRetries: 1}, nil, nil, nil)
			svc.Attempts.GrokMutation = nil
			provider := &providercore.Record{
				ID:          122,
				Platform:    capability.PlatformGrok,
				Type:        capability.ProviderTypeOAuth,
				Status:      providercore.StatusActive,
				Schedulable: true,
				Credentials: map[string]any{"refresh_token": "attempted"},
			}
			refresher := &tokenRefresherStub{err: tt.refreshErr}

			err := svc.Attempts.Run(context.Background(), provider, refresher, nil, time.Hour, nil)

			var providerErr *providercore.ProviderConfigurationRefreshError
			require.ErrorAs(t, err, &providerErr)
			state := providercore.NewRefreshProviderState(nil, nil, svc.Attempts.Tuning.FailureThreshold(), IsNonRetryableRefreshError)
			state.RecordResult(err)
			require.True(t, state.IsTripped(), "a missing safety contract must stop the provider cycle")
			require.Equal(t, providercore.StatusActive, provider.Status)
			require.True(t, provider.Schedulable)
		})
	}
}

func TestTokenRefreshService_GrokConditionalMutationErrorsContainProviderCycle(t *testing.T) {
	tests := []struct {
		name             string
		upstreamErr      error
		configureRepo    func(*tokenRefreshProviderRepo, error)
		expectedCASCalls func(*tokenRefreshProviderRepo) int
	}{
		{
			name:        "permanent failure",
			upstreamErr: errors.New("invalid_grant: revoked"),
			configureRepo: func(repo *tokenRefreshProviderRepo, casErr error) {
				repo.conditionalErrorErr = casErr
			},
			expectedCASCalls: func(repo *tokenRefreshProviderRepo) int { return repo.conditionalErrorCalls },
		},
		{
			name:        "transient failure",
			upstreamErr: errors.New("temporary provider timeout"),
			configureRepo: func(repo *tokenRefreshProviderRepo, casErr error) {
				repo.conditionalTempErr = casErr
			},
			expectedCASCalls: func(repo *tokenRefreshProviderRepo) int { return repo.conditionalTempCalls },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &providercore.Record{
				ID:          123,
				Platform:    capability.PlatformGrok,
				Type:        capability.ProviderTypeOAuth,
				Status:      providercore.StatusActive,
				Schedulable: true,
				Credentials: map[string]any{"refresh_token": "attempted"},
			}
			casErr := errors.New("conditional provider mutation unavailable")
			repo := &tokenRefreshProviderRepo{}
			repo.providersByID = map[int64]*providercore.Record{provider.ID: provider}
			tt.configureRepo(repo, casErr)
			invalidator := &tokenCacheInvalidatorStub{}
			blocker := &tokenRefreshRuntimeBlocker{}
			svc := newRefreshAttemptFixture(repo, &providercore.RefreshTuning{MaxRetries: 1}, invalidator, nil, nil)
			svc.Attempts.PrepareFailure = func(v *providercore.Record) func(time.Time, string) {
				return providercore.PrepareRefreshFailureNotice(blocker, v)
			}
			refresher := &tokenRefresherStub{err: tt.upstreamErr}

			err := svc.Attempts.Run(context.Background(), provider, refresher, nil, time.Hour, nil)

			var containmentErr *providercore.ProviderCycleContainmentRefreshError
			require.ErrorAs(t, err, &containmentErr)
			require.ErrorIs(t, err, casErr)
			require.NotErrorIs(t, err, tt.upstreamErr, "a CAS execution failure must replace the stale upstream classification")
			var permanentErr *providercore.ProviderPermanentRefreshError
			require.False(t, errors.As(err, &permanentErr))
			require.Equal(t, 1, tt.expectedCASCalls(repo))

			state := providercore.NewRefreshProviderState(nil, nil, svc.Attempts.Tuning.FailureThreshold(), IsNonRetryableRefreshError)
			state.RecordResult(err)
			require.True(t, state.IsTripped(), "an unsafe mutation result must stop the provider cycle immediately")
			require.Zero(t, repo.setErrorCalls)
			require.Zero(t, repo.setTempUnschedCalls)
			require.Zero(t, blocker.blockCalls)
			require.Zero(t, invalidator.calls)
			require.Equal(t, providercore.StatusActive, provider.Status)
			require.True(t, provider.Schedulable)
		})
	}
}

// TestPathA_DBUpdateFailed 统一 API 路径 DB 更新失败 → 返回 error，不执行 postRefreshActions
func TestPathA_DBUpdateFailed(t *testing.T) {
	provider := &providercore.Record{
		ID:       105,
		Platform: capability.PlatformGemini,
		Type:     capability.ProviderTypeOAuth,
		Status:   providercore.StatusActive,
	}
	repo := &tokenRefreshProviderRepo{updateErr: errors.New("db connection lost")}
	repo.providersByID = map[int64]*providercore.Record{provider.ID: provider}
	invalidator := &tokenCacheInvalidatorStub{}
	cache := &mockTokenCacheForRefreshAPI{lockResult: true}

	service, refresher := buildPathAService(repo, cache, invalidator)

	err := service.Attempts.Run(context.Background(), provider, refresher, refresher, time.Hour, nil)
	require.Error(t, err)
	require.ErrorIs(t, err, providercore.ErrRefreshCredentialPersist)
	require.Equal(t, 1, repo.updateCalls)  // DB 更新被尝试
	require.Equal(t, 0, invalidator.calls) // DB 失败时不应触发缓存失效
}
