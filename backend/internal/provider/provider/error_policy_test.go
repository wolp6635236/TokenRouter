package provider

import (
	"context"
	"net/http"
	"testing"
	"time"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

func TestCheckErrorPolicy(t *testing.T) {
	tests := []struct {
		name       string
		provider   *providercore.Record
		statusCode int
		body       []byte
		expected   providercore.ErrorPolicyResult
	}{
		{
			name: "no_policy_oauth_returns_none",
			provider: &providercore.Record{
				ID:       1,
				Type:     capability.ProviderTypeOAuth,
				Platform: capability.PlatformAntigravity,
				// no custom error codes, no temp rules
			},
			statusCode: 500,
			body:       []byte(`"error"`),
			expected:   providercore.ErrorPolicyNone,
		},
		{
			name: "custom_error_codes_hit_returns_matched",
			provider: &providercore.Record{
				ID:       2,
				Type:     capability.ProviderTypeAPIKey,
				Platform: capability.PlatformAntigravity,
				Credentials: map[string]any{
					"custom_error_codes_enabled": true,
					"custom_error_codes":         []any{float64(429), float64(500)},
				},
			},
			statusCode: 500,
			body:       []byte(`"error"`),
			expected:   providercore.ErrorPolicyCustomMatched,
		},
		{
			name: "custom_error_codes_miss_returns_skipped",
			provider: &providercore.Record{
				ID:       3,
				Type:     capability.ProviderTypeAPIKey,
				Platform: capability.PlatformAntigravity,
				Credentials: map[string]any{
					"custom_error_codes_enabled": true,
					"custom_error_codes":         []any{float64(429), float64(500)},
				},
			},
			statusCode: 503,
			body:       []byte(`"error"`),
			expected:   providercore.ErrorPolicyCustomSkipped,
		},
		{
			name: "custom_error_codes_excluding_529_skip_global_cooldown",
			provider: &providercore.Record{
				ID:       33,
				Type:     capability.ProviderTypeAPIKey,
				Platform: capability.PlatformOpenAI,
				Credentials: map[string]any{
					"custom_error_codes_enabled": true,
					"custom_error_codes":         []any{float64(429)},
				},
			},
			statusCode: 529,
			body:       []byte(`{"error":{"message":"overloaded"}}`),
			expected:   providercore.ErrorPolicyCustomSkipped,
		},
		{
			name: "pool_mode_skips_global_529_cooldown",
			provider: &providercore.Record{
				ID:       34,
				Type:     capability.ProviderTypeAPIKey,
				Platform: capability.PlatformOpenAI,
				Credentials: map[string]any{
					"pool_mode": true,
				},
			},
			statusCode: 529,
			body:       []byte(`{"error":{"message":"overloaded"}}`),
			expected:   providercore.ErrorPolicyPoolBypassed,
		},
		{
			name: "ordinary_provider_uses_global_529_cooldown",
			provider: &providercore.Record{
				ID:       35,
				Type:     capability.ProviderTypeAPIKey,
				Platform: capability.PlatformOpenAI,
			},
			statusCode: 529,
			body:       []byte(`{"error":{"message":"overloaded"}}`),
			expected:   providercore.ErrorPolicyCustomMatched,
		},
		{
			name: "custom_error_codes_including_529_take_precedence",
			provider: &providercore.Record{
				ID:       36,
				Type:     capability.ProviderTypeAPIKey,
				Platform: capability.PlatformOpenAI,
				Credentials: map[string]any{
					"custom_error_codes_enabled": true,
					"custom_error_codes":         []any{float64(529)},
				},
			},
			statusCode: 529,
			body:       []byte(`{"error":{"message":"overloaded"}}`),
			expected:   providercore.ErrorPolicyCustomMatched,
		},
		{
			name: "temp_unschedulable_hit_returns_temp_unscheduled",
			provider: &providercore.Record{
				ID:       4,
				Type:     capability.ProviderTypeOAuth,
				Platform: capability.PlatformAntigravity,
				Credentials: map[string]any{
					"temp_unschedulable_enabled": true,
					"temp_unschedulable_rules": []any{
						map[string]any{
							"error_code":       float64(503),
							"keywords":         []any{"overloaded"},
							"duration_minutes": float64(10),
							"description":      "overloaded rule",
						},
					},
				},
			},
			statusCode: 503,
			body:       []byte(`overloaded service`),
			expected:   providercore.ErrorPolicyTempUnscheduled,
		},
		{
			name: "temp_unschedulable_401_first_hit_returns_temp_unscheduled",
			provider: &providercore.Record{
				ID:       14,
				Type:     capability.ProviderTypeOAuth,
				Platform: capability.PlatformAntigravity,
				Credentials: map[string]any{
					"temp_unschedulable_enabled": true,
					"temp_unschedulable_rules": []any{
						map[string]any{
							"error_code":       float64(401),
							"keywords":         []any{"unauthorized"},
							"duration_minutes": float64(10),
						},
					},
				},
			},
			statusCode: 401,
			body:       []byte(`unauthorized`),
			expected:   providercore.ErrorPolicyTempUnscheduled,
		},
		{
			// Antigravity 401 不走升级逻辑（由 applyErrorPolicy 的 temp_unschedulable_rules 自行控制），
			// second hit 仍然返回 TempUnscheduled。
			name: "temp_unschedulable_401_second_hit_antigravity_stays_temp",
			provider: &providercore.Record{
				ID:                      15,
				Type:                    capability.ProviderTypeOAuth,
				Platform:                capability.PlatformAntigravity,
				TempUnschedulableReason: `{"status_code":401,"until_unix":1735689600}`,
				Credentials: map[string]any{
					"temp_unschedulable_enabled": true,
					"temp_unschedulable_rules": []any{
						map[string]any{
							"error_code":       float64(401),
							"keywords":         []any{"unauthorized"},
							"duration_minutes": float64(10),
						},
					},
				},
			},
			statusCode: 401,
			body:       []byte(`unauthorized`),
			expected:   providercore.ErrorPolicyTempUnscheduled,
		},
		{
			name: "temp_unschedulable_body_miss_returns_none",
			provider: &providercore.Record{
				ID:       5,
				Type:     capability.ProviderTypeOAuth,
				Platform: capability.PlatformAntigravity,
				Credentials: map[string]any{
					"temp_unschedulable_enabled": true,
					"temp_unschedulable_rules": []any{
						map[string]any{
							"error_code":       float64(503),
							"keywords":         []any{"overloaded"},
							"duration_minutes": float64(10),
							"description":      "overloaded rule",
						},
					},
				},
			},
			statusCode: 503,
			body:       []byte(`random msg`),
			expected:   providercore.ErrorPolicyNone,
		},
		{
			name: "custom_error_codes_override_temp_unschedulable",
			provider: &providercore.Record{
				ID:       6,
				Type:     capability.ProviderTypeAPIKey,
				Platform: capability.PlatformAntigravity,
				Credentials: map[string]any{
					"custom_error_codes_enabled": true,
					"custom_error_codes":         []any{float64(503)},
					"temp_unschedulable_enabled": true,
					"temp_unschedulable_rules": []any{
						map[string]any{
							"error_code":       float64(503),
							"keywords":         []any{"overloaded"},
							"duration_minutes": float64(10),
							"description":      "overloaded rule",
						},
					},
				},
			},
			statusCode: 503,
			body:       []byte(`overloaded`),
			expected:   providercore.ErrorPolicyCustomMatched, // custom codes take precedence
		},
		{
			name: "pool_mode_custom_error_codes_hit_returns_matched",
			provider: &providercore.Record{
				ID:       7,
				Type:     capability.ProviderTypeAPIKey,
				Platform: capability.PlatformOpenAI,
				Credentials: map[string]any{
					"pool_mode":                  true,
					"custom_error_codes_enabled": true,
					"custom_error_codes":         []any{float64(401), float64(403)},
				},
			},
			statusCode: 401,
			body:       []byte(`unauthorized`),
			expected:   providercore.ErrorPolicyCustomMatched,
		},
		{
			name: "pool_mode_without_custom_error_codes_returns_bypassed",
			provider: &providercore.Record{
				ID:       8,
				Type:     capability.ProviderTypeAPIKey,
				Platform: capability.PlatformOpenAI,
				Credentials: map[string]any{
					"pool_mode": true,
				},
			},
			statusCode: 401,
			body:       []byte(`unauthorized`),
			expected:   providercore.ErrorPolicyPoolBypassed,
		},
		{
			name: "pool_mode_temp_unschedulable_hit_returns_temp_unscheduled",
			provider: &providercore.Record{
				ID:       9,
				Type:     capability.ProviderTypeAPIKey,
				Platform: capability.PlatformOpenAI,
				Credentials: map[string]any{
					"pool_mode":                  true,
					"temp_unschedulable_enabled": true,
					"temp_unschedulable_rules": []any{
						map[string]any{
							"error_code":       float64(http.StatusServiceUnavailable),
							"keywords":         []any{"unavailable"},
							"duration_minutes": float64(30),
						},
					},
				},
			},
			statusCode: http.StatusServiceUnavailable,
			body:       []byte(`Service temporarily unavailable`),
			expected:   providercore.ErrorPolicyTempUnscheduled,
		},
		{
			name: "pool_mode_repeated_401_explicit_rule_stays_temp_unscheduled",
			provider: &providercore.Record{
				ID:                      11,
				Type:                    capability.ProviderTypeAPIKey,
				Platform:                capability.PlatformOpenAI,
				TempUnschedulableReason: `{"status_code":401,"until_unix":1735689600}`,
				Credentials: map[string]any{
					"pool_mode":                  true,
					"temp_unschedulable_enabled": true,
					"temp_unschedulable_rules": []any{
						map[string]any{
							"error_code":       float64(http.StatusUnauthorized),
							"keywords":         []any{"unauthorized"},
							"duration_minutes": float64(30),
						},
					},
				},
			},
			statusCode: http.StatusUnauthorized,
			body:       []byte(`unauthorized`),
			expected:   providercore.ErrorPolicyTempUnscheduled,
		},
		{
			name: "pool_mode_temp_unschedulable_miss_returns_bypassed",
			provider: &providercore.Record{
				ID:       10,
				Type:     capability.ProviderTypeAPIKey,
				Platform: capability.PlatformOpenAI,
				Credentials: map[string]any{
					"pool_mode":                  true,
					"temp_unschedulable_enabled": true,
					"temp_unschedulable_rules": []any{
						map[string]any{
							"error_code":       float64(http.StatusServiceUnavailable),
							"keywords":         []any{"maintenance"},
							"duration_minutes": float64(30),
						},
					},
				},
			},
			statusCode: http.StatusServiceUnavailable,
			body:       []byte(`Service temporarily unavailable`),
			expected:   providercore.ErrorPolicyPoolBypassed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &errorPolicyRepoStub{}
			svc := newErrorPolicyObserver(repo)

			result := svc.CheckErrorPolicy(context.Background(), tt.provider, HealthObservation{Status: tt.statusCode, Body: tt.body})
			require.Equal(t, tt.expected, result, "unexpected ErrorPolicyResult")
		})
	}
}

