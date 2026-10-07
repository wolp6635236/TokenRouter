package googleforward_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/provider/googleforward"
	gatewaytestkit "github.com/TokenFlux/TokenRouter/internal/gateway/testkit"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestShouldFailoverGeminiUpstreamError 检查 ErrorPolicyNone 时是否触发故障转移。

func TestShouldFailoverGeminiUpstreamError(t *testing.T) {
	svc := newGeminiFixture(geminiDependencies{})

	tests := []struct {
		name       string
		statusCode int
		expected   bool
	}{
		{"401_failover", 401, true},

		{"403_failover", 403, true},

		{"429_failover", 429, true},

		{"529_failover", 529, true},

		{"500_failover", 500, true},

		{"502_failover", 502, true},

		{"503_failover", 503, true},

		{"400_no_failover", 400, false},

		{"404_no_failover", 404, false},

		{"422_no_failover", 422, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := googleforward.GeminiFailoverForTest(svc, tt.statusCode)
			require.Equal(t, tt.expected, got)
		})
	}
}

// TestCheckErrorPolicy_GeminiProviders 检查 Gemini API Key 提供商的错误策略。

func TestCheckErrorPolicy_GeminiProviders(t *testing.T) {
	tests := []struct {
		name       string
		provider   *gatewayprovider.ExecutionProvider
		statusCode int
		body       []byte
		expected   providercore.ErrorPolicyResult
	}{
		{
			name: "gemini_apikey_custom_codes_hit",

			provider: &gatewayprovider.ExecutionProvider{
				Record: providercore.Record{
					LoadLocation: time.LoadLocation,
					ID:           100,

					Type: capability.ProviderTypeAPIKey,

					Platform: capability.PlatformGemini,

					Credentials: map[string]any{
						"custom_error_codes_enabled": true,
						"custom_error_codes":         []any{float64(429), float64(500)},
					},
				},
			},

			statusCode: 429,

			body: []byte(`{"error":"rate limited"}`),

			expected: providercore.ErrorPolicyCustomMatched,
		},

		{
			name: "gemini_apikey_custom_codes_miss",

			provider: &gatewayprovider.ExecutionProvider{
				Record: providercore.Record{
					LoadLocation: time.LoadLocation,
					ID:           101,

					Type: capability.ProviderTypeAPIKey,

					Platform: capability.PlatformGemini,

					Credentials: map[string]any{
						"custom_error_codes_enabled": true,
						"custom_error_codes":         []any{float64(429)},
					},
				},
			},

			statusCode: 500,

			body: []byte(`{"error":"internal"}`),

			expected: providercore.ErrorPolicyCustomSkipped,
		},

		{
			name: "gemini_apikey_no_custom_codes_returns_none",

			provider: &gatewayprovider.ExecutionProvider{
				Record: providercore.Record{
					LoadLocation: time.LoadLocation,
					ID:           102,

					Type: capability.ProviderTypeAPIKey,

					Platform: capability.PlatformGemini,
				},
			},

			statusCode: 500,

			body: []byte(`{"error":"internal"}`),

			expected: providercore.ErrorPolicyNone,
		},

		{
			name: "gemini_apikey_temp_unschedulable_hit",

			provider: &gatewayprovider.ExecutionProvider{
				Record: providercore.Record{
					LoadLocation: time.LoadLocation,
					ID:           103,

					Type: capability.ProviderTypeAPIKey,

					Platform: capability.PlatformGemini,

					Credentials: map[string]any{
						"temp_unschedulable_enabled": true,

						"temp_unschedulable_rules": []any{
							map[string]any{
								"error_code": float64(503),

								"keywords": []any{"overloaded"},

								"duration_minutes": float64(10),
							},
						},
					},
				},
			},

			statusCode: 503,

			body: []byte(`overloaded service`),

			expected: providercore.ErrorPolicyTempUnscheduled,
		},

		{
			name: "gemini_apikey_temp_unschedulable_401_second_hit_returns_none",

			provider: &gatewayprovider.ExecutionProvider{
				Record: providercore.Record{
					LoadLocation: time.LoadLocation,
					ID:           105,

					Type: capability.ProviderTypeAPIKey,

					Platform: capability.PlatformGemini,

					TempUnschedulableReason: `{"status_code":401,"until_unix":1735689600}`,

					Credentials: map[string]any{
						"temp_unschedulable_enabled": true,

						"temp_unschedulable_rules": []any{
							map[string]any{
								"error_code": float64(401),

								"keywords": []any{"unauthorized"},

								"duration_minutes": float64(10),
							},
						},
					},
				},
			},

			statusCode: 401,

			body: []byte(`unauthorized`),

			expected: providercore.ErrorPolicyNone,
		},

		{
			name: "gemini_custom_codes_override_temp_unschedulable",

			provider: &gatewayprovider.ExecutionProvider{
				Record: providercore.Record{
					LoadLocation: time.LoadLocation,
					ID:           104,

					Type: capability.ProviderTypeAPIKey,

					Platform: capability.PlatformGemini,

					Credentials: map[string]any{
						"custom_error_codes_enabled": true,

						"custom_error_codes": []any{float64(503)},

						"temp_unschedulable_enabled": true,

						"temp_unschedulable_rules": []any{
							map[string]any{
								"error_code": float64(503),

								"keywords": []any{"overloaded"},

								"duration_minutes": float64(10),
							},
						},
					},
				},
			},

			statusCode: 503,

			body: []byte(`overloaded`),

			expected: providercore.ErrorPolicyCustomMatched, // custom codes take precedence

		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &gatewaytestkit.ErrorPolicyStore{}
			svc := newUpstreamHealthForTest(repo, &googleforward.Options{}, nil, providercore.HealthOptions{}, nil)

			result := svc.CheckErrorPolicy(context.Background(), gatewayprovider.ExecutionRecord(tt.provider), gatewayprovider.HealthObservationFromContext(context.Background(), tt.statusCode, nil, tt.body, nil))
			require.Equal(t, tt.expected, result)
		})
	}
}

