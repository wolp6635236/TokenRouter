package provider

import (
	"context"
	"errors"
	"maps"
	"testing"
	"time"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

// 返回旧交换失败之前，模拟管理员已经持久化一份新凭据。
type oldFailureRefresher struct {
	row     *providercore.Record
	failure error
}

func (*oldFailureRefresher) CanRefresh(*providercore.Record) bool { return true }

func (*oldFailureRefresher) NeedsRefresh(*providercore.Record, time.Duration) bool { return true }

func (*oldFailureRefresher) CacheKey(*providercore.Record) string { return "fixture:old-failure" }

func (r *oldFailureRefresher) Refresh(context.Context, *providercore.Record) (map[string]any, error) {
	r.row.Credentials = map[string]any{"access_token": "fresh-admin-fixture", "refresh_token": "fresh-refresh-fixture"}
	return nil, r.failure
}

type failureBlocker struct{ calls int }

func (b *failureBlocker) PrepareRefreshFailure(int64) func(providercore.RefreshFailureNotice) {
	return func(providercore.RefreshFailureNotice) { b.calls++ }
}

func TestBackgroundFailureCannotBlockNewCredentials(t *testing.T) {
	for _, engine := range []string{"fallback", "unified"} {
		for _, platform := range []string{capability.PlatformAnthropic, capability.PlatformOpenAI, capability.PlatformGemini, capability.PlatformAntigravity, capability.PlatformQoder} {
			for _, mode := range []string{"permanent", "retry_exhausted"} {
				t.Run(platform+"/"+mode+"/"+engine, func(t *testing.T) {
					row := &providercore.Record{ID: 1, Platform: platform, Type: capability.ProviderTypeOAuth, Status: providercore.StatusActive, Schedulable: true, Credentials: map[string]any{"access_token": "old-fixture", "refresh_token": "old-refresh-fixture"}}
					if platform == capability.PlatformQoder {
						row.Type = capability.ProviderTypeCosy
					}
					if platform == capability.PlatformAntigravity {
						row.Extra = providercore.AntigravityForceTokenRefreshExtra("fixture 401")
					}
					snapshot := *row
					snapshot.Credentials = maps.Clone(row.Credentials)
					repo := &tokenRefreshProviderRepo{}
					repo.providersByID = map[int64]*providercore.Record{1: row}
					blocker := &failureBlocker{}
					s := newRefreshAttemptFixture(repo, &providercore.RefreshTuning{MaxRetries: 1}, nil, nil, nil)
					s.Attempts.PrepareFailure = func(v *providercore.Record) func(time.Time, string) {
						return providercore.PrepareRefreshFailureNotice(blocker, v)
					}
					if engine == "unified" {
						s.Attempts.API = newRefreshAPI(repo, nil)
					}
					message := "invalid_grant fixture"
					if mode == "retry_exhausted" {
						message = "temporary upstream fixture"
					}
					refresher := &oldFailureRefresher{row: row, failure: errors.New(message)}
					require.Error(t, s.Attempts.Run(context.Background(), &snapshot, refresher, refresher, time.Hour, nil))
					require.Zero(t, repo.setErrorCalls+repo.setTempUnschedCalls, "旧失败不能修改新身份的健康状态")
					require.Zero(t, blocker.calls, "旧失败不能阻断新身份的内存调度")
					require.Zero(t, repo.updateExtraCalls, "旧失败不能退休新身份的强制刷新标记")
				})
			}
		}
	}
}
