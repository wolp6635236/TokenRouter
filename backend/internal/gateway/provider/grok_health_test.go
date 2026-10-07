package provider_test

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	gatewaytestkit "github.com/TokenFlux/TokenRouter/internal/gateway/testkit"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/upstream/grok"
	"github.com/stretchr/testify/require"
)

type grokQuotaProviderRepo struct {
	gatewaytestkit.HealthStoreBase
	providersByID         map[int64]*gatewayprovider.ExecutionProvider
	updates               map[int64]map[string]any
	updateCalls           int
	rateLimitedCalls      int
	lastRateLimitedID     int64
	lastRateLimitResetAt  time.Time
	tempUnschedCalls      int
	lastTempUnschedID     int64
	lastTempUnschedUntil  time.Time
	lastTempUnschedReason string
	recoveryClearCalls    int
	recoveryObservedAt    time.Time
	recoveryObservedReset time.Time
	recoveryClearResult   bool
}

func (r *grokQuotaProviderRepo) UpdateExtra(_ context.Context, id int64, updates map[string]any) error {
	r.updateCalls++
	if r.updates == nil {
		r.updates = make(map[int64]map[string]any)
	}
	r.updates[id] = updates
	if r.providersByID != nil {
		provider := r.providersByID[id]
		if provider == nil {
			return nil
		}
		if provider.Record.Extra == nil {
			provider.Record.Extra = make(map[string]any)
		}
		for key, value := range updates {
			provider.Record.Extra[key] = value
		}
	}
	return nil
}

func (r *grokQuotaProviderRepo) SetRateLimited(_ context.Context, id int64, resetAt time.Time) error {
	r.rateLimitedCalls++
	r.lastRateLimitedID = id
	r.lastRateLimitResetAt = resetAt
	return nil
}

func (r *grokQuotaProviderRepo) SetRateLimitedIfLater(ctx context.Context, id int64, resetAt time.Time) error {
	return r.SetRateLimited(ctx, id, resetAt)
}

func (r *grokQuotaProviderRepo) ClearRateLimitIfObserved(_ context.Context, _ int64, observedLimitedAt, observedResetAt time.Time) (bool, error) {
	r.recoveryClearCalls++
	r.recoveryObservedAt = observedLimitedAt
	r.recoveryObservedReset = observedResetAt
	return r.recoveryClearResult, nil
}

func (r *grokQuotaProviderRepo) SetTempUnschedulable(_ context.Context, id int64, until time.Time, reason string) error {
	r.tempUnschedCalls++
	r.lastTempUnschedID = id
	r.lastTempUnschedUntil = until
	r.lastTempUnschedReason = reason
	return nil
}

// grokPoolPolicyProviderRepo 记录 Grok 池模式错误策略产生的提供商状态写入。
type grokPoolPolicyProviderRepo struct {
	*grokQuotaProviderRepo
	setErrorCalls            int
	overloadedCalls          int
	modelRateLimitCalls      int
	lastModelRateLimitScope  string
	lastModelRateLimitReason string
}

func (r *grokPoolPolicyProviderRepo) SetError(_ context.Context, _ int64, _ string) error {
	r.setErrorCalls++
	return nil
}

func (r *grokPoolPolicyProviderRepo) SetOverloaded(_ context.Context, _ int64, _ time.Time) error {
	r.overloadedCalls++
	return nil
}

func (r *grokPoolPolicyProviderRepo) SetModelRateLimit(_ context.Context, _ int64, scope string, _ time.Time, reason ...string) error {
	r.modelRateLimitCalls++
	r.lastModelRateLimitScope = scope
	if len(reason) > 0 {
		r.lastModelRateLimitReason = reason[0]
	}
	return nil
}

// newGrokPoolProvider 返回开启池模式的 Grok API Key 提供商。
func newGrokPoolProvider(id int64) *gatewayprovider.ExecutionProvider {
	return &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: id,
			Platform:    capability.PlatformGrok,
			Type:        capability.ProviderTypeAPIKey,
			Status:      billing.StatusActive,
			Schedulable: true,
			Credentials: map[string]any{"pool_mode": true},
		},
	}
}

// newGrokHealthForTest 测试直接组合生产健康组件；传输、凭据和完整网关不参与这些状态断言。
func newGrokHealthForTest(store provideradapter.GrokHealthStore, throttle *providercore.WriteThrottle) *provideradapter.GrokHealth {
	return &provideradapter.GrokHealth{Store: store, Throttle: throttle, Runtime: providercore.NewRuntimeBlockState(time.Now), ModelTransient: providercore.NewModelTransientState(0), NormalizeModel: func(value *providercore.Record, model string) string {
		return (gatewayprovider.ModelPolicy{Record: value}).NormalizeOpenAI(model)
	}}
}

func newGrokPoolHealthForTest(value *gatewayprovider.ExecutionProvider) (*provideradapter.GrokHealth, *grokPoolPolicyProviderRepo) {
	repo := &grokPoolPolicyProviderRepo{grokQuotaProviderRepo: &grokQuotaProviderRepo{providersByID: map[int64]*gatewayprovider.ExecutionProvider{value.Record.ID: value}}}
	health := newGrokHealthForTest(repo, nil)
	health.Health = gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{Store: repo, Options: providercore.HealthOptions{Block: health.Runtime.BlockProviderScheduling}})
	return health, repo
}

func (r *grokQuotaProviderRepo) GetByID(_ context.Context, id int64) (*gatewayprovider.ExecutionProvider, error) {
	return r.providersByID[id], nil
}

func handleGrokHealthForTest(health *provideradapter.GrokHealth, ctx context.Context, value *gatewayprovider.ExecutionProvider, status int, headers http.Header, body []byte, models ...string) bool {
	return gatewayprovider.ApplyGrokExecutionHealth(ctx, health, value, status, headers, body, "", models...).StopScheduling
}

func handleGrokHealthWithTeamForTest(health *provideradapter.GrokHealth, team string, ctx context.Context, value *gatewayprovider.ExecutionProvider, status int, headers http.Header, body []byte, models ...string) bool {
	return gatewayprovider.ApplyGrokExecutionHealth(ctx, health, value, status, headers, body, team, models...).StopScheduling
}