// TestGeminiErrorPolicyIntegration 按兼容与 Gemini 协议入口的调用顺序，检查各类错误策略结果。

func TestGeminiErrorPolicyIntegration(t *testing.T) {
	tests := []struct {
		name                 string
		provider             *gatewayprovider.ExecutionProvider
		statusCode           int
		respBody             []byte
		expectFailover       bool // expect UpstreamFailoverError
		expectHandleError    bool // expect handleGeminiUpstreamError to be called
		expectShouldFailover bool // for None path, whether shouldFailover triggers
		expectModelScope     string
	}{
		{
			name: "custom_codes_matched_429_failover",

			provider: &gatewayprovider.ExecutionProvider{
				Record: providercore.Record{
					LoadLocation: time.LoadLocation,
					ID:           200,

					Type: capability.ProviderTypeAPIKey,

					Platform: capability.PlatformGemini,

					Credentials: map[string]any{
						"custom_error_codes_enabled": true,
						"custom_error_codes":         []any{float64(429)},
					},
				},
			},

			statusCode: 429,

			respBody: []byte(`{"error":"rate limited"}`),

			expectFailover: true,

			expectHandleError: true,
		},

		{
			name: "custom_codes_skipped_500_no_failover",

			provider: &gatewayprovider.ExecutionProvider{
				Record: providercore.Record{
					LoadLocation: time.LoadLocation,
					ID:           201,

					Type: capability.ProviderTypeAPIKey,

					Platform: capability.PlatformGemini,

					Credentials: map[string]any{
						"custom_error_codes_enabled": true,
						"custom_error_codes":         []any{float64(429)},
					},
				},
			},

			statusCode: 500,

			respBody: []byte(`{"error":"internal"}`),

			expectFailover: false,

			expectHandleError: false,
		},

		{
			name: "temp_unschedulable_matched_failover",

			provider: &gatewayprovider.ExecutionProvider{
				Record: providercore.Record{
					LoadLocation: time.LoadLocation,
					ID:           202,

					Type: capability.ProviderTypeAPIKey,

					Platform: capability.PlatformGemini,

					Credentials: map[string]any{
						"temp_unschedulable_enabled": true,

						"temp_unschedulable_rules": []any{
							map[string]any{
								"error_code": float64(503),

								"keywords": []any{"overloaded"},

								"duration_minutes": float64(10),
							},
						},
					},
				},
			},

			statusCode: 503,

			respBody: []byte(`overloaded`),

			expectFailover: true,

			expectHandleError: false,

			expectModelScope: "gemini-2.5-pro",
		},

		{
			name: "no_policy_429_failover_via_shouldFailover",

			provider: &gatewayprovider.ExecutionProvider{
				Record: providercore.Record{
					LoadLocation: time.LoadLocation,
					ID:           203,

					Type: capability.ProviderTypeAPIKey,

					Platform: capability.PlatformGemini,
				},
			},

			statusCode: 429,

			respBody: []byte(`{"error":"rate limited"}`),

			expectFailover: true,

			expectHandleError: true,

			expectShouldFailover: true,
		},

		{
			name: "no_policy_400_no_failover",

			provider: &gatewayprovider.ExecutionProvider{
				Record: providercore.Record{
					LoadLocation: time.LoadLocation,
					ID:           204,

					Type: capability.ProviderTypeAPIKey,

					Platform: capability.PlatformGemini,
				},
			},

			statusCode: 400,

			respBody: []byte(`{"error":"bad request"}`),

			expectFailover: false,

			expectHandleError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &geminiErrorPolicyRepo{}
			rlSvc := newUpstreamHealthForTest(repo, &googleforward.Options{}, nil, providercore.HealthOptions{}, nil)

			svc := newGeminiFixture(geminiDependencies{
				providerRepo:   repo,
				healthObserver: rlSvc,
			})

			writer := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(writer)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

			// Simulate the Claude compat error handling path (same logic as native).
			// This mirrors the inline switch in handleClaudeCompat.
			var handleErrorCalled bool
			var gotFailover bool

			ctx := context.Background()
			statusCode := tt.statusCode
			respBody := tt.respBody
			provider := tt.provider
			headers := http.Header{}

			if svc.Health != nil {
				policy := svc.Health.CheckErrorPolicy(ctx, gatewayprovider.ExecutionRecord(provider), gatewayprovider.HealthObservationFromContext(ctx, statusCode, nil, respBody, []string{"gemini-2.5-pro"}))
				switch policy {
				case providercore.ErrorPolicyCustomSkipped:
					// Skipped → return error directly (no handleGeminiUpstreamError, no failover)
					gotFailover = false
					handleErrorCalled = false
					goto verify
				case providercore.ErrorPolicyCustomMatched:
					svc.Errors.Observe(ctx, gatewayprovider.ExecutionRecord(provider), statusCode, headers, respBody, gatewayprovider.HealthObservationFromContext(ctx, statusCode, headers, respBody, nil))
					handleErrorCalled = true
					gotFailover = true
					goto verify
				case providercore.ErrorPolicyTempUnscheduled:
					handleErrorCalled = false
					gotFailover = true
					goto verify
				}
			}

			// ErrorPolicyNone → original logic
			svc.Errors.Observe(ctx, gatewayprovider.ExecutionRecord(provider), statusCode, headers, respBody, gatewayprovider.HealthObservationFromContext(ctx, statusCode, headers, respBody, nil))
			handleErrorCalled = true
			if googleforward.GeminiFailoverForTest(svc, statusCode) {
				gotFailover = true
			}

		verify:
			require.Equal(t, tt.expectFailover, gotFailover, "failover mismatch")
			require.Equal(t, tt.expectHandleError, handleErrorCalled, "handleGeminiUpstreamError call mismatch")
			if tt.expectModelScope != "" {
				require.Equal(t, 1, repo.setModelRateLimitedCalls)
				require.Equal(t, tt.expectModelScope, repo.lastModelScope)
				require.Zero(t, repo.setTempCalls)
				require.Zero(t, repo.setRateLimitedCalls, "model temp rule must not be widened into a provider rate limit")
			}

			if tt.expectShouldFailover {
				require.True(t, googleforward.GeminiFailoverForTest(svc, statusCode),
					"shouldFailoverGeminiUpstreamError should return true for status %d", statusCode)
			}
		})
	}
}

