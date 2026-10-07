package provider_test

import (
	"context"
	"reflect"
	"time"

	forwardcore "github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/pkg/querycache"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

type tokenRefreshProviderRepo struct {
	credentialReadStore
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
	lastProvider                 *gatewayprovider.ExecutionProvider
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

func (r *tokenRefreshProviderRepo) Update(ctx context.Context, provider *gatewayprovider.ExecutionProvider) error {
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
	cloned := querycache.ShallowMap(credentials)
	if r.providersByID != nil {
		if acc, ok := r.providersByID[id]; ok && acc != nil {
			acc.Record.Credentials = cloned
			r.lastProvider = acc
			if r.cancelOnUpdate != nil {
				r.cancelOnUpdate()
			}
			return nil
		}
	}
	r.lastProvider = &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, ID: id, Credentials: cloned}}
	if r.cancelOnUpdate != nil {
		r.cancelOnUpdate()
	}
	return nil
}

func (r *tokenRefreshProviderRepo) GetByID(ctx context.Context, id int64) (*gatewayprovider.ExecutionProvider, error) {
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
	provider, err := r.credentialReadStore.GetByID(ctx, id)
	if err != nil || !r.snapshotReads {
		return provider, err
	}
	return grokCredentialStoredSnapshot(provider), nil
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
		(errorMsg == string(forwardcore.GrokCredentialReasonProxyInvalid) && provider.Record.Proxy != nil) {
		return false, nil
	}
	r.setErrorCalls++
	r.lastErrorMessage = errorMsg
	if r.setErrorErr != nil {
		return false, r.setErrorErr
	}
	provider.Record.Status = providercore.StatusError
	provider.Record.Schedulable = false
	provider.Record.ErrorMessage = errorMsg
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
	provider.Record.TempUnschedulableUntil = &value
	return true, nil
}

func grokCredentialSnapshotMatchesProvider(provider *gatewayprovider.ExecutionProvider, snapshot providercore.CredentialMutationSnapshot) bool {
	return provider != nil && provider.View().IsGrokOAuth() && provider.View().IsSchedulable() &&
		grokCredentialMutationSnapshot(provider).CredentialsJSON == snapshot.CredentialsJSON &&
		providercore.GrokCredentialProxyIDsEqual(provider.Record.ProxyID, snapshot.ProxyID)
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
		provider.Record.Credentials = map[string]any{
			"access_token":   "fresh-access",
			"refresh_token":  "fresh-refresh",
			"_token_version": int64(2),
		}
		provider.Record.Status = billing.StatusActive
		provider.Record.Schedulable = true
	}
	if r.repairProxyOnErrorCAS {
		r.repairProxyOnErrorCAS = false
		proxyID := int64(902)
		provider.Record.ProxyID = &proxyID
	}
	if provider.Record.Status != billing.StatusActive || provider.Record.Platform != capability.PlatformGrok || provider.Record.Type != capability.ProviderTypeOAuth ||
		!reflect.DeepEqual(provider.Record.Credentials, expectedCredentials) || !reflect.DeepEqual(provider.Record.ProxyID, expectedProxyID) {
		return false, nil
	}
	r.setErrorCalls++
	r.lastErrorMessage = errorMsg
	provider.Record.Status = providercore.StatusError
	provider.Record.Schedulable = false
	provider.Record.ErrorMessage = errorMsg
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
		provider.Record.Status = billing.StatusDisabled
		provider.Record.Schedulable = false
		resetAt := time.Now().Add(30 * time.Minute)
		provider.Record.RateLimitResetAt = &resetAt
	}
	if provider == nil || provider.Record.Platform != capability.PlatformGrok ||
		provider.Record.Type != capability.ProviderTypeOAuth || !reflect.DeepEqual(provider.Record.Credentials, expectedCredentials) ||
		!reflect.DeepEqual(provider.Record.ProxyID, expectedProxyID) {
		return false, nil
	}
	r.updateCalls++
	r.updateCredentialsCalls++
	provider.Record.Credentials = querycache.ShallowMap(credentials)
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
		provider.Record.Credentials = map[string]any{
			"access_token":   "fresh-access",
			"refresh_token":  "fresh-refresh",
			"_token_version": int64(2),
		}
		provider.Record.Status = billing.StatusActive
		provider.Record.Schedulable = true
	}
	if r.repairProxyOnTempCAS {
		r.repairProxyOnTempCAS = false
		proxyID := int64(902)
		provider.Record.ProxyID = &proxyID
	}
	if provider.Record.Status != billing.StatusActive || provider.Record.Platform != capability.PlatformGrok || provider.Record.Type != capability.ProviderTypeOAuth ||
		!reflect.DeepEqual(provider.Record.Credentials, expectedCredentials) || !reflect.DeepEqual(provider.Record.ProxyID, expectedProxyID) {
		return false, nil
	}
	r.setTempUnschedCalls++
	r.lastTempUnschedReason = reason
	provider.Record.TempUnschedulableUntil = &until
	provider.Record.TempUnschedulableReason = reason
	return true, nil
}

func (r *tokenRefreshProviderRepo) UpdateExtra(ctx context.Context, id int64, updates map[string]any) error {
	r.updateExtraCalls++
	r.lastExtraUpdates = querycache.ShallowMap(updates)
	if r.providersByID != nil {
		if acc, ok := r.providersByID[id]; ok && acc != nil {
			if acc.Record.Extra == nil {
				acc.Record.Extra = make(map[string]any, len(updates))
			}
			for k, v := range updates {
				acc.Record.Extra[k] = v
			}
		}
	}
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

// ========== Path A (refreshAPI) 测试用例 ==========

// grokCredentialStoredSnapshot 只为旧消费者测试生成独立夹具，运行实现归 provider。
func grokCredentialStoredSnapshot(value *gatewayprovider.ExecutionProvider) *gatewayprovider.ExecutionProvider {
	copy := gatewayprovider.NewExecutionProvider(gatewayprovider.ExecutionRecord(value))
	if copy != nil && copy.Record.Credentials == nil {
		copy.Record.Credentials = map[string]any{}
	}
	return copy
}