func TestHandleUpstreamError_PoolModePolicies(t *testing.T) {
	t.Run("pool_mode_without_custom_error_codes_still_skips", func(t *testing.T) {
		repo := &errorPolicyRepoStub{}
		svc := newErrorPolicyObserver(repo)
		provider := &providercore.Record{
			ID:       30,
			Type:     capability.ProviderTypeAPIKey,
			Platform: capability.PlatformOpenAI,
			Credentials: map[string]any{
				"pool_mode": true,
			},
		}

		shouldDisable := svc.ApplyUpstreamError(context.Background(), provider, HealthObservation{Status: 401, Headers: http.Header{}, Body: []byte("unauthorized")}).StopScheduling

		require.False(t, shouldDisable)
		require.Equal(t, 0, repo.setErrCalls)
		require.Equal(t, 0, repo.tempCalls)
	})

	t.Run("pool_mode_with_custom_error_codes_uses_local_error_policy", func(t *testing.T) {
		repo := &errorPolicyRepoStub{}
		svc := newErrorPolicyObserver(repo)
		provider := &providercore.Record{
			ID:       31,
			Type:     capability.ProviderTypeAPIKey,
			Platform: capability.PlatformOpenAI,
			Credentials: map[string]any{
				"pool_mode":                  true,
				"custom_error_codes_enabled": true,
				"custom_error_codes":         []any{float64(401)},
			},
		}

		shouldDisable := svc.ApplyUpstreamError(context.Background(), provider, HealthObservation{Status: 401, Headers: http.Header{}, Body: []byte("unauthorized")}).StopScheduling

		require.True(t, shouldDisable)
		require.Equal(t, 1, repo.setErrCalls)
		require.Equal(t, 0, repo.tempCalls)
	})

	t.Run("pool_mode_explicit_temp_rule_stops_scheduling", func(t *testing.T) {
		repo := &errorPolicyRepoStub{}
		svc := newErrorPolicyObserver(repo)
		provider := &providercore.Record{
			ID:       32,
			Type:     capability.ProviderTypeAPIKey,
			Platform: capability.PlatformOpenAI,
			Credentials: map[string]any{
				"pool_mode":                  true,
				"temp_unschedulable_enabled": true,
				"temp_unschedulable_rules": []any{
					map[string]any{
						"error_code":       float64(http.StatusServiceUnavailable),
						"keywords":         []any{"unavailable"},
						"duration_minutes": float64(30),
					},
				},
			},
		}

		shouldDisable := svc.ApplyUpstreamError(context.Background(), provider, HealthObservation{Status: http.StatusServiceUnavailable, Headers: http.Header{}, Body: []byte("Service temporarily unavailable")}).StopScheduling

		require.True(t, shouldDisable)
		require.Equal(t, 0, repo.setErrCalls)
		require.Equal(t, 1, repo.tempCalls)
	})

	t.Run("pool_mode_temp_rule_miss_still_skips", func(t *testing.T) {
		repo := &errorPolicyRepoStub{}
		svc := newErrorPolicyObserver(repo)
		provider := &providercore.Record{
			ID:       33,
			Type:     capability.ProviderTypeAPIKey,
			Platform: capability.PlatformOpenAI,
			Credentials: map[string]any{
				"pool_mode":                  true,
				"temp_unschedulable_enabled": true,
				"temp_unschedulable_rules": []any{
					map[string]any{
						"error_code":       float64(http.StatusServiceUnavailable),
						"keywords":         []any{"maintenance"},
						"duration_minutes": float64(30),
					},
				},
			},
		}

		shouldDisable := svc.ApplyUpstreamError(context.Background(), provider, HealthObservation{Status: http.StatusServiceUnavailable, Headers: http.Header{}, Body: []byte("Service temporarily unavailable")}).StopScheduling

		require.False(t, shouldDisable)
		require.Equal(t, 0, repo.setErrCalls)
		require.Equal(t, 0, repo.tempCalls)
	})
}