// TestGeminiErrorPolicy_NilRateLimitService 检查健康观测器为 nil 时的错误处理。

func TestGeminiErrorPolicy_NilRateLimitService(t *testing.T) {
	svc := newGeminiFixture(geminiDependencies{
		healthObserver: nil,
	})

	// When healthObserver is nil, error policy is skipped → falls through to
	// shouldFailoverGeminiUpstreamError (original logic).
	// Verify this doesn't panic and follows expected behavior.

	ctx := context.Background()
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation,
			ID:           300,

			Type: capability.ProviderTypeAPIKey,

			Platform: capability.PlatformGemini,

			Credentials: map[string]any{
				"custom_error_codes_enabled": true,
				"custom_error_codes":         []any{float64(429)},
			},
		},
	}

	// The nil check should prevent CheckErrorPolicy from being called
	if svc.Health != nil {
		t.Fatal("healthObserver should be nil for this test")
	}

	// shouldFailoverGeminiUpstreamError still works
	require.True(t, googleforward.GeminiFailoverForTest(svc, 429))
	require.False(t, googleforward.GeminiFailoverForTest(svc, 400))

	// handleGeminiUpstreamError should not panic with nil healthObserver
	require.NotPanics(t, func() {
		svc.Errors.Observe(ctx, gatewayprovider.ExecutionRecord(provider), 500, http.Header{}, []byte(`error`), gatewayprovider.HealthObservationFromContext(ctx, 500, http.Header{}, []byte(`error`), nil))
	})
}