type grokHealthTestClock struct{ nanos atomic.Int64 }

func (c *grokHealthTestClock) Now() time.Time {
	if n := c.nanos.Load(); n != 0 {
		return time.Unix(0, n)
	}
	return time.Now()
}
func (c *grokHealthTestClock) Set(now time.Time) { c.nanos.Store(now.UnixNano()) }
func bindGrokHealthClockForTest(health *provideradapter.GrokHealth) *grokHealthTestClock {
	clock := &grokHealthTestClock{}
	health.Runtime = providercore.NewRuntimeBlockState(clock.Now)
	return clock
}

func bindExpiredGrokHealthForTest(health *provideradapter.GrokHealth, id int64, expired time.Time) {
	clock := bindGrokHealthClockForTest(health)
	clock.Set(expired.Add(-time.Minute))
	health.Runtime.Block(id, expired, "原过期快照夹具")
	clock.nanos.Store(0)
}
func grokInt64PtrForTest(v int64) *int64 { return &v }

const (
	grokQuotaSnapshotExtraKey        = "grok_usage_snapshot"
	grokRateLimitFallbackCooldown    = 2 * time.Minute
	grokRateLimitRepeatCooldown      = 10 * time.Minute
	grokRateLimitSustainedCooldown   = 30 * time.Minute
	grokRateLimitMaxAdaptiveCooldown = time.Hour
	grokRateLimitBackoffQuietPeriod  = time.Hour
	grokSpendingLimitProbeCooldown   = 10 * time.Minute
)

func TestHandleGrokProviderUpstreamErrorPoolModeSkipsDefaultLocalState(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		headers    http.Header
		body       []byte
	}{
		{name: "unauthorized", statusCode: http.StatusUnauthorized},
		{name: "payment required", statusCode: http.StatusPaymentRequired},
		{name: "forbidden", statusCode: http.StatusForbidden, body: []byte(`{"error":{"message":"access denied"}}`)},
		{name: "rate limited", statusCode: http.StatusTooManyRequests, headers: http.Header{"Retry-After": []string{"60"}}},
		{name: "server error", statusCode: http.StatusInternalServerError},
		{name: "overloaded", statusCode: 529},
	}

	for index, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := newGrokPoolProvider(int64(620 + index))
			svc, repo := newGrokPoolHealthForTest(provider)

			shouldDisable := handleGrokHealthForTest(svc,
				context.Background(),
				provider,
				tt.statusCode,
				tt.headers,
				tt.body,
				"grok-4.5",
			)

			require.False(t, shouldDisable)
			require.False(t, svc.Runtime.Blocked(provider.Record.ID, func() string { return providercore.RefreshCredentialIdentity(provider.View()) }))
			require.Zero(t, repo.rateLimitedCalls)
			require.Zero(t, repo.tempUnschedCalls)
			require.Zero(t, repo.setErrorCalls)
			require.Zero(t, repo.overloadedCalls)
			require.Zero(t, repo.modelRateLimitCalls)
			require.Nil(t, provider.Record.RateLimitResetAt)
			require.Nil(t, provider.Record.TempUnschedulableUntil)

			if tt.statusCode == http.StatusTooManyRequests {
				require.Equal(t, 1, repo.updateCalls)
				stored, ok := provider.Record.Extra[grokQuotaSnapshotExtraKey].(*grok.QuotaSnapshot)
				require.True(t, ok)
				require.NotNil(t, stored.RetryAfterSeconds)
				require.Equal(t, 60, *stored.RetryAfterSeconds)
			}
		})
	}
}

func TestUpdateGrokUsageSnapshotPoolModeExhaustedSuccessIsObservationOnly(t *testing.T) {
	provider := newGrokPoolProvider(626)
	svc, repo := newGrokPoolHealthForTest(provider)
	resetAt := time.Now().Add(10 * time.Minute).UTC().Truncate(time.Second)
	headers := http.Header{
		"X-Ratelimit-Limit-Requests":     []string{"10"},
		"X-Ratelimit-Remaining-Requests": []string{"0"},
		"X-Ratelimit-Reset-Requests":     []string{fmt.Sprintf("%d", resetAt.Unix())},
	}

	svc.ObserveResponse(context.Background(), provider.View(), headers, http.StatusOK, "")

	require.Equal(t, 1, repo.updateCalls)
	require.Zero(t, repo.rateLimitedCalls)
	require.False(t, svc.Runtime.Blocked(provider.Record.ID, func() string { return providercore.RefreshCredentialIdentity(provider.View()) }))
	stored, ok := provider.Record.Extra[grokQuotaSnapshotExtraKey].(*grok.QuotaSnapshot)
	require.True(t, ok)
	require.NotNil(t, stored.Requests)
	require.NotNil(t, stored.Requests.Remaining)
	require.Zero(t, *stored.Requests.Remaining)
}

