package provider

import (
	"context"

	"github.com/TokenFlux/TokenRouter/internal/provider"
)

// ApplyOAuthRefreshFailure 原独立计数替身未设置读取集时，视作调用版本存在；竞争场景必须明确提供当前行。
func (r *tokenRefreshProviderRepo) ApplyOAuthRefreshFailure(ctx context.Context, version provider.RefreshFailureVersion, failure provider.RefreshFailure) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if r.providersByID != nil && !refreshFailureMatchesFixture(r.providersByID[version.ID], version) {
		return false, nil
	}
	var err error
	if failure.Kind == provider.RefreshFailurePermanent {
		err = r.SetError(ctx, version.ID, failure.Message)
	} else {
		err = r.SetTempUnschedulable(ctx, version.ID, failure.Until, failure.Message)
	}
	if err == nil && failure.Kind == provider.RefreshFailurePermanent && r.providersByID != nil {
		if value := r.providersByID[version.ID]; value != nil {
			value.Status = provider.StatusError
			value.Schedulable = false
			value.ErrorMessage = failure.Message
		}
	}
	return err == nil, err
}

// 观察替身为凭据版本发布记录调用次数，供零调用断言检查。

func (b *tokenRefreshRuntimeBlocker) PrepareRefreshFailure(int64) func(provider.RefreshFailureNotice) {
	return func(provider.RefreshFailureNotice) { b.blockCalls++ }
}

func (r *tokenRefreshProviderRepo) ClearAntigravityRefreshRequest(ctx context.Context, version provider.CredentialVersion) (bool, error) {
	if r.providersByID != nil {
		value := r.providersByID[version.ID]
		if value == nil || !refreshFailureMatchesFixture(value, provider.RefreshFailureVersion{CredentialVersion: version, Schedulable: value.Schedulable}) {
			return false, nil
		}
	}
	err := r.UpdateExtra(ctx, version.ID, provider.ClearedAntigravityRefreshRequest())
	return err == nil, err
}