// TestHandleGeminiUpstreamError_GoogleOneCapacityExhaustedUsesTierCooldown 检查 Google One 容量耗尽时的层级冷却。

func TestHandleGeminiUpstreamError_GoogleOneCapacityExhaustedUsesTierCooldown(t *testing.T) {
	repo := &rateLimit429ProviderRepoStub{}
	quotaSvc := providercore.NewGeminiQuotaService(providercore.GeminiQuotaOptions{})
	rlSvc := newUpstreamHealthForTest(repo, &googleforward.Options{}, nil, providercore.HealthOptions{}, nil)

	svc := newGeminiFixture(geminiDependencies{
		quotaPrecheck: providercore.NewGeminiPrecheck(quotaSvc, nil, providercore.GeminiPrecheckOptions{Now: time.Now, Location: geminiQuotaLocation()}),

		providerRepo: repo,

		healthObserver: rlSvc,
	})

	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation,
			ID:           511,

			Platform: capability.PlatformGemini,

			Type: capability.ProviderTypeOAuth,

			Credentials: map[string]any{
				"oauth_type": "google_one",
				"tier_id":    "google_ai_pro",
			},
		},
	}
	body := []byte(`{"error":{"code":429,"details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","domain":"cloudcode-pa.googleapis.com","metadata":{"model":"gemini-3.1-pro-preview"},"reason":"MODEL_CAPACITY_EXHAUSTED"}],"message":"No capacity available for model gemini-3.1-pro-preview on the server","status":"RESOURCE_EXHAUSTED"}}`)

	before := time.Now()
	svc.Errors.Observe(context.Background(), gatewayprovider.ExecutionRecord(provider), http.StatusTooManyRequests, http.Header{}, body, gatewayprovider.HealthObservationFromContext(context.Background(), http.StatusTooManyRequests, http.Header{}, body, nil))
	after := time.Now()

	require.Equal(t, 1, repo.rateLimitCalls)
	require.Equal(t, int64(511), repo.lastRateLimitID)
	require.WithinDuration(t, before.Add(5*time.Minute), repo.lastRateLimitReset, 2*time.Second)
	require.True(t, repo.lastRateLimitReset.After(before))
	require.True(t, repo.lastRateLimitReset.Before(after.Add(5*time.Minute).Add(2*time.Second)))
}