func TestHandleGrokProviderUpstreamErrorPoolModeKeepsExplicitPolicies(t *testing.T) {
	t.Run("custom error code still disables provider", func(t *testing.T) {
		provider := newGrokPoolProvider(627)
		provider.Record.Credentials["custom_error_codes_enabled"] = true
		provider.Record.Credentials["custom_error_codes"] = []any{float64(http.StatusUnauthorized)}
		svc, repo := newGrokPoolHealthForTest(provider)

		shouldDisable := handleGrokHealthForTest(svc,
			context.Background(),
			provider,
			http.StatusUnauthorized,
			nil,
			[]byte(`{"error":{"message":"invalid api key"}}`),
			"grok-4.5",
		)

		require.True(t, shouldDisable)
		require.Equal(t, 1, repo.setErrorCalls)
		require.True(t, svc.Runtime.Blocked(provider.Record.ID, func() string { return providercore.RefreshCredentialIdentity(provider.View()) }))
	})

	t.Run("custom non-failover code still disables provider", func(t *testing.T) {
		provider := newGrokPoolProvider(633)
		provider.Record.Credentials["custom_error_codes_enabled"] = true
		provider.Record.Credentials["custom_error_codes"] = []any{float64(http.StatusUnprocessableEntity)}
		svc, repo := newGrokPoolHealthForTest(provider)

		decision := gatewayprovider.ApplyGrokExecutionHealth(context.Background(), svc, provider, http.StatusUnprocessableEntity, nil, []byte(`{"error":{"message":"configured"}}`), "", "grok-4.5")

		require.Equal(t, providercore.ErrorPolicyCustomMatched, decision.Policy)
		require.True(t, decision.ShouldFailover(gatewayprovider.ExecutionErrorPolicy(provider), http.StatusUnprocessableEntity, false))
		require.False(t, decision.RetryableOnSameProvider(gatewayprovider.ExecutionErrorPolicy(provider), http.StatusUnprocessableEntity))
		require.Equal(t, 1, repo.setErrorCalls)
		require.True(t, svc.Runtime.Blocked(provider.Record.ID, func() string { return providercore.RefreshCredentialIdentity(provider.View()) }))
	})

	t.Run("matching temporary rule only pauses requested model", func(t *testing.T) {
		provider := newGrokPoolProvider(628)
		provider.Record.Credentials["temp_unschedulable_enabled"] = true
		provider.Record.Credentials["temp_unschedulable_rules"] = []any{
			map[string]any{
				"error_code":       float64(http.StatusServiceUnavailable),
				"keywords":         []any{"maintenance"},
				"duration_minutes": float64(30),
			},
		}
		svc, repo := newGrokPoolHealthForTest(provider)

		shouldDisable := handleGrokHealthForTest(svc,
			context.Background(),
			provider,
			http.StatusServiceUnavailable,
			nil,
			[]byte(`{"error":{"message":"maintenance in progress"}}`),
			"grok-4.5",
		)

		require.True(t, shouldDisable)
		require.Equal(t, 1, repo.modelRateLimitCalls)
		require.Equal(t, "grok-4.5", repo.lastModelRateLimitScope)
		require.Zero(t, repo.tempUnschedCalls)
		require.False(t, svc.Runtime.Blocked(provider.Record.ID, func() string { return providercore.RefreshCredentialIdentity(provider.View()) }))
	})

	t.Run("unmatched temporary rule keeps default pool behavior", func(t *testing.T) {
		provider := newGrokPoolProvider(629)
		provider.Record.Credentials["temp_unschedulable_enabled"] = true
		provider.Record.Credentials["temp_unschedulable_rules"] = []any{
			map[string]any{
				"error_code":       float64(http.StatusServiceUnavailable),
				"keywords":         []any{"maintenance"},
				"duration_minutes": float64(30),
			},
		}
		svc, repo := newGrokPoolHealthForTest(provider)

		shouldDisable := handleGrokHealthForTest(svc,
			context.Background(),
			provider,
			http.StatusServiceUnavailable,
			nil,
			[]byte(`{"error":{"message":"temporary outage"}}`),
			"grok-4.5",
		)

		require.False(t, shouldDisable)
		require.Zero(t, repo.modelRateLimitCalls)
		require.Zero(t, repo.tempUnschedCalls)
		require.False(t, svc.Runtime.Blocked(provider.Record.ID, func() string { return providercore.RefreshCredentialIdentity(provider.View()) }))
	})
}

func TestHandleGrokProviderUpstreamErrorTempUnschedulesNonRateLimitStates(t *testing.T) {
	tests := []struct {
		name            string
		status          int
		headers         http.Header
		wantReason      string
		wantMinCooldown time.Duration
		wantMaxCooldown time.Duration
	}{
		{
			name:            "unauthorized reauth",
			status:          http.StatusUnauthorized,
			wantReason:      "grok credentials unauthorized",
			wantMinCooldown: 10*time.Minute - time.Second,
			wantMaxCooldown: 10*time.Minute + time.Second,
		},
		{
			name:            "forbidden entitlement",
			status:          http.StatusForbidden,
			wantReason:      "grok access or entitlement denied",
			wantMinCooldown: 30*time.Minute - time.Second,
			wantMaxCooldown: 30*time.Minute + time.Second,
		},
		{
			name:            "payment required",
			status:          http.StatusPaymentRequired,
			wantReason:      "grok payment required",
			wantMinCooldown: 30*time.Minute - time.Second,
			wantMaxCooldown: 30*time.Minute + time.Second,
		},
		{
			name:            "method not allowed",
			status:          http.StatusMethodNotAllowed,
			wantReason:      "grok endpoint not supported (405)",
			wantMinCooldown: 30*time.Minute - time.Second,
			wantMaxCooldown: 30*time.Minute + time.Second,
		},
		{
			name:            "upstream temporary error",
			status:          http.StatusInternalServerError,
			wantReason:      "grok upstream temporary error",
			wantMinCooldown: 2*time.Minute - time.Second,
			wantMaxCooldown: 2*time.Minute + time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, ID: 61, Platform: capability.PlatformGrok, Type: capability.ProviderTypeOAuth}}
			repo := &grokQuotaProviderRepo{}
			svc := newGrokHealthForTest(repo, nil)
			before := time.Now()

			handleGrokHealthForTest(svc, context.Background(), provider, tt.status, tt.headers, nil)

			require.True(t, svc.Runtime.Blocked(provider.Record.ID, func() string { return providercore.RefreshCredentialIdentity(provider.View()) }))
			require.Equal(t, 1, repo.tempUnschedCalls)
			require.Zero(t, repo.rateLimitedCalls)
			require.Equal(t, provider.Record.ID, repo.lastTempUnschedID)
			require.Equal(t, tt.wantReason, repo.lastTempUnschedReason)
			require.True(t, repo.lastTempUnschedUntil.After(before.Add(tt.wantMinCooldown)))
			require.True(t, repo.lastTempUnschedUntil.Before(before.Add(tt.wantMaxCooldown)))
		})
	}
}

func TestHandleGrokProviderUpstreamErrorSpendingLimit403RateLimits(t *testing.T) {
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, ID: 614, Platform: capability.PlatformGrok, Type: capability.ProviderTypeOAuth}}
	repo := &grokQuotaProviderRepo{}
	svc := newGrokHealthForTest(repo, nil)
	before := time.Now()
	body := []byte(`{"code":"personal-team-blocked:spending-limit","error":"You have run out of credits"}`)

	handleGrokHealthForTest(svc, context.Background(), provider, http.StatusForbidden, nil, body)

	require.True(t, svc.Runtime.Blocked(provider.Record.ID, func() string { return providercore.RefreshCredentialIdentity(provider.View()) }))
	require.Equal(t, 1, repo.rateLimitedCalls)
	require.Equal(t, provider.Record.ID, repo.lastRateLimitedID)
	require.WithinDuration(t, before.Add(grokSpendingLimitProbeCooldown), repo.lastRateLimitResetAt, 2*time.Second)
	require.Zero(t, repo.tempUnschedCalls)
	require.True(t, grok.IsSpendingLimitError(body))
}