// TestHandleUpstreamError_CustomCodesAlwaysStopScheduling 验证自定义错误码不会再被
// 400、429、529 的内置分支覆盖。
func TestHandleUpstreamError_CustomCodesAlwaysStopScheduling(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
	}{
		{name: "bad_request", statusCode: http.StatusBadRequest},
		{name: "rate_limit", statusCode: http.StatusTooManyRequests},
		{name: "overloaded", statusCode: 529},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &errorPolicyRepoStub{}
			svc := newErrorPolicyObserver(repo)
			provider := &providercore.Record{
				ID:       int64(1000 + tt.statusCode),
				Type:     capability.ProviderTypeAPIKey,
				Platform: capability.PlatformOpenAI,
				Credentials: map[string]any{
					"pool_mode":                  true,
					"custom_error_codes_enabled": true,
					"custom_error_codes":         []any{float64(tt.statusCode)},
				},
			}

			decision := svc.ApplyUpstreamError(context.Background(), provider, HealthObservation{Status: tt.statusCode, Headers: http.Header{}, Body: []byte(`{"error":{"message":"configured failure"}}`)})

			require.Equal(t, providercore.ErrorPolicyCustomMatched, decision.Policy)
			require.True(t, decision.StopScheduling)
			require.False(t, decision.RetryableOnSameProvider(provider, tt.statusCode))
			require.Equal(t, 1, repo.setErrCalls)
			require.Equal(t, 0, repo.tempCalls)
		})
	}
}