func TestHandleGeminiUpstreamError_ThirdPartyAPIKeyIgnoresOfficialQuotaMessage(t *testing.T) {
	repo := &rateLimit429ProviderRepoStub{}
	quotaSvc := providercore.NewGeminiQuotaService(providercore.GeminiQuotaOptions{})
	rlSvc := newUpstreamHealthForTest(repo, &googleforward.Options{}, nil, providercore.HealthOptions{}, nil)

	svc := newGeminiFixture(geminiDependencies{
		quotaPrecheck: providercore.NewGeminiPrecheck(quotaSvc, nil, providercore.GeminiPrecheckOptions{Now: time.Now, Location: geminiQuotaLocation()}),

		providerRepo: repo,

		healthObserver: rlSvc,
	})
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation,
			ID:           512,

			Platform: capability.PlatformGemini,

			Type: capability.ProviderTypeAPIKey,

			Credentials: map[string]any{
				providercore.GeminiProviderTypeCredentialKey: providercore.GeminiProviderTypeThirdParty,
			},
		},
	}

	before := time.Now()
	svc.Errors.Observe(context.Background(), gatewayprovider.ExecutionRecord(provider), http.StatusTooManyRequests, http.Header{}, []byte(`{"error":{"code":429,"message":"Quota exceeded: 20 requests per day"}}`), gatewayprovider.HealthObservationFromContext(context.Background(), http.StatusTooManyRequests, http.Header{}, []byte(`{"error":{"code":429,"message":"Quota exceeded: 20 requests per day"}}`), nil))
	after := time.Now()

	require.Equal(t, 1, repo.rateLimitCalls)
	require.Equal(t, int64(512), repo.lastRateLimitID)
	require.WithinDuration(t, before.Add(5*time.Minute), repo.lastRateLimitReset, 2*time.Second)
	require.True(t, repo.lastRateLimitReset.After(before))
	require.True(t, repo.lastRateLimitReset.Before(after.Add(5*time.Minute).Add(2*time.Second)))
}

// TestGeminiPoolMode429BypassesLocalRateLimit 验证池模式不再被当作自定义未命中，
// 也不会继续执行 Gemini 默认 429 限流写入。
func TestGeminiPoolMode429BypassesLocalRateLimit(t *testing.T) {
	repo := &geminiErrorPolicyRepo{}
	healthObserver := newUpstreamHealthForTest(repo, &googleforward.Options{}, nil, providercore.HealthOptions{}, nil)

	svc := newGeminiFixture(geminiDependencies{providerRepo: repo, healthObserver: healthObserver})
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation,
			ID:           520,

			Type: capability.ProviderTypeAPIKey,

			Platform: capability.PlatformGemini,

			Credentials: map[string]any{
				"pool_mode": true,
			},
		},
	}

	decision := googleforward.GeminiPolicyForTest(svc, context.Background(), provider, http.StatusTooManyRequests, http.Header{}, []byte(`{"error":{"message":"rate limited"}}`), "gemini-2.5-pro")

	require.Equal(t, providercore.ErrorPolicyPoolBypassed, decision.Policy)
	require.True(t, decision.RetryableOnSameProvider(gatewayprovider.ExecutionErrorPolicy(provider), http.StatusTooManyRequests))
	require.Zero(t, repo.setRateLimitedCalls)
	require.Zero(t, repo.setTempCalls)
	require.Zero(t, repo.setErrorCalls)
}