func TestHandleGrokProviderUpstreamError5xxRespectsPoolMode(t *testing.T) {
	t.Run("pool mode keeps scheduling state", func(t *testing.T) {
		provider := &gatewayprovider.ExecutionProvider{
			Record: providercore.Record{
				LoadLocation: time.LoadLocation, ID: 611,
				Platform: capability.PlatformGrok,
				Type:     capability.ProviderTypeAPIKey,
				Credentials: map[string]any{
					"pool_mode": true,
				},
			},
		}
		repo := &grokQuotaProviderRepo{}
		svc := newGrokHealthForTest(repo, nil)

		handleGrokHealthForTest(svc, context.Background(), provider, http.StatusBadGateway, nil, nil)

		require.False(t, svc.Runtime.Blocked(provider.Record.ID, func() string { return providercore.RefreshCredentialIdentity(provider.View()) }))
		require.Zero(t, repo.tempUnschedCalls)
		require.Nil(t, provider.Record.TempUnschedulableUntil)
		require.Empty(t, provider.Record.TempUnschedulableReason)
	})

	t.Run("non-pool mode keeps two minute cooldown", func(t *testing.T) {
		provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, ID: 612, Platform: capability.PlatformGrok, Type: capability.ProviderTypeAPIKey}}
		repo := &grokQuotaProviderRepo{}
		svc := newGrokHealthForTest(repo, nil)
		before := time.Now()

		handleGrokHealthForTest(svc, context.Background(), provider, http.StatusBadGateway, nil, nil)

		require.True(t, svc.Runtime.Blocked(provider.Record.ID, func() string { return providercore.RefreshCredentialIdentity(provider.View()) }))
		require.Equal(t, 1, repo.tempUnschedCalls)
		require.Equal(t, provider.Record.ID, repo.lastTempUnschedID)
		require.Equal(t, "grok upstream temporary error", repo.lastTempUnschedReason)
		require.WithinDuration(t, before.Add(2*time.Minute), repo.lastTempUnschedUntil, time.Second)
	})
}

func TestHandleGrokProviderUpstreamError405RespectsPoolMode(t *testing.T) {
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: 613,
			Platform: capability.PlatformGrok,
			Type:     capability.ProviderTypeAPIKey,
			Credentials: map[string]any{
				"pool_mode": true,
			},
		},
	}
	repo := &grokQuotaProviderRepo{}
	svc := newGrokHealthForTest(repo, nil)

	handleGrokHealthForTest(svc, context.Background(), provider, http.StatusMethodNotAllowed, nil, nil)

	require.False(t, svc.Runtime.Blocked(provider.Record.ID, func() string { return providercore.RefreshCredentialIdentity(provider.View()) }), "公共池提供商应跳过 405 默认冷却")
	require.Zero(t, repo.tempUnschedCalls)
	require.Nil(t, provider.Record.TempUnschedulableUntil)
}

func TestHandleGrokProviderUpstreamError429SetsRateLimitedFromRetryAfter(t *testing.T) {
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, ID: 61, Platform: capability.PlatformGrok, Type: capability.ProviderTypeOAuth}}
	repo := &grokQuotaProviderRepo{}
	svc := newGrokHealthForTest(repo, nil)
	before := time.Now()

	handleGrokHealthForTest(svc, context.Background(), provider, http.StatusTooManyRequests, http.Header{"Retry-After": []string{"45"}}, nil)

	require.True(t, svc.Runtime.Blocked(provider.Record.ID, func() string { return providercore.RefreshCredentialIdentity(provider.View()) }))
	require.Equal(t, 1, repo.rateLimitedCalls)
	require.Equal(t, provider.Record.ID, repo.lastRateLimitedID)
	require.WithinDuration(t, before.Add(45*time.Second), repo.lastRateLimitResetAt, time.Second)
	require.Zero(t, repo.tempUnschedCalls)
}

func TestHandleGrokProviderUpstreamError402RecoversAfterCooldownExpiry(t *testing.T) {
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: 610, Platform: capability.PlatformGrok, Type: capability.ProviderTypeOAuth,
			Status: billing.StatusActive, Schedulable: true,
		},
	}
	repo := &grokQuotaProviderRepo{}
	svc := newGrokHealthForTest(repo, nil)

	handleGrokHealthForTest(svc, context.Background(), provider, http.StatusPaymentRequired, nil, nil)
	require.True(t, svc.Runtime.Blocked(provider.Record.ID, func() string { return providercore.RefreshCredentialIdentity(provider.View()) }))
	require.Equal(t, 1, repo.tempUnschedCalls)

	expired := time.Now().Add(-time.Second)
	provider.Record.TempUnschedulableUntil = &expired
	bindExpiredGrokHealthForTest(svc, provider.Record.ID, expired)

	require.False(t, svc.Runtime.Blocked(provider.Record.ID, func() string { return providercore.RefreshCredentialIdentity(provider.View()) }))
	require.True(t, provider.View().IsSchedulable())
}

func TestHandleGrokProviderUpstreamError429UsesLatestExhaustedWindowReset(t *testing.T) {
	now := time.Now()
	requestReset := now.Add(10 * time.Minute).Truncate(time.Second)
	tokenReset := now.Add(20 * time.Minute).Truncate(time.Second)
	headers := http.Header{
		"X-Ratelimit-Limit-Requests":     []string{"10"},
		"X-Ratelimit-Remaining-Requests": []string{"0"},
		"X-Ratelimit-Reset-Requests":     []string{fmt.Sprintf("%d", requestReset.Unix())},
		"X-Ratelimit-Limit-Tokens":       []string{"1000"},
		"X-Ratelimit-Remaining-Tokens":   []string{"0"},
		"X-Ratelimit-Reset-Tokens":       []string{fmt.Sprintf("%d", tokenReset.Unix())},
	}
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, ID: 62, Platform: capability.PlatformGrok, Type: capability.ProviderTypeOAuth}}
	repo := &grokQuotaProviderRepo{}
	svc := newGrokHealthForTest(repo, nil)

	handleGrokHealthForTest(svc, context.Background(), provider, http.StatusTooManyRequests, headers, nil)

	require.Equal(t, 1, repo.rateLimitedCalls)
	require.WithinDuration(t, tokenReset, repo.lastRateLimitResetAt, time.Second)
	require.Zero(t, repo.tempUnschedCalls)
}