// TestUpstreamErrorDecision_PoolRetryStatusPromotesFailover 验证管理员配置的池模式
// 重试状态码可以提升非默认故障转移错误，同时不会写入本地状态。
func TestUpstreamErrorDecision_PoolRetryStatusPromotesFailover(t *testing.T) {
	repo := &errorPolicyRepoStub{}
	svc := newErrorPolicyObserver(repo)
	provider := &providercore.Record{
		ID:       20422,
		Type:     capability.ProviderTypeAPIKey,
		Platform: capability.PlatformOpenAI,
		Credentials: map[string]any{
			"pool_mode":                    true,
			"pool_mode_retry_status_codes": []any{float64(http.StatusUnprocessableEntity)},
		},
	}

	decision := svc.ApplyUpstreamError(context.Background(), provider, HealthObservation{Status: http.StatusUnprocessableEntity, Headers: http.Header{}, Body: []byte(`{"error":{"message":"unprocessable"}}`)})

	require.Equal(t, providercore.ErrorPolicyPoolBypassed, decision.Policy)
	require.True(t, decision.ShouldFailover(provider, http.StatusUnprocessableEntity, false))
	require.True(t, decision.RetryableOnSameProvider(provider, http.StatusUnprocessableEntity))
	require.Zero(t, repo.setErrCalls)
	require.Zero(t, repo.tempCalls)
	require.Empty(t, repo.modelRateLimitCalls)
}