func TestHandleGeminiUpstreamError_PoolMode429SkipsProviderLimit(t *testing.T) {
	body := []byte(`{"error":{"code":429,"message":"capacity exhausted"}}`)
	tests := []struct {
		name      string
		provider  *gatewayprovider.ExecutionProvider
		wantCalls int
	}{
		{
			name: "池模式跳过默认提供商限流",

			provider: &gatewayprovider.ExecutionProvider{
				Record: providercore.Record{
					LoadLocation: time.LoadLocation,
					ID:           530,
					Type:         capability.ProviderTypeAPIKey,
					Platform:     capability.PlatformGemini,

					Credentials: map[string]any{"pool_mode": true},
				},
			},
		},

		{
			name: "自定义错误码命中优先于池模式",

			provider: &gatewayprovider.ExecutionProvider{
				Record: providercore.Record{
					LoadLocation: time.LoadLocation,
					ID:           531,
					Type:         capability.ProviderTypeAPIKey,
					Platform:     capability.PlatformGemini,

					Credentials: map[string]any{
						"pool_mode": true,

						"custom_error_codes_enabled": true,

						"custom_error_codes": []any{float64(http.StatusTooManyRequests)},
					},
				},
			},

			wantCalls: 1,
		},

		{
			name: "自定义错误码未命中跳过提供商限流",

			provider: &gatewayprovider.ExecutionProvider{
				Record: providercore.Record{
					LoadLocation: time.LoadLocation,
					ID:           532,
					Type:         capability.ProviderTypeAPIKey,
					Platform:     capability.PlatformGemini,

					Credentials: map[string]any{
						"pool_mode": true,

						"custom_error_codes_enabled": true,

						"custom_error_codes": []any{float64(http.StatusInternalServerError)},
					},
				},
			},
		},

		{
			name: "普通提供商保留默认限流",

			provider: &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, ID: 533, Type: capability.ProviderTypeAPIKey, Platform: capability.PlatformGemini}},

			wantCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &rateLimit429ProviderRepoStub{}
			svc := newGeminiFixture(geminiDependencies{providerRepo: repo})

			svc.Errors.Observe(context.Background(), gatewayprovider.ExecutionRecord(tt.provider), http.StatusTooManyRequests, http.Header{}, body, gatewayprovider.HealthObservationFromContext(context.Background(), http.StatusTooManyRequests, http.Header{}, body, nil))

			require.Equal(t, tt.wantCalls, repo.rateLimitCalls)
		})
	}
}

// TestGeminiCustomNonFailoverStatusStopsScheduling 验证非默认故障转移状态也会执行
// 管理员显式策略并写入提供商错误。
func TestGeminiCustomNonFailoverStatusStopsScheduling(t *testing.T) {
	repo := &geminiErrorPolicyRepo{}
	healthObserver := newUpstreamHealthForTest(repo, &googleforward.Options{}, nil, providercore.HealthOptions{}, nil)

	svc := newGeminiFixture(geminiDependencies{providerRepo: repo, healthObserver: healthObserver})
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation,
			ID:           521,

			Type: capability.ProviderTypeAPIKey,

			Platform: capability.PlatformGemini,

			Credentials: map[string]any{
				"pool_mode": true,

				"custom_error_codes_enabled": true,

				"custom_error_codes": []any{float64(http.StatusUnprocessableEntity)},
			},
		},
	}

	decision := googleforward.GeminiPolicyForTest(svc, context.Background(), provider, http.StatusUnprocessableEntity, http.Header{}, []byte(`{"error":{"message":"configured"}}`), "gemini-2.5-pro")

	require.Equal(t, providercore.ErrorPolicyCustomMatched, decision.Policy)
	require.True(t, decision.StopScheduling)
	require.False(t, decision.RetryableOnSameProvider(gatewayprovider.ExecutionErrorPolicy(provider), http.StatusUnprocessableEntity))
	require.Equal(t, 1, repo.setErrorCalls)
	require.Zero(t, repo.setRateLimitedCalls)
}

type geminiErrorPolicyRepo struct {
	gatewaytestkit.ErrorPolicyStore
	setErrorCalls            int
	setRateLimitedCalls      int
	setTempCalls             int
	setModelRateLimitedCalls int
	lastModelScope           string
}

func (r *geminiErrorPolicyRepo) SetError(_ context.Context, _ int64, _ string) error {
	r.setErrorCalls++
	return nil
}

func (r *geminiErrorPolicyRepo) SetRateLimited(_ context.Context, _ int64, _ time.Time) error {
	r.setRateLimitedCalls++
	return nil
}

func (r *geminiErrorPolicyRepo) SetTempUnschedulable(_ context.Context, _ int64, _ time.Time, _ string) error {
	r.setTempCalls++
	return nil
}

func (r *geminiErrorPolicyRepo) SetModelRateLimit(_ context.Context, _ int64, scope string, _ time.Time, _ ...string) error {
	r.setModelRateLimitedCalls++
	r.lastModelScope = scope
	return nil
}