func TestHandleGrokProviderUpstreamError429UsesFallbackReset(t *testing.T) {
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, ID: 63, Platform: capability.PlatformGrok, Type: capability.ProviderTypeOAuth}}
	repo := &grokQuotaProviderRepo{}
	svc := newGrokHealthForTest(repo, nil)
	before := time.Now()

	handleGrokHealthForTest(svc, context.Background(), provider, http.StatusTooManyRequests, nil, nil)

	require.Equal(t, 1, repo.rateLimitedCalls)
	require.WithinDuration(t, before.Add(grokRateLimitFallbackCooldown), repo.lastRateLimitResetAt, time.Second)
	require.Zero(t, repo.tempUnschedCalls)
}

func TestGrokRateLimitResetAtForProviderEscalatesRepeated429s(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	retryAfter := 45
	snapshot := &grok.QuotaSnapshot{
		StatusCode:        http.StatusTooManyRequests,
		RetryAfterSeconds: &retryAfter,
		UpdatedAt:         now.Format(time.RFC3339),
	}
	tests := []struct {
		name             string
		previousCooldown time.Duration
		wantCooldown     time.Duration
	}{
		{name: "repeat after short boundary", previousCooldown: 45 * time.Second, wantCooldown: grokRateLimitRepeatCooldown},
		{name: "sustained repeat", previousCooldown: grokRateLimitRepeatCooldown, wantCooldown: grokRateLimitSustainedCooldown},
		{name: "capped repeat", previousCooldown: grokRateLimitSustainedCooldown, wantCooldown: grokRateLimitMaxAdaptiveCooldown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previousReset := now.Add(-time.Second)
			previousLimited := previousReset.Add(-tt.previousCooldown)
			provider := &gatewayprovider.ExecutionProvider{
				Record: providercore.Record{
					LoadLocation: time.LoadLocation, ID: 630,
					Platform:         capability.PlatformGrok,
					Type:             capability.ProviderTypeOAuth,
					RateLimitedAt:    &previousLimited,
					RateLimitResetAt: &previousReset,
				},
			}

			resetAt, limited := providercore.GrokRateLimitResetAtForProvider(provider.View(), snapshot, now)

			require.True(t, limited)
			require.WithinDuration(t, now.Add(tt.wantCooldown), resetAt, time.Second)
		})
	}
}

func TestGrokRateLimitResetAtForProviderPreservesAuthoritativeAndQuietRecovery(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	retryAfter := 45
	previousReset := now.Add(-grokRateLimitBackoffQuietPeriod - time.Second)
	previousLimited := previousReset.Add(-grokRateLimitSustainedCooldown)
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: 631,
			Platform:         capability.PlatformGrok,
			Type:             capability.ProviderTypeOAuth,
			RateLimitedAt:    &previousLimited,
			RateLimitResetAt: &previousReset,
		},
	}
	snapshot := &grok.QuotaSnapshot{
		StatusCode:        http.StatusTooManyRequests,
		RetryAfterSeconds: &retryAfter,
		UpdatedAt:         now.Format(time.RFC3339),
	}

	resetAt, limited := providercore.GrokRateLimitResetAtForProvider(provider.View(), snapshot, now)
	require.True(t, limited)
	require.WithinDuration(t, now.Add(45*time.Second), resetAt, time.Second)

	authoritativeReset := now.Add(2 * time.Hour)
	remaining := int64(0)
	snapshot.Requests = &grok.QuotaWindow{Remaining: &remaining, ResetUnix: grokInt64PtrForTest(authoritativeReset.Unix())}
	recentReset := now.Add(-time.Second)
	recentLimited := recentReset.Add(-grokRateLimitSustainedCooldown)
	provider.Record.RateLimitResetAt = &recentReset
	provider.Record.RateLimitedAt = &recentLimited

	resetAt, limited = providercore.GrokRateLimitResetAtForProvider(provider.View(), snapshot, now)
	require.True(t, limited)
	require.WithinDuration(t, authoritativeReset, resetAt, time.Second)
}

func TestGrokRateLimitResetAtForProviderLeavesAPIKey429PolicyUnchanged(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	retryAfter := 45
	previousReset := now.Add(-time.Second)
	previousLimited := previousReset.Add(-grokRateLimitSustainedCooldown)
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: 632,
			Platform:         capability.PlatformGrok,
			Type:             capability.ProviderTypeAPIKey,
			RateLimitedAt:    &previousLimited,
			RateLimitResetAt: &previousReset,
		},
	}
	snapshot := &grok.QuotaSnapshot{
		StatusCode:        http.StatusTooManyRequests,
		RetryAfterSeconds: &retryAfter,
		UpdatedAt:         now.Format(time.RFC3339),
	}

	resetAt, limited := providercore.GrokRateLimitResetAtForProvider(provider.View(), snapshot, now)
	require.True(t, limited)
	require.WithinDuration(t, now.Add(45*time.Second), resetAt, time.Second)
}

func TestGrokRateLimitResetAtUsesFutureWindowAfterRetryAfterExpires(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	observedAt := now.Add(-2 * time.Minute)
	windowReset := now.Add(15 * time.Minute)
	retryAfter := 30
	snapshot := &grok.QuotaSnapshot{
		StatusCode:        http.StatusTooManyRequests,
		UpdatedAt:         observedAt.Format(time.RFC3339),
		RetryAfterSeconds: &retryAfter,
		Requests: &grok.QuotaWindow{
			Limit:     grokInt64PtrForTest(10),
			Remaining: grokInt64PtrForTest(0),
			ResetUnix: grokInt64PtrForTest(windowReset.Unix()),
		},
	}

	resetAt, limited := providercore.GrokRateLimitResetAt(snapshot, now)

	require.True(t, limited)
	require.WithinDuration(t, windowReset, resetAt, time.Second)
}

