package provider

import (
	"context"
	"errors"
	"log/slog"
	"time"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
)

// refreshAttemptFixture 只装配生产组件；重试、清理与熔断均由 provider 的唯一实现执行。
type refreshAttemptFixture struct {
	Attempts providercore.RefreshAttempts
	Post     *providercore.RefreshPostActions
}

func newRefreshAttemptFixture(repo *tokenRefreshProviderRepo, tuning *providercore.RefreshTuning, invalidator providercore.TokenCacheInvalidator, scheduler *tokenRefreshSchedulerCache, cooldown providercore.TempUnschedCache) *refreshAttemptFixture {
	post := &providercore.RefreshPostActions{
		Now: time.Now, Info: slog.Info, Warn: slog.Warn, Debug: slog.Debug,
		RequestClearer: repo, NeedsReauth: providercore.GrokNeedsReauth,
		ClearBlock: func(int64) {},
		ClearReauth: func(ctx context.Context, value *providercore.Record) {
			providercore.ClearGrokNeedsReauth(ctx, repo, value.ID)
		},
		ClearError: func(context.Context, *providercore.Record) (bool, error) {
			return false, errors.New("refresh error conditional writer is not configured")
		},
		ClearCooldown: func(ctx context.Context, value *providercore.Record) (bool, error) {
			return repo.ClearRefreshCooldownIfUnchanged(ctx, providercore.ObserveRefreshCooldown(value))
		},
	}
	if invalidator != nil {
		post.Invalidate = invalidator.InvalidateToken
	}
	if scheduler != nil {
		post.SyncProvider = scheduler.SetProvider
	}
	if cooldown != nil {
		post.DeleteCooldown = cooldown.DeleteTempUnsched
	}
	return &refreshAttemptFixture{Post: post, Attempts: providercore.RefreshAttempts{
		Tuning: tuning, Policy: providercore.DefaultBackgroundRefreshPolicy(), AttemptTimeout: tuning.AttemptTimeout(0, 0, false),
		Now: time.Now, Info: slog.Info, Warn: slog.Warn, Error: slog.Error,
		NonRetryable: IsNonRetryableRefreshError, SharedProviderError: IsSharedProviderRefreshError,
		AmbiguousEntitlement: IsAmbiguousGrokEntitlementRefreshError,
		FailureWriter:        repo, GrokMutation: repo, Invalidate: post.Invalidate,
		PrepareFailure:      func(*providercore.Record) func(time.Time, string) { return func(time.Time, string) {} },
		ClearRefreshRequest: post.ClearRefreshRequest, PostActions: post.Run, SyncCleanup: post.SyncWithCleanup,
		Persist: func(ctx context.Context, value *providercore.Record, credentials map[string]any) error {
			_, err := providercore.PersistCredentials(ctx, repo, value, credentials, slog.Warn)
			return err
		},
	}}
}

func newRefreshAPI(repo providercore.RefreshRepository, cache providercore.RefreshCache) *providercore.OAuthRefreshAPI {
	return providercore.NewOAuthRefreshAPI(repo, cache, providercore.RefreshOptions{Platform: providercore.ProviderRefreshPlatformPolicy()})
}

// refreshRecordFixture 模拟提供商记录读取。
type refreshRecordFixture struct {
	providersByID map[int64]*providercore.Record
}

func (r *refreshRecordFixture) GetByID(_ context.Context, id int64) (*providercore.Record, error) {
	if value := r.providersByID[id]; value != nil {
		return value, nil
	}
	return nil, errors.New("provider not found")
}