// TestUpstreamErrorDecision_UsesSeparateEntryDefaults 检查普通提供商使用各入口的默认处理规则，
// 池模式使用平台错误分类，配置的错误策略优先。
func TestUpstreamErrorDecision_UsesSeparateEntryDefaults(t *testing.T) {
	provider := &providercore.Record{
		ID:          20423,
		Type:        capability.ProviderTypeAPIKey,
		Credentials: map[string]any{"pool_mode": true},
	}

	require.False(t, (providercore.UpstreamErrorDecision{Policy: providercore.ErrorPolicyNone}).ShouldFailoverWithDefaults(provider, http.StatusBadGateway, false, true))
	require.True(t, (providercore.UpstreamErrorDecision{Policy: providercore.ErrorPolicyPoolBypassed}).ShouldFailoverWithDefaults(provider, http.StatusBadGateway, false, true))
	require.True(t, (providercore.UpstreamErrorDecision{Policy: providercore.ErrorPolicyCustomMatched}).ShouldFailoverWithDefaults(provider, http.StatusUnprocessableEntity, false, false))
	require.False(t, (providercore.UpstreamErrorDecision{Policy: providercore.ErrorPolicyCustomSkipped}).ShouldFailoverWithDefaults(provider, http.StatusBadGateway, true, true))
}

// 替身记录健康字段写入。
type errorPolicyRepoStub struct {
	providercore.HealthStore
	tempCalls, setErrCalls int
	modelRateLimitCalls    []int64
}

func (r *errorPolicyRepoStub) SetTempUnschedulable(context.Context, int64, time.Time, string) error {
	r.tempCalls++
	return nil
}

func (r *errorPolicyRepoStub) SetError(context.Context, int64, string) error {
	r.setErrCalls++
	return nil
}

func (r *errorPolicyRepoStub) SetModelRateLimit(_ context.Context, id int64, _ string, _ time.Time, _ ...string) error {
	r.modelRateLimitCalls = append(r.modelRateLimitCalls, id)
	return nil
}

func newErrorPolicyObserver(repo *errorPolicyRepoStub) *UpstreamHealth {
	return &UpstreamHealth{Core: providercore.NewHealthService(repo, nil, providercore.HealthOptions{})}
}