func TestHandleGrokProviderUpstreamError429DoesNotShortenExistingPause(t *testing.T) {
	existingUntil := time.Now().Add(15 * time.Minute)
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: 64,
			Platform:                capability.PlatformGrok,
			Type:                    capability.ProviderTypeOAuth,
			TempUnschedulableUntil:  &existingUntil,
			TempUnschedulableReason: "existing pause",
		},
	}
	repo := &grokQuotaProviderRepo{}
	svc := newGrokHealthForTest(repo, nil)
	clock := bindGrokHealthClockForTest(svc)

	handleGrokHealthForTest(svc, context.Background(), provider, http.StatusTooManyRequests, http.Header{"Retry-After": []string{"45"}}, nil)

	require.Equal(t, 1, repo.rateLimitedCalls)
	require.WithinDuration(t, time.Now().Add(45*time.Second), repo.lastRateLimitResetAt, time.Second)
	require.Zero(t, repo.tempUnschedCalls)
	clock.Set(existingUntil.Add(-time.Second))
	require.True(t, svc.Runtime.Blocked(provider.Record.ID, func() string { return providercore.RefreshCredentialIdentity(provider.View()) }))
	clock.Set(existingUntil.Add(time.Second))
	require.False(t, svc.Runtime.Blocked(provider.Record.ID, func() string { return providercore.RefreshCredentialIdentity(provider.View()) }))
}

func TestUpdateGrokUsageSnapshotExhaustedSuccessBypassesThrottleAndSetsRateLimited(t *testing.T) {
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, ID: 65, Platform: capability.PlatformGrok, Type: capability.ProviderTypeOAuth}}
	repo := &grokQuotaProviderRepo{}
	svc := newGrokHealthForTest(repo, providercore.NewWriteThrottle(time.Hour))
	now := time.Now()

	// 先消耗普通快照的写入额度。
	svc.StoreSnapshot(context.Background(), provider.View(), &grok.QuotaSnapshot{
		StatusCode: http.StatusOK,
		Requests: &grok.QuotaWindow{
			Limit:     grokInt64PtrForTest(10),
			Remaining: grokInt64PtrForTest(9),
		},
		UpdatedAt: now.UTC().Format(time.RFC3339),
	}, true, "")
	resetAt := now.Add(30 * time.Minute).Truncate(time.Second)
	svc.StoreSnapshot(context.Background(), provider.View(), &grok.QuotaSnapshot{
		StatusCode: http.StatusOK,
		Requests: &grok.QuotaWindow{
			Limit:     grokInt64PtrForTest(10),
			Remaining: grokInt64PtrForTest(0),
			ResetUnix: grokInt64PtrForTest(resetAt.Unix()),
			ResetAt:   resetAt.UTC().Format(time.RFC3339),
		},
		UpdatedAt: now.UTC().Format(time.RFC3339),
	}, true, "")

	require.Equal(t, 2, repo.updateCalls)
	require.Equal(t, 1, repo.rateLimitedCalls)
	require.Equal(t, provider.Record.ID, repo.lastRateLimitedID)
	require.WithinDuration(t, resetAt, repo.lastRateLimitResetAt, time.Second)
	require.True(t, svc.Runtime.Blocked(provider.Record.ID, func() string { return providercore.RefreshCredentialIdentity(provider.View()) }))
}

func TestUpdateGrokUsageSnapshotAvailableSuccessDoesNotSetRateLimited(t *testing.T) {
	repo := &grokQuotaProviderRepo{}
	svc := newGrokHealthForTest(repo, nil)
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, ID: 66, Platform: capability.PlatformGrok, Type: capability.ProviderTypeOAuth}}

	svc.StoreSnapshot(context.Background(), provider.View(), &grok.QuotaSnapshot{
		StatusCode: http.StatusOK,
		Requests: &grok.QuotaWindow{
			Limit:     grokInt64PtrForTest(10),
			Remaining: grokInt64PtrForTest(1),
		},
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}, true, "")

	require.Equal(t, 1, repo.updateCalls)
	require.Zero(t, repo.rateLimitedCalls)
}

func TestUpdateGrokUsageFromResponseHeaderlessSuccessClearsObservedCooldown(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	limitedAt := now.Add(-grokRateLimitRepeatCooldown)
	observedResetAt := now.Add(-time.Second)
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: 660,
			Platform:         capability.PlatformGrok,
			Type:             capability.ProviderTypeOAuth,
			RateLimitedAt:    &limitedAt,
			RateLimitResetAt: &observedResetAt,
		},
	}
	repo := &grokQuotaProviderRepo{recoveryClearResult: true}
	svc := newGrokHealthForTest(repo, providercore.NewWriteThrottle(time.Hour))

	svc.ObserveResponse(context.Background(), provider.View(), nil, http.StatusOK, "")

	require.Zero(t, repo.updateCalls, "headerless success must not overwrite an informative quota snapshot")
	require.Equal(t, 1, repo.recoveryClearCalls)
	require.Equal(t, limitedAt, repo.recoveryObservedAt)
	require.Equal(t, observedResetAt, repo.recoveryObservedReset)
	require.Same(t, &observedResetAt, provider.Record.RateLimitResetAt, "shared provider snapshots must not be mutated in place")
}

func TestUpdateGrokUsageFromResponseRecoveryRespectsCancellationAndAPIKeyBoundary(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	observedResetAt := now.Add(-time.Second)
	observedLimitedAt := observedResetAt.Add(-grokRateLimitRepeatCooldown)

	t.Run("parent cancellation does not mutate provider state", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		provider := &gatewayprovider.ExecutionProvider{
			Record: providercore.Record{
				LoadLocation: time.LoadLocation, ID: 661,
				Platform:         capability.PlatformGrok,
				Type:             capability.ProviderTypeOAuth,
				RateLimitedAt:    &observedLimitedAt,
				RateLimitResetAt: &observedResetAt,
			},
		}
		repo := &grokQuotaProviderRepo{recoveryClearResult: true}
		svc := newGrokHealthForTest(repo, nil)

		svc.ObserveResponse(ctx, provider.View(), nil, http.StatusOK, "")

		require.Zero(t, repo.recoveryClearCalls)
	})

	t.Run("API key success does not alter OAuth cooldown state", func(t *testing.T) {
		provider := &gatewayprovider.ExecutionProvider{
			Record: providercore.Record{
				LoadLocation: time.LoadLocation, ID: 662,
				Platform:         capability.PlatformGrok,
				Type:             capability.ProviderTypeAPIKey,
				RateLimitedAt:    &observedLimitedAt,
				RateLimitResetAt: &observedResetAt,
			},
		}
		repo := &grokQuotaProviderRepo{recoveryClearResult: true}
		svc := newGrokHealthForTest(repo, nil)

		svc.ObserveResponse(context.Background(), provider.View(), nil, http.StatusOK, "")

		require.Zero(t, repo.recoveryClearCalls)
	})
}

func TestUpdateGrokUsageSnapshotExhaustedSuccessWithoutResetUsesFallback(t *testing.T) {
	repo := &grokQuotaProviderRepo{}
	svc := newGrokHealthForTest(repo, nil)
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, ID: 67, Platform: capability.PlatformGrok, Type: capability.ProviderTypeOAuth}}
	before := time.Now()

	svc.StoreSnapshot(context.Background(), provider.View(), &grok.QuotaSnapshot{
		StatusCode: http.StatusOK,
		Tokens: &grok.QuotaWindow{
			Limit:     grokInt64PtrForTest(2_000_000),
			Remaining: grokInt64PtrForTest(0),
		},
		UpdatedAt: before.UTC().Format(time.RFC3339),
	}, true, "")

	require.Equal(t, 1, repo.rateLimitedCalls)
	require.WithinDuration(t, before.Add(grokRateLimitFallbackCooldown), repo.lastRateLimitResetAt, time.Second)
	stored, ok := repo.updates[provider.Record.ID][grokQuotaSnapshotExtraKey].(*grok.QuotaSnapshot)
	require.True(t, ok)
	require.NotNil(t, stored.Tokens.ResetUnix)
	paused, _ := providercore.GrokQuotaWindowAutoPause("tokens", stored.Tokens, before.Add(time.Second))
	require.True(t, paused)
	paused, _ = providercore.GrokQuotaWindowAutoPause("tokens", stored.Tokens, repo.lastRateLimitResetAt.Add(time.Second))
	require.False(t, paused)
}

func TestHandleGrokProviderUpstreamErrorEntitlement403KeepsDefaultCooldown(t *testing.T) {
	repo := &grokQuotaProviderRepo{}
	svc := newGrokHealthForTest(repo, nil)
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, ID: 4716, Platform: capability.PlatformGrok, Type: capability.ProviderTypeOAuth}}
	before := time.Now()

	handleGrokHealthForTest(svc,
		context.Background(), provider, http.StatusForbidden, nil,
		[]byte(`{"error":{"message":"subscription required"}}`),
	)

	require.Equal(t, 1, repo.tempUnschedCalls)
	require.Equal(t, "grok access or entitlement denied", repo.lastTempUnschedReason)
	require.Greater(t, repo.lastTempUnschedUntil, before.Add(29*time.Minute))
	require.Less(t, repo.lastTempUnschedUntil, before.Add(31*time.Minute))
}

func TestHandleGrokProviderUpstreamError403UsesConfiguredRule(t *testing.T) {
	repo := &grokQuotaProviderRepo{}
	svc := newGrokHealthForTest(repo, nil)
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: 4717,
			Platform: capability.PlatformGrok,
			Type:     capability.ProviderTypeOAuth,
			Credentials: map[string]any{
				"temp_unschedulable_enabled": true,
				"temp_unschedulable_rules": []any{
					map[string]any{
						"error_code":       float64(http.StatusForbidden),
						"keywords":         []any{"subscription"},
						"duration_minutes": float64(7),
					},
				},
			},
		},
	}
	before := time.Now()

	handleGrokHealthForTest(svc,
		context.Background(), provider, http.StatusForbidden, nil,
		[]byte(`{"error":{"message":"subscription required"}}`),
	)

	require.Equal(t, 1, repo.tempUnschedCalls)
	require.Greater(t, repo.lastTempUnschedUntil, before.Add(6*time.Minute))
	require.Less(t, repo.lastTempUnschedUntil, before.Add(8*time.Minute))
}

func TestHandleGrokProviderUpstreamError403ConfiguredUnmatchedKeepsDefaultCooldown(t *testing.T) {
	repo := &grokQuotaProviderRepo{}
	svc := newGrokHealthForTest(repo, nil)
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: 4718,
			Platform: capability.PlatformGrok,
			Type:     capability.ProviderTypeOAuth,
			Credentials: map[string]any{
				"temp_unschedulable_enabled": true,
				"temp_unschedulable_rules": []any{
					map[string]any{
						"error_code":       float64(http.StatusForbidden),
						"keywords":         []any{"different failure"},
						"duration_minutes": float64(7),
					},
				},
			},
		},
	}

	handleGrokHealthForTest(svc,
		context.Background(), provider, http.StatusForbidden, nil,
		[]byte(`{"error":{"message":"subscription required"}}`),
	)

	require.Equal(t, 1, repo.tempUnschedCalls)
	require.Equal(t, "grok access or entitlement denied", repo.lastTempUnschedReason)
	require.True(t, svc.Runtime.Blocked(provider.Record.ID, func() string { return providercore.RefreshCredentialIdentity(provider.View()) }))
}

func TestHandleGrokProviderUpstreamError_FreeUsageBodyCoolsProvider(t *testing.T) {
	repo := &grokQuotaProviderRepo{}
	svc := newGrokHealthForTest(repo, nil)
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, ID: 9101, Platform: capability.PlatformGrok, Type: capability.ProviderTypeOAuth}}
	before := time.Now()
	body := []byte(`{"error":{"code":"subscription:free-usage-exhausted","message":"You've used all the included free usage. Usage resets over a rolling 24-hour window."}}`)

	handleGrokHealthForTest(svc, context.Background(), provider, http.StatusBadRequest, nil, body)

	require.Equal(t, 1, repo.tempUnschedCalls)
	require.Equal(t, "grok free usage exhausted", repo.lastTempUnschedReason)
	// 滚动窗口耗尽且缺少上游绝对重置时间时必须使用短期探测冷却，
	// 不得在此启动 24 小时锁定。
	require.Greater(t, repo.lastTempUnschedUntil, before.Add(grok.GrokFreeUsageProbeCooldown-time.Second))
	require.Less(t, repo.lastTempUnschedUntil, before.Add(grok.GrokFreeUsageProbeCooldown+time.Second))
}

func TestHandleGrokProviderUpstreamError_FreeUsageUsesUpstreamReset(t *testing.T) {
	repo := &grokQuotaProviderRepo{}
	svc := newGrokHealthForTest(repo, nil)
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, ID: 9102, Platform: capability.PlatformGrok, Type: capability.ProviderTypeOAuth}}
	body := []byte(`{"error":{"code":"subscription:free-usage-exhausted","message":"free usage exhausted; rolling 24-hour window"}}`)

	handleGrokHealthForTest(svc, context.Background(), provider, http.StatusTooManyRequests,
		http.Header{"Retry-After": []string{"3600"}}, body)

	require.Zero(t, repo.tempUnschedCalls)
	require.WithinDuration(t, time.Now().Add(time.Hour), repo.lastRateLimitResetAt, 2*time.Second)
}

func TestHandleGrokProviderUpstreamError_EmptyOutputCoolsProvider(t *testing.T) {
	repo := &grokQuotaProviderRepo{}
	svc := newGrokHealthForTest(repo, nil)
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, ID: 9102, Platform: capability.PlatformGrok, Type: capability.ProviderTypeOAuth}}
	before := time.Now()

	handleGrokHealthForTest(svc,
		context.Background(), provider, http.StatusBadGateway, nil,
		[]byte(`empty model output: no content/tool_calls`),
	)

	require.Equal(t, 1, repo.tempUnschedCalls)
	require.Equal(t, "grok empty model output", repo.lastTempUnschedReason)
	require.WithinDuration(t, before.Add(4*time.Minute), repo.lastTempUnschedUntil, time.Second)
}

func TestHandleGrokProviderUpstreamError_MultiAgentCapacityBlocksOnlyThatModel(t *testing.T) {
	repo := &grokQuotaProviderRepo{}
	svc := newGrokHealthForTest(repo, nil)
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, ID: 9120, Platform: capability.PlatformGrok, Type: capability.ProviderTypeOAuth}}
	ctx := context.Background()

	handleGrokHealthWithTeamForTest(svc, "grok-4.20-multi-agent-0309",
		ctx, provider, http.StatusBadGateway, nil,
		[]byte(`{"error":{"message":"engine_overloaded"}}`),
	)

	require.Zero(t, repo.tempUnschedCalls)
	require.True(t, providercore.IsGrokModelQuotaBlocked(provider.Record.ID, "grok-4.20-multi-agent-0309", time.Now()))
	require.False(t, providercore.IsGrokModelQuotaBlocked(provider.Record.ID, "grok-4.5", time.Now()))
}

func TestHandleGrokProviderUpstreamError_CapacityNeverCoolsProvider(t *testing.T) {
	repo := &grokQuotaProviderRepo{}
	svc := newGrokHealthForTest(repo, nil)
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, ID: 9121, Platform: capability.PlatformGrok, Type: capability.ProviderTypeOAuth}}
	ctx := context.Background()

	handleGrokHealthWithTeamForTest(svc, "grok-4.6", ctx, provider, http.StatusTooManyRequests, nil,
		[]byte(`{"error":{"message":"The model is currently at capacity due to high demand"}}`))

	require.Zero(t, repo.tempUnschedCalls)
	require.False(t, svc.Runtime.Blocked(provider.Record.ID, func() string { return providercore.RefreshCredentialIdentity(provider.View()) }))
}

func TestHandleGrokProviderUpstreamError_FreeUsageDoesNotCoolPoolMode(t *testing.T) {
	repo := &grokQuotaProviderRepo{}
	svc := newGrokHealthForTest(repo, nil)
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: 9103,
			Platform: capability.PlatformGrok,
			Type:     capability.ProviderTypeAPIKey,
			Credentials: map[string]any{
				"pool_mode": true,
			},
		},
	}
	body := []byte(`{"error":{"code":"subscription:free-usage-exhausted","message":"free usage exhausted"}}`)

	handleGrokHealthForTest(svc, context.Background(), provider, http.StatusBadRequest, nil, body)

	require.Zero(t, repo.tempUnschedCalls)
	require.False(t, svc.Runtime.Blocked(provider.Record.ID, func() string { return providercore.RefreshCredentialIdentity(provider.View()) }))
}

func TestHandleGrokProviderUpstreamError_ContentPolicyStillNoMutation(t *testing.T) {
	repo := &grokQuotaProviderRepo{}
	svc := newGrokHealthForTest(repo, nil)
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, ID: 9104, Platform: capability.PlatformGrok, Type: capability.ProviderTypeOAuth}}
	body := []byte(`{"error":{"code":"new_sensitive","message":"text is sensitive"}}`)

	handleGrokHealthForTest(svc, context.Background(), provider, http.StatusForbidden, nil, body)

	require.Zero(t, repo.tempUnschedCalls)
}

func TestHandleGrokProviderUpstreamError_Entitlement403Unchanged(t *testing.T) {
	repo := &grokQuotaProviderRepo{}
	svc := newGrokHealthForTest(repo, nil)
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, ID: 9105, Platform: capability.PlatformGrok, Type: capability.ProviderTypeOAuth}}
	before := time.Now()

	handleGrokHealthForTest(svc,
		context.Background(), provider, http.StatusForbidden, nil,
		[]byte(`{"error":{"message":"subscription required"}}`),
	)

	require.Equal(t, 1, repo.tempUnschedCalls)
	require.Equal(t, "grok access or entitlement denied", repo.lastTempUnschedReason)
	require.Greater(t, repo.lastTempUnschedUntil, before.Add(29*time.Minute))
	require.Less(t, repo.lastTempUnschedUntil, before.Add(31*time.Minute))
}
