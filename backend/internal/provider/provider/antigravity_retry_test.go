package provider

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/gateway/session"
	"github.com/TokenFlux/TokenRouter/internal/infra/httpclient/tlsfingerprint"
	"github.com/TokenFlux/TokenRouter/internal/pkg/logredact"
	acct "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/upstream/antigravity"
)

// epFixedUpstream returns a fixed response for every request.
type epFixedUpstream struct {
	statusCode int
	body       string
	calls      int
}

// epProviderRepo records SetTempUnschedulable / SetError calls.
type epProviderRepo struct {
	acct.HealthStore
	tempCalls   int
	setErrCalls int
}

type epTrackingRepo struct {
	acct.HealthStore
	rateLimitedCalls int
	rateLimitedID    int64
	setErrCalls      int
	setErrID         int64
	tempCalls        int
}

// 保存每次模型窗口写入，用于核对原模型与家族键。
type antigravityFamilyStoreFixture struct {
	acct.AntigravityHealthStore
	modelRateLimitCalls []struct{ modelKey string }
}

// 夹具记录健康状态写入，策略和供应商错误分类使用生产实现。
type antigravityPolicyStoreFixture struct {
	acct.HealthStore
	tempCalls           int
	modelRateLimitCalls []struct{ scope, modelKey string }
}

// 记录两个测试端点，检查重试期间使用同一提供商。
type stubAntigravityUpstream struct {
	firstBase, secondBase string
	calls                 []string
}

// 预检查测试记录是否发送请求，成功响应使用固定报文。
type recordingOKUpstream struct{ calls int }

// stubSmartRetryCache 用于 handleSmartRetry 测试的 GatewayCache mock
// 仅关注 DeleteSessionProviderID 的调用记录。
type stubSmartRetryCache struct {
	session.GatewayCache // 调用未实现的方法会 panic。
	deleteCalls          []deleteSessionCall
}

type deleteSessionCall struct {
	groupID     int64
	sessionHash string
}

// mockSmartRetryUpstream 用于 handleSmartRetry 测试的 mock upstream。
type mockSmartRetryUpstream struct {
	responses      []*http.Response
	responseBodies [][]byte // 缓存的 response body 字节（用于 repeatLast 重建）
	errors         []error
	callIdx        int
	calls          []string
	userAgents     []string
	requestBodies  [][]byte
	repeatLast     bool // 超出范围时重复最后一个响应
}

type antigravityRetryStoreFixture struct {
	extraUpdateCalls []map[string]any
	acct.AntigravityHealthStore
	modelRateLimitCalls []struct {
		providerID int64
		modelKey   string
		resetAt    time.Time
	}
}

func TestIsCreditsExhausted_UsesAICreditsKey(t *testing.T) {
	t.Run("无 AICredits key 则积分可用", func(t *testing.T) {
		provider := &acct.Record{
			ID:       1,
			Platform: capability.PlatformAntigravity,
			Extra: map[string]any{
				"allow_overages": true,
			},
		}
		_, input := newAntigravityRetryFixture().Bind(AntigravityRetryRequest{Provider: provider})
		require.False(t, input.CreditsExhausted())
	})

	t.Run("AICredits key 生效则积分耗尽", func(t *testing.T) {
		provider := &acct.Record{
			ID:       2,
			Platform: capability.PlatformAntigravity,
			Extra: map[string]any{
				"allow_overages": true,
				"model_rate_limits": map[string]any{
					acct.CreditsExhaustedKey: map[string]any{
						"rate_limited_at":     time.Now().UTC().Format(time.RFC3339),
						"rate_limit_reset_at": time.Now().Add(5 * time.Hour).UTC().Format(time.RFC3339),
					},
				},
			},
		}
		_, input := newAntigravityRetryFixture().Bind(AntigravityRetryRequest{Provider: provider})
		require.True(t, input.CreditsExhausted())
	})

	t.Run("AICredits key 过期则积分可用", func(t *testing.T) {
		provider := &acct.Record{
			ID:       3,
			Platform: capability.PlatformAntigravity,
			Extra: map[string]any{
				"allow_overages": true,
				"model_rate_limits": map[string]any{
					acct.CreditsExhaustedKey: map[string]any{
						"rate_limited_at":     time.Now().Add(-6 * time.Hour).UTC().Format(time.RFC3339),
						"rate_limit_reset_at": time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339),
					},
				},
			},
		}
		_, input := newAntigravityRetryFixture().Bind(AntigravityRetryRequest{Provider: provider})
		require.False(t, input.CreditsExhausted())
	})
}

func TestHandleSmartRetry_QuotaExhausted_UsesCreditsAndStoresIndependentState(t *testing.T) {
	successResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
	}
	upstream := &mockSmartRetryUpstream{
		responses: []*http.Response{successResp},
		errors:    []error{nil},
	}
	repo := &antigravityRetryStoreFixture{}
	provider := &acct.Record{
		ID:       101,
		Name:     "acc-101",
		Type:     capability.ProviderTypeOAuth,
		Platform: capability.PlatformAntigravity,
		Extra: map[string]any{
			"allow_overages": true,
		},
		Credentials: map[string]any{
			"model_mapping": map[string]any{
				"claude-opus-4-6": "claude-sonnet-4-5",
			},
		},
	}

	respBody := []byte(`{"error":{"status":"RESOURCE_EXHAUSTED","message":"QUOTA_EXHAUSTED"}}`)
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader(respBody)),
	}
	params := AntigravityRetryRequest{
		Context:     context.Background(),
		UserAgent:   "probe-client/9.9",
		Prefix:      "[test]",
		Provider:    provider,
		AccessToken: "token",
		Action:      "generateContent",
		Body:        []byte(`{"model":"claude-opus-4-6","request":{}}`),
		Do: func(req *http.Request) (*http.Response, error) {
			return upstream.Do(req, "", provider.ID, provider.Concurrency)
		},
		ModelStore:     repo,
		RequestedModel: "claude-opus-4-6",
		HandleError: func(int, http.Header, []byte) {
		},
	}

	svc := newAntigravityRetryFixture()
	adapter, input := svc.Bind(params)
	result := adapter.HandleSmartRetry(input, resp, respBody, "https://ag-1.test", 0, []string{"https://ag-1.test"})

	require.NotNil(t, result)
	require.Equal(t, antigravity.SmartRetryActionBreakWithResp, result.Action)
	require.NotNil(t, result.Resp)
	require.Nil(t, result.SwitchError)
	require.Len(t, upstream.requestBodies, 1)
	require.Contains(t, string(upstream.requestBodies[0]), "enabledCreditTypes")
	require.Equal(t, "probe-client/9.9", upstream.userAgents[0])
	require.Empty(t, repo.modelRateLimitCalls, "overages 成功后不应写入普通 model_rate_limits")
}

func TestHandleSmartRetry_RateLimited_DoesNotUseCredits(t *testing.T) {
	successResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
	}
	upstream := &mockSmartRetryUpstream{
		responses: []*http.Response{successResp},
		errors:    []error{nil},
	}
	repo := &antigravityRetryStoreFixture{}
	provider := &acct.Record{
		ID:       102,
		Name:     "acc-102",
		Type:     capability.ProviderTypeOAuth,
		Platform: capability.PlatformAntigravity,
		Extra: map[string]any{
			"allow_overages": true,
		},
	}

	respBody := []byte(`{
		"error": {
			"status": "RESOURCE_EXHAUSTED",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "claude-sonnet-4-5"}, "reason": "RATE_LIMIT_EXCEEDED"},
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "0.1s"}
			]
		}
	}`)
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader(respBody)),
	}
	params := AntigravityRetryRequest{
		Context:     context.Background(),
		Prefix:      "[test]",
		Provider:    provider,
		AccessToken: "token",
		Action:      "generateContent",
		Body:        []byte(`{"model":"claude-sonnet-4-5","request":{}}`),
		Do: func(req *http.Request) (*http.Response, error) {
			return upstream.Do(req, "", provider.ID, provider.Concurrency)
		},
		ModelStore: repo,
		HandleError: func(int, http.Header, []byte) {
		},
	}

	svc := newAntigravityRetryFixture()
	adapter, input := svc.Bind(params)
	result := adapter.HandleSmartRetry(input, resp, respBody, "https://ag-1.test", 0, []string{"https://ag-1.test"})

	require.NotNil(t, result)
	require.Equal(t, antigravity.SmartRetryActionBreakWithResp, result.Action)
	require.NotNil(t, result.Resp)
	require.Len(t, upstream.requestBodies, 1)
	require.NotContains(t, string(upstream.requestBodies[0]), "enabledCreditTypes")
	require.Empty(t, repo.extraUpdateCalls)
	require.Empty(t, repo.modelRateLimitCalls)
}

func TestAntigravityRetryLoop_ModelRateLimited_InjectsCredits(t *testing.T) {
	oldBaseURLs := append([]string(nil), antigravity.BaseURLs...)
	oldAvailability := antigravity.DefaultURLAvailability
	defer func() {
		antigravity.BaseURLs = oldBaseURLs
		antigravity.DefaultURLAvailability = oldAvailability
	}()

	antigravity.BaseURLs = []string{"https://ag-1.test"}
	antigravity.DefaultURLAvailability = antigravity.NewURLAvailability(time.Minute)

	upstream := &mockSmartRetryUpstream{
		responses: []*http.Response{
			{
				StatusCode: http.StatusOK,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			},
		},
		errors: []error{nil},
	}
	// 模型已限流 + overages 启用 + 无 AICredits key → 应直接注入积分
	provider := &acct.Record{
		ID:          103,
		Name:        "acc-103",
		Type:        capability.ProviderTypeOAuth,
		Platform:    capability.PlatformAntigravity,
		Status:      billing.StatusActive,
		Schedulable: true,
		Extra: map[string]any{
			"allow_overages": true,
			"model_rate_limits": map[string]any{
				"claude-sonnet-4-5": map[string]any{
					"rate_limited_at":     time.Now().UTC().Format(time.RFC3339),
					"rate_limit_reset_at": time.Now().Add(30 * time.Minute).UTC().Format(time.RFC3339),
				},
			},
		},
	}

	svc := newAntigravityRetryFixture()
	adapter, input := svc.Bind(AntigravityRetryRequest{
		Context:     context.Background(),
		Prefix:      "[test]",
		Provider:    provider,
		AccessToken: "token",
		Action:      "generateContent",
		Body:        []byte(`{"model":"claude-sonnet-4-5","request":{}}`),
		Do: func(req *http.Request) (*http.Response, error) {
			return upstream.Do(req, "", provider.ID, provider.Concurrency)
		},
		RequestedModel: "claude-sonnet-4-5",
		HandleError: func(int, http.Header, []byte) {
		},
	})
	result, err := adapter.AntigravityRetryLoop(input)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.requestBodies, 1)
	require.Contains(t, string(upstream.requestBodies[0]), "enabledCreditTypes")
}

func TestAntigravityRetryLoop_CreditsExhausted_DoesNotInject(t *testing.T) {
	oldBaseURLs := append([]string(nil), antigravity.BaseURLs...)
	oldAvailability := antigravity.DefaultURLAvailability
	defer func() {
		antigravity.BaseURLs = oldBaseURLs
		antigravity.DefaultURLAvailability = oldAvailability
	}()

	antigravity.BaseURLs = []string{"https://ag-1.test"}
	antigravity.DefaultURLAvailability = antigravity.NewURLAvailability(time.Minute)

	// 模型限流 + overages 启用 + AICredits key 生效 → 不应注入积分，应切号
	provider := &acct.Record{
		ID:          104,
		Name:        "acc-104",
		Type:        capability.ProviderTypeOAuth,
		Platform:    capability.PlatformAntigravity,
		Status:      billing.StatusActive,
		Schedulable: true,
		Extra: map[string]any{
			"allow_overages": true,
			"model_rate_limits": map[string]any{
				"claude-sonnet-4-5": map[string]any{
					"rate_limited_at":     time.Now().UTC().Format(time.RFC3339),
					"rate_limit_reset_at": time.Now().Add(30 * time.Minute).UTC().Format(time.RFC3339),
				},
				acct.CreditsExhaustedKey: map[string]any{
					"rate_limited_at":     time.Now().UTC().Format(time.RFC3339),
					"rate_limit_reset_at": time.Now().Add(5 * time.Hour).UTC().Format(time.RFC3339),
				},
			},
		},
	}

	svc := newAntigravityRetryFixture()
	adapter, input := svc.Bind(AntigravityRetryRequest{
		Context:        context.Background(),
		Prefix:         "[test]",
		Provider:       provider,
		AccessToken:    "token",
		Action:         "generateContent",
		Body:           []byte(`{"model":"claude-sonnet-4-5","request":{}}`),
		RequestedModel: "claude-sonnet-4-5",
		HandleError: func(int, http.Header, []byte) {
		},
	})
	_, err := adapter.AntigravityRetryLoop(input)

	// 模型限流 + 积分耗尽 → 应触发切号错误
	require.Error(t, err)
	var switchErr *antigravity.AntigravityProviderSwitchError
	require.ErrorAs(t, err, &switchErr)
}

func TestAntigravityRetryLoop_CreditErrorMarksExhausted(t *testing.T) {
	oldBaseURLs := append([]string(nil), antigravity.BaseURLs...)
	oldAvailability := antigravity.DefaultURLAvailability
	defer func() {
		antigravity.BaseURLs = oldBaseURLs
		antigravity.DefaultURLAvailability = oldAvailability
	}()

	antigravity.BaseURLs = []string{"https://ag-1.test"}
	antigravity.DefaultURLAvailability = antigravity.NewURLAvailability(time.Minute)

	repo := &antigravityRetryStoreFixture{}
	upstream := &mockSmartRetryUpstream{
		responses: []*http.Response{
			{
				StatusCode: http.StatusForbidden,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"Insufficient GOOGLE_ONE_AI credits"}}`)),
			},
		},
		errors: []error{nil},
	}
	// 模型限流 + overages 启用 + 积分可用 → 注入积分但上游返回积分不足
	provider := &acct.Record{
		ID:          105,
		Name:        "acc-105",
		Type:        capability.ProviderTypeOAuth,
		Platform:    capability.PlatformAntigravity,
		Status:      billing.StatusActive,
		Schedulable: true,
		Extra: map[string]any{
			"allow_overages": true,
			"model_rate_limits": map[string]any{
				"claude-sonnet-4-5": map[string]any{
					"rate_limited_at":     time.Now().UTC().Format(time.RFC3339),
					"rate_limit_reset_at": time.Now().Add(30 * time.Minute).UTC().Format(time.RFC3339),
				},
			},
		},
	}

	svc := newAntigravityRetryFixture()
	svc.Health.Store = repo
	adapter, input := svc.Bind(AntigravityRetryRequest{
		Context:     context.Background(),
		Prefix:      "[test]",
		Provider:    provider,
		AccessToken: "token",
		Action:      "generateContent",
		Body:        []byte(`{"model":"claude-sonnet-4-5","request":{}}`),
		Do: func(req *http.Request) (*http.Response, error) {
			return upstream.Do(req, "", provider.ID, provider.Concurrency)
		},
		ModelStore:     repo,
		RequestedModel: "claude-sonnet-4-5",
		HandleError: func(int, http.Header, []byte) {
		},
	})
	result, err := adapter.AntigravityRetryLoop(input)

	require.NoError(t, err)
	require.NotNil(t, result)
	// 验证 AICredits key 已通过 SetModelRateLimit 写入数据库
	require.Len(t, repo.modelRateLimitCalls, 1, "应通过 SetModelRateLimit 写入 AICredits key")
	require.Equal(t, acct.CreditsExhaustedKey, repo.modelRateLimitCalls[0].modelKey)
}

func TestRetryLoop_ErrorPolicy_CustomErrorCodes(t *testing.T) {
	tests := []struct {
		name              string
		upstreamStatus    int
		upstreamBody      string
		customCodes       []any
		expectHandleError int
		expectUpstream    int
		expectStatusCode  int
	}{
		{
			name:              "429_in_custom_codes_matched",
			upstreamStatus:    429,
			upstreamBody:      `{"error":"rate limited"}`,
			customCodes:       []any{float64(429)},
			expectHandleError: 1,
			expectUpstream:    1,
			expectStatusCode:  429,
		},
		{
			name:              "429_not_in_custom_codes_skipped",
			upstreamStatus:    429,
			upstreamBody:      `{"error":"rate limited"}`,
			customCodes:       []any{float64(500)},
			expectHandleError: 0,
			expectUpstream:    1,
			expectStatusCode:  500,
		},
		{
			name:              "500_in_custom_codes_matched",
			upstreamStatus:    500,
			upstreamBody:      `{"error":"internal"}`,
			customCodes:       []any{float64(500)},
			expectHandleError: 1,
			expectUpstream:    1,
			expectStatusCode:  500,
		},
		{
			name:              "500_not_in_custom_codes_skipped",
			upstreamStatus:    500,
			upstreamBody:      `{"error":"internal"}`,
			customCodes:       []any{float64(429)},
			expectHandleError: 0,
			expectUpstream:    1,
			expectStatusCode:  500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			saveAndSetBaseURLs(t)

			upstream := &epFixedUpstream{statusCode: tt.upstreamStatus, body: tt.upstreamBody}
			repo := &epProviderRepo{}
			rlSvc := acct.NewHealthService(repo, nil, acct.HealthOptions{})

			provider := &acct.Record{
				ID:          100,
				Type:        capability.ProviderTypeAPIKey,
				Platform:    capability.PlatformAntigravity,
				Schedulable: true,
				Status:      billing.StatusActive,
				Concurrency: 1,
				Credentials: map[string]any{
					"custom_error_codes_enabled": true,
					"custom_error_codes":         tt.customCodes,
				},
			}

			svc := newAntigravityRetryFixture()
			svc.Policy = rlSvc

			var handleErrorCount int
			p := newRetryParams(provider, upstream, func(int, http.Header, []byte) {
				handleErrorCount++
			})

			adapter, input := svc.Bind(p)
			result, err := adapter.AntigravityRetryLoop(input)

			require.NoError(t, err)
			require.NotNil(t, result)
			require.NotNil(t, result.Resp)
			defer func() { _ = result.Resp.Body.Close() }()

			require.Equal(t, tt.expectStatusCode, result.Resp.StatusCode)
			require.Equal(t, tt.expectHandleError, handleErrorCount, "handleError call count")
			require.Equal(t, tt.expectUpstream, upstream.calls, "upstream call count")
		})
	}
}

func TestRetryLoop_ErrorPolicy_TempUnschedulable(t *testing.T) {
	tempRulesProvider := func(rules []any) *acct.Record {
		return &acct.Record{
			ID:          200,
			Type:        capability.ProviderTypeOAuth,
			Platform:    capability.PlatformAntigravity,
			Schedulable: true,
			Status:      billing.StatusActive,
			Concurrency: 1,
			Credentials: map[string]any{
				"temp_unschedulable_enabled": true,
				"temp_unschedulable_rules":   rules,
			},
		}
	}

	overloadedRule := map[string]any{
		"error_code":       float64(503),
		"keywords":         []any{"overloaded"},
		"duration_minutes": float64(10),
	}

	rateLimitRule := map[string]any{
		"error_code":       float64(429),
		"keywords":         []any{"rate limited keyword"},
		"duration_minutes": float64(5),
	}

	t.Run("503_overloaded_matches_rule", func(t *testing.T) {
		saveAndSetBaseURLs(t)

		upstream := &epFixedUpstream{statusCode: 503, body: `overloaded`}
		repo := &epProviderRepo{}
		rlSvc := acct.NewHealthService(repo, nil, acct.HealthOptions{})
		svc := newAntigravityRetryFixture()
		svc.Policy = rlSvc

		provider := tempRulesProvider([]any{overloadedRule})
		p := newRetryParams(provider, upstream, func(int, http.Header, []byte) {
			t.Error("handleError should not be called for temp unschedulable")
		})

		adapter, input := svc.Bind(p)
		result, err := adapter.AntigravityRetryLoop(input)

		require.Nil(t, result)
		var switchErr *antigravity.AntigravityProviderSwitchError
		require.ErrorAs(t, err, &switchErr)
		require.Equal(t, provider.ID, switchErr.OriginalProviderID)
		require.Equal(t, 1, upstream.calls, "should not retry")
	})

	t.Run("429_rate_limited_keyword_matches_rule", func(t *testing.T) {
		saveAndSetBaseURLs(t)

		upstream := &epFixedUpstream{statusCode: 429, body: `rate limited keyword`}
		repo := &epProviderRepo{}
		rlSvc := acct.NewHealthService(repo, nil, acct.HealthOptions{})
		svc := newAntigravityRetryFixture()
		svc.Policy = rlSvc

		provider := tempRulesProvider([]any{rateLimitRule})
		p := newRetryParams(provider, upstream, func(int, http.Header, []byte) {
			t.Error("handleError should not be called for temp unschedulable")
		})

		adapter, input := svc.Bind(p)
		result, err := adapter.AntigravityRetryLoop(input)

		require.Nil(t, result)
		var switchErr *antigravity.AntigravityProviderSwitchError
		require.ErrorAs(t, err, &switchErr)
		require.Equal(t, provider.ID, switchErr.OriginalProviderID)
		require.Equal(t, 1, upstream.calls, "should not retry")
	})

	t.Run("503_body_no_match_continues_default_retry", func(t *testing.T) {
		saveAndSetBaseURLs(t)

		upstream := &epFixedUpstream{statusCode: 503, body: `random`}
		repo := &epProviderRepo{}
		rlSvc := acct.NewHealthService(repo, nil, acct.HealthOptions{})
		svc := newAntigravityRetryFixture()
		svc.Policy = rlSvc

		provider := tempRulesProvider([]any{overloadedRule})

		// Use a short-lived context: the backoff sleep (~1s) will be
		// interrupted, proving the code entered the default retry path
		// instead of breaking early via error policy.
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		p := newRetryParams(provider, upstream, func(int, http.Header, []byte) {
		})
		p.Context = ctx

		adapter, input := svc.Bind(p)
		result, err := adapter.AntigravityRetryLoop(input)

		// Context cancellation during backoff proves default retry was entered
		require.Nil(t, result)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.GreaterOrEqual(t, upstream.calls, 1, "should have called upstream at least once")
	})
}

func TestRetryLoop_ErrorPolicy_NilRateLimitService(t *testing.T) {
	saveAndSetBaseURLs(t)

	upstream := &epFixedUpstream{statusCode: 429, body: `{"error":"rate limited"}`}
	// rateLimitService 为 nil 时请求仍能结束。
	svc := newAntigravityRetryFixture()

	provider := &acct.Record{
		ID:          300,
		Type:        capability.ProviderTypeOAuth,
		Platform:    capability.PlatformAntigravity,
		Schedulable: true,
		Status:      billing.StatusActive,
		Concurrency: 1,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	p := newRetryParams(provider, upstream, func(int, http.Header, []byte) {
	})
	p.Context = ctx

	// Should not panic; enters the default retry path (eventually times out)
	adapter, input := svc.Bind(p)
	result, err := adapter.AntigravityRetryLoop(input)

	require.Nil(t, result)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.GreaterOrEqual(t, upstream.calls, 1)
}

func TestRetryLoop_ErrorPolicy_NoPolicy_OriginalBehavior(t *testing.T) {
	saveAndSetBaseURLs(t)

	upstream := &epFixedUpstream{statusCode: 429, body: `{"error":"rate limited"}`}
	repo := &epProviderRepo{}
	rlSvc := acct.NewHealthService(repo, nil, acct.HealthOptions{})
	svc := newAntigravityRetryFixture()
	svc.Policy = rlSvc

	// Plain OAuth provider with no error policy configured
	provider := &acct.Record{
		ID:          400,
		Type:        capability.ProviderTypeOAuth,
		Platform:    capability.PlatformAntigravity,
		Schedulable: true,
		Status:      billing.StatusActive,
		Concurrency: 1,
	}

	var handleErrorCount int
	p := newRetryParams(provider, upstream, func(int, http.Header, []byte) {
		handleErrorCount++
	})

	adapter, input := svc.Bind(p)
	result, err := adapter.AntigravityRetryLoop(input)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Resp)
	defer func() { _ = result.Resp.Body.Close() }()

	require.Equal(t, http.StatusTooManyRequests, result.Resp.StatusCode)
	require.Equal(t, antigravity.AntigravityMaxRetries, upstream.calls, "should exhaust all retries")
	require.Equal(t, 1, handleErrorCount, "handleError should be called once after retries exhausted")
}

func TestCustomErrorCode599_SkippedErrors_Return500_NoRateLimit(t *testing.T) {
	errorCodes := []int{429, 500, 503, 401, 403}

	for _, upstreamStatus := range errorCodes {
		t.Run(http.StatusText(upstreamStatus), func(t *testing.T) {
			saveAndSetBaseURLs(t)

			upstream := &epFixedUpstream{
				statusCode: upstreamStatus,
				body:       `{"error":"some upstream error"}`,
			}
			repo := &epTrackingRepo{}
			rlSvc := acct.NewHealthService(repo, nil, acct.HealthOptions{})
			svc := newAntigravityRetryFixture()
			svc.Policy = rlSvc

			provider := &acct.Record{
				ID:          500,
				Type:        capability.ProviderTypeAPIKey,
				Platform:    capability.PlatformAntigravity,
				Schedulable: true,
				Status:      billing.StatusActive,
				Concurrency: 1,
				Credentials: map[string]any{
					"custom_error_codes_enabled": true,
					"custom_error_codes":         []any{float64(599)},
				},
			}

			var handleErrorCount int
			p := newRetryParams(provider, upstream, func(int, http.Header, []byte) {
				handleErrorCount++
			})

			adapter, input := svc.Bind(p)
			result, err := adapter.AntigravityRetryLoop(input)

			// 不应返回 error（Skipped 不触发提供商切换）
			require.NoError(t, err, "should not return error")
			require.NotNil(t, result, "result should not be nil")
			require.NotNil(t, result.Resp, "response should not be nil")
			defer func() { _ = result.Resp.Body.Close() }()

			// 跳过的自定义错误统一返回 500。
			require.Equal(t, http.StatusInternalServerError, result.Resp.StatusCode,
				"skipped error should return 500, not %d", upstreamStatus)

			// 不调用 handleError
			require.Equal(t, 0, handleErrorCount,
				"handleError should NOT be called for skipped errors")

			// 不标记限流
			require.Equal(t, 0, repo.rateLimitedCalls,
				"SetRateLimited should NOT be called for skipped errors")

			// 不停止调度
			require.Equal(t, 0, repo.setErrCalls,
				"SetError should NOT be called for skipped errors")

			// 上游调用次数为 1。
			require.Equal(t, 1, upstream.calls,
				"should call upstream exactly once (no retry)")
		})
	}
}

func TestSetAntigravityModelRateLimits_GeminiWritesFamilyScope(t *testing.T) {
	repo := &antigravityFamilyStoreFixture{}
	svc := &acct.AntigravityHealth{ModelKeys: AntigravityModelLimitKeys, Logf: func(string, ...any) {}}
	provider := &acct.Record{ID: 789, Platform: capability.PlatformAntigravity}
	resetAt := time.Now().Add(30 * time.Second)

	success := svc.SetAntigravityModelRateLimits(
		context.Background(),
		repo,
		provider,
		"gemini-3-pro",
		"[test]",
		429,
		resetAt,
		false,
	)

	require.True(t, success)
	require.Len(t, repo.modelRateLimitCalls, 2)
	require.Equal(t, "gemini-3-pro", repo.modelRateLimitCalls[0].modelKey)
	require.Equal(t, "antigravity:gemini", repo.modelRateLimitCalls[1].modelKey)
}

func TestSetAntigravityModelRateLimits_ClaudeDoesNotWriteGeminiScope(t *testing.T) {
	repo := &antigravityFamilyStoreFixture{}
	svc := &acct.AntigravityHealth{ModelKeys: AntigravityModelLimitKeys, Logf: func(string, ...any) {}}
	provider := &acct.Record{ID: 790, Platform: capability.PlatformAntigravity}
	resetAt := time.Now().Add(30 * time.Second)

	success := svc.SetAntigravityModelRateLimits(
		context.Background(),
		repo,
		provider,
		"claude-sonnet-4-5",
		"[test]",
		429,
		resetAt,
		false,
	)

	require.True(t, success)
	require.Len(t, repo.modelRateLimitCalls, 1)
	require.Equal(t, "claude-sonnet-4-5", repo.modelRateLimitCalls[0].modelKey)
}

func TestApplyErrorPolicy(t *testing.T) {
	tests := []struct {
		name              string
		provider          *acct.Record
		statusCode        int
		body              []byte
		expectedHandled   bool
		expectedStatus    int  // expected outStatus
		expectedSwitchErr bool // expect *AntigravityProviderSwitchError
		handleErrorCalls  int
	}{
		{
			name: "none_not_handled",
			provider: &acct.Record{
				ID:       10,
				Type:     capability.ProviderTypeOAuth,
				Platform: capability.PlatformAntigravity,
			},
			statusCode:       500,
			body:             []byte(`"error"`),
			expectedHandled:  false,
			expectedStatus:   500, // passthrough
			handleErrorCalls: 0,
		},
		{
			name: "skipped_handled_no_handleError",
			provider: &acct.Record{
				ID:       11,
				Type:     capability.ProviderTypeAPIKey,
				Platform: capability.PlatformAntigravity,
				Credentials: map[string]any{
					"custom_error_codes_enabled": true,
					"custom_error_codes":         []any{float64(429)},
				},
			},
			statusCode:       500, // not in custom codes
			body:             []byte(`"error"`),
			expectedHandled:  true,
			expectedStatus:   http.StatusInternalServerError, // skipped → 500
			handleErrorCalls: 0,
		},
		{
			name: "matched_handled_calls_handleError",
			provider: &acct.Record{
				ID:       12,
				Type:     capability.ProviderTypeAPIKey,
				Platform: capability.PlatformAntigravity,
				Credentials: map[string]any{
					"custom_error_codes_enabled": true,
					"custom_error_codes":         []any{float64(500)},
				},
			},
			statusCode:       500,
			body:             []byte(`"error"`),
			expectedHandled:  true,
			expectedStatus:   500, // matched → original status
			handleErrorCalls: 1,
		},
		{
			name: "temp_unscheduled_returns_switch_error",
			provider: &acct.Record{
				ID:       13,
				Type:     capability.ProviderTypeOAuth,
				Platform: capability.PlatformAntigravity,
				Credentials: map[string]any{
					"model_mapping": map[string]any{
						"claude-sonnet-4-5": "claude-sonnet-4-5",
					},
					"temp_unschedulable_enabled": true,
					"temp_unschedulable_rules": []any{
						map[string]any{
							"error_code":       float64(503),
							"keywords":         []any{"overloaded"},
							"duration_minutes": float64(10),
						},
					},
				},
			},
			statusCode:        503,
			body:              []byte(`overloaded`),
			expectedHandled:   true,
			expectedStatus:    503, // temp_unscheduled → original status
			expectedSwitchErr: true,
			handleErrorCalls:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &antigravityPolicyStoreFixture{}
			rlSvc := acct.NewHealthService(repo, nil, acct.HealthOptions{})
			svc := newAntigravityPolicyFixture(repo, rlSvc)

			var handleErrorCount int
			p := AntigravityRetryRequest{
				Context:        context.Background(),
				Prefix:         "[test]",
				Provider:       tt.provider,
				RequestedModel: "claude-sonnet-4-5",
				HandleError: func(int, http.Header, []byte) {
					handleErrorCount++
				},
				Sticky: true,
			}

			handled, outStatus, retErr := svc.applyPolicy(p, tt.statusCode, http.Header{}, tt.body)

			require.Equal(t, tt.expectedHandled, handled, "handled mismatch")
			require.Equal(t, tt.expectedStatus, outStatus, "outStatus mismatch")
			require.Equal(t, tt.handleErrorCalls, handleErrorCount, "handleError call count mismatch")

			if tt.expectedSwitchErr {
				var switchErr *antigravity.AntigravityProviderSwitchError
				require.ErrorAs(t, retErr, &switchErr)
				require.Equal(t, tt.provider.ID, switchErr.OriginalProviderID)
				require.Zero(t, repo.tempCalls)
				require.Len(t, repo.modelRateLimitCalls, 1)
				require.Equal(t, "claude-sonnet-4-5", repo.modelRateLimitCalls[0].scope)
			} else {
				require.NoError(t, retErr)
			}
		})
	}
}

func TestApplyErrorPolicy_GeminiRateLimitBypassesCustomSkip(t *testing.T) {
	repo := &antigravityPolicyStoreFixture{}
	var cleared []struct {
		groupID     int64
		sessionHash string
	}
	rlSvc := acct.NewHealthService(repo, nil, acct.HealthOptions{})
	svc := newAntigravityPolicyFixture(repo, rlSvc)

	provider := &acct.Record{
		ID:       31,
		Type:     capability.ProviderTypeAPIKey,
		Platform: capability.PlatformAntigravity,
		Credentials: map[string]any{
			"custom_error_codes_enabled": true,
			"custom_error_codes":         []any{float64(500)},
		},
	}
	body := []byte(`{
		"error": {
			"status": "RESOURCE_EXHAUSTED",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "gemini-3-flash"}, "reason": "RATE_LIMIT_EXCEEDED"},
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "15s"}
			]
		}
	}`)
	p := AntigravityRetryRequest{
		Context:    context.Background(),
		Prefix:     "[test]",
		Provider:   provider,
		ModelStore: repo,
		ClearSticky: func() {
			cleared = append(cleared, struct {
				groupID     int64
				sessionHash string
			}{42, "gemini:sticky"})
		},
		HandleError: func(int, http.Header, []byte) {
			t.Fatal("model rate limit should be handled before custom error fallback")
		},
	}

	handled, outStatus, retErr := svc.applyPolicy(p, http.StatusTooManyRequests, http.Header{}, body)

	require.True(t, handled)
	require.Equal(t, http.StatusTooManyRequests, outStatus)
	require.NoError(t, retErr)
	require.Len(t, repo.modelRateLimitCalls, 2)
	require.Equal(t, "gemini-3-flash", repo.modelRateLimitCalls[0].modelKey)
	require.Equal(t, "antigravity:gemini", repo.modelRateLimitCalls[1].modelKey)
	require.Len(t, cleared, 1)
	require.Equal(t, int64(42), cleared[0].groupID)
	require.Equal(t, "gemini:sticky", cleared[0].sessionHash)
}

func TestAntigravityRetryLoop_NoURLFallback_UsesConfiguredBaseURL(t *testing.T) {
	t.Setenv("GATEWAY_ANTIGRAVITY_FORWARD_BASE_URL", "")

	oldBaseURLs := append([]string(nil), antigravity.BaseURLs...)
	oldAvailability := antigravity.DefaultURLAvailability
	defer func() {
		antigravity.BaseURLs = oldBaseURLs
		antigravity.DefaultURLAvailability = oldAvailability
	}()

	base1 := "https://ag-1.test"
	base2 := "https://ag-2.test"
	antigravity.BaseURLs = []string{base1, base2}
	antigravity.DefaultURLAvailability = antigravity.NewURLAvailability(time.Minute)

	upstream := &stubAntigravityUpstream{firstBase: base1, secondBase: base2}
	provider := &acct.Record{
		ID:          1,
		Name:        "acc-1",
		Platform:    capability.PlatformAntigravity,
		Schedulable: true,
		Status:      billing.StatusActive,
		Concurrency: 1,
	}

	var handleErrorCalled bool
	svc := newAntigravityRetryFixture()
	adapter, input := svc.Bind(AntigravityRetryRequest{
		Prefix:      "[test]",
		Context:     context.Background(),
		Provider:    provider,
		ProxyURL:    "",
		AccessToken: "token",
		Action:      "generateContent",
		Body:        []byte(`{"input":"test"}`),
		Do: func(req *http.Request) (*http.Response, error) {
			return upstream.Do(req, "", provider.ID, provider.Concurrency)
		},
		RequestedModel: "claude-sonnet-4-5",
		HandleError: func(int, http.Header, []byte) {
			handleErrorCalled = true
		},
	})
	result, err := adapter.AntigravityRetryLoop(input)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Resp)
	defer func() { _ = result.Resp.Body.Close() }()
	require.Equal(t, http.StatusTooManyRequests, result.Resp.StatusCode)
	require.True(t, handleErrorCalled)
	require.Len(t, upstream.calls, antigravity.AntigravityMaxRetries)
	for _, callURL := range upstream.calls {
		require.True(t, strings.HasPrefix(callURL, base1))
	}

	available := antigravity.DefaultURLAvailability.GetAvailableURLsWithBase(antigravity.BaseURLs)
	require.NotEmpty(t, available)
	require.Equal(t, base1, available[0])
}

func TestAntigravityRetryLoop_PreCheck_SwitchesWhenRateLimited(t *testing.T) {
	upstream := &recordingOKUpstream{}
	provider := &acct.Record{
		ID:          1,
		Name:        "acc-1",
		Platform:    capability.PlatformAntigravity,
		Schedulable: true,
		Status:      billing.StatusActive,
		Concurrency: 1,
		Extra: map[string]any{
			"model_rate_limits": map[string]any{
				"claude-sonnet-4-5": map[string]any{
					"rate_limit_reset_at": time.Now().Add(2 * time.Second).Format(time.RFC3339),
				},
			},
		},
	}

	svc := newAntigravityRetryFixture()
	adapter, input := svc.Bind(AntigravityRetryRequest{
		Context:        context.Background(),
		Prefix:         "[test]",
		Provider:       provider,
		AccessToken:    "token",
		Action:         "generateContent",
		Body:           []byte(`{"input":"test"}`),
		RequestedModel: "claude-sonnet-4-5",
		Do: func(req *http.Request) (*http.Response, error) {
			return upstream.Do(req, "", provider.ID, provider.Concurrency)
		},
		Sticky: true,
		HandleError: func(int, http.Header, []byte) {
		},
	})
	result, err := adapter.AntigravityRetryLoop(input)

	require.Nil(t, result)
	var switchErr *antigravity.AntigravityProviderSwitchError
	require.ErrorAs(t, err, &switchErr)
	require.Equal(t, provider.ID, switchErr.OriginalProviderID)
	require.Equal(t, "claude-sonnet-4-5", switchErr.RateLimitedModel)
	require.True(t, switchErr.IsStickySession)
	require.Equal(t, 0, upstream.calls, "should not call upstream when switching on pre-check")
}

func TestAntigravityRetryLoop_PreCheck_SwitchesWhenRemainingLong(t *testing.T) {
	upstream := &recordingOKUpstream{}
	provider := &acct.Record{
		ID:          2,
		Name:        "acc-2",
		Platform:    capability.PlatformAntigravity,
		Schedulable: true,
		Status:      billing.StatusActive,
		Concurrency: 1,
		Extra: map[string]any{
			"model_rate_limits": map[string]any{
				"claude-sonnet-4-5": map[string]any{
					"rate_limit_reset_at": time.Now().Add(11 * time.Second).Format(time.RFC3339),
				},
			},
		},
	}

	svc := newAntigravityRetryFixture()
	adapter, input := svc.Bind(AntigravityRetryRequest{
		Context:        context.Background(),
		Prefix:         "[test]",
		Provider:       provider,
		AccessToken:    "token",
		Action:         "generateContent",
		Body:           []byte(`{"input":"test"}`),
		RequestedModel: "claude-sonnet-4-5",
		Do: func(req *http.Request) (*http.Response, error) {
			return upstream.Do(req, "", provider.ID, provider.Concurrency)
		},
		Sticky: true,
		HandleError: func(int, http.Header, []byte) {
		},
	})
	result, err := adapter.AntigravityRetryLoop(input)

	require.Nil(t, result)
	var switchErr *antigravity.AntigravityProviderSwitchError
	require.ErrorAs(t, err, &switchErr)
	require.Equal(t, provider.ID, switchErr.OriginalProviderID)
	require.Equal(t, "claude-sonnet-4-5", switchErr.RateLimitedModel)
	require.True(t, switchErr.IsStickySession)
	require.Equal(t, 0, upstream.calls, "should not call upstream when switching on pre-check")
}

func TestSingleProviderRetryConstants(t *testing.T) {
	require.Equal(t, 3, antigravity.AntigravitySingleProviderSmartRetryMaxAttempts,
		"单提供商原地重试最多 3 次")
	require.Equal(t, 15*time.Second, antigravity.AntigravitySingleProviderSmartRetryMaxWait,
		"单次最大等待 15s")
	require.Equal(t, 30*time.Second, antigravity.AntigravitySingleProviderSmartRetryTotalMaxWait,
		"总累计等待不超过 30s")
}

// TestHandleSmartRetry_503_LongDelay_SingleProviderRetry_RetryInPlace
// 核心场景：503 + retryDelay >= 7s + SingleProviderRetry 标记
// 单提供商的 503 响应触发原地重试。
func TestHandleSmartRetry_503_LongDelay_SingleProviderRetry_RetryInPlace(t *testing.T) {
	// 原地重试成功
	successResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(`{"result":"ok"}`)),
	}
	upstream := &mockSmartRetryUpstream{
		responses: []*http.Response{successResp},
		errors:    []error{nil},
	}

	repo := &antigravityRetryStoreFixture{}
	provider := &acct.Record{
		ID:          1,
		Name:        "acc-single",
		Type:        capability.ProviderTypeOAuth,
		Platform:    capability.PlatformAntigravity,
		Concurrency: 1,
	}

	// 503 + 39s >= 7s 阈值 + MODEL_CAPACITY_EXHAUSTED
	respBody := []byte(`{
		"error": {
			"code": 503,
			"status": "UNAVAILABLE",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "gemini-3-pro-high"}, "reason": "MODEL_CAPACITY_EXHAUSTED"},
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "39s"}
			],
			"message": "No capacity available for model gemini-3-pro-high on the server"
		}
	}`)
	resp := &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader(respBody)),
	}

	params := AntigravityRetryRequest{
		Context: context.Background(), SingleProvider: true, // 关键：设置单提供商标记
		Prefix:      "[test]",
		Provider:    provider,
		AccessToken: "token",
		Action:      "generateContent",
		Body:        []byte(`{"input":"test"}`),
		Do: func(req *http.Request) (*http.Response, error) {
			return upstream.Do(req, "", provider.ID, provider.Concurrency)
		},
		ModelStore: repo,
		HandleError: func(int, http.Header, []byte) {
		},
	}

	availableURLs := []string{"https://ag-1.test"}

	svc := newAntigravityRetryFixture()
	adapter, input := svc.Bind(params)
	result := adapter.HandleSmartRetry(input, resp, respBody, "https://ag-1.test", 0, availableURLs)

	require.NotNil(t, result)
	require.Equal(t, antigravity.SmartRetryActionBreakWithResp, result.Action)
	// 同一提供商重试成功后返回 resp。
	require.NotNil(t, result.Resp, "should return successful response from in-place retry")
	require.Equal(t, http.StatusOK, result.Resp.StatusCode)
	require.Nil(t, result.SwitchError, "should NOT return switchError in single provider mode")
	require.Nil(t, result.Err)

	// 验证未设模型限流（单提供商模式不应设限流）
	require.Len(t, repo.modelRateLimitCalls, 0,
		"should NOT set model rate limit in single provider retry mode")

	// 验证确实调用了 upstream（原地重试）
	require.GreaterOrEqual(t, len(upstream.calls), 1, "should have made at least one retry call")
}

// TestHandleSmartRetry_503_LongDelay_NoSingleProviderRetry_StillSwitches
// 对照组：503 + retryDelay >= 7s + 无 SingleProviderRetry 标记
// → 照常设模型限流 + 切换提供商。
func TestHandleSmartRetry_503_LongDelay_NoSingleProviderRetry_StillSwitches(t *testing.T) {
	repo := &antigravityRetryStoreFixture{}
	provider := &acct.Record{
		ID:       2,
		Name:     "acc-multi",
		Type:     capability.ProviderTypeOAuth,
		Platform: capability.PlatformAntigravity,
	}

	// 503 携带 39 秒等待时间，超过七秒阈值。
	// RATE_LIMIT_EXCEEDED 触发模型限流检查，MODEL_CAPACITY_EXHAUSTED 使用单独的重试流程。
	respBody := []byte(`{
		"error": {
			"code": 503,
			"status": "RESOURCE_EXHAUSTED",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "gemini-3-pro-high"}, "reason": "RATE_LIMIT_EXCEEDED"},
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "39s"}
			]
		}
	}`)
	resp := &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader(respBody)),
	}

	params := AntigravityRetryRequest{
		Context:     context.Background(), // 关键：无单提供商标记
		Prefix:      "[test]",
		Provider:    provider,
		AccessToken: "token",
		Action:      "generateContent",
		Body:        []byte(`{"input":"test"}`),
		ModelStore:  repo,
		HandleError: func(int, http.Header, []byte) {
		},
	}

	availableURLs := []string{"https://ag-1.test"}

	svc := newAntigravityRetryFixture()
	adapter, input := svc.Bind(params)
	result := adapter.HandleSmartRetry(input, resp, respBody, "https://ag-1.test", 0, availableURLs)

	require.NotNil(t, result)
	require.Equal(t, antigravity.SmartRetryActionBreakWithResp, result.Action)
	// 对照：多提供商模式返回 switchError
	require.NotNil(t, result.SwitchError, "multi-provider mode should return switchError for 503")
	require.Nil(t, result.Resp, "should not return resp when switchError is set")

	// 对照：多提供商模式应设模型限流
	require.Len(t, repo.modelRateLimitCalls, 2,
		"multi-provider mode SHOULD set model rate limit")
	require.Equal(t, "gemini-3-pro-high", repo.modelRateLimitCalls[0].modelKey)
	require.Equal(t, "antigravity:gemini", repo.modelRateLimitCalls[1].modelKey)
}

// TestHandleSmartRetry_429_LongDelay_SingleProviderRetry_StillSwitches
// 检查 429 与 SingleProviderRetry 标记的组合。
// 单提供商的 503 响应触发原地重试，429 响应切换提供商。
func TestHandleSmartRetry_429_LongDelay_SingleProviderRetry_StillSwitches(t *testing.T) {
	repo := &antigravityRetryStoreFixture{}
	provider := &acct.Record{
		ID:       3,
		Name:     "acc-429",
		Type:     capability.ProviderTypeOAuth,
		Platform: capability.PlatformAntigravity,
	}

	// 429 + 15s >= 7s 阈值
	respBody := []byte(`{
		"error": {
			"status": "RESOURCE_EXHAUSTED",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "claude-sonnet-4-5"}, "reason": "RATE_LIMIT_EXCEEDED"},
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "15s"}
			]
		}
	}`)
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests, // 429，不是 503
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader(respBody)),
	}

	params := AntigravityRetryRequest{
		Context: context.Background(), SingleProvider: true, // 有单提供商标记
		Prefix:      "[test]",
		Provider:    provider,
		AccessToken: "token",
		Action:      "generateContent",
		Body:        []byte(`{"input":"test"}`),
		ModelStore:  repo,
		HandleError: func(int, http.Header, []byte) {
		},
	}

	availableURLs := []string{"https://ag-1.test"}

	svc := newAntigravityRetryFixture()
	adapter, input := svc.Bind(params)
	result := adapter.HandleSmartRetry(input, resp, respBody, "https://ag-1.test", 0, availableURLs)

	require.NotNil(t, result)
	require.Equal(t, antigravity.SmartRetryActionBreakWithResp, result.Action)
	// 429 即使有单提供商标记，也应走切换提供商
	require.NotNil(t, result.SwitchError, "429 should still return switchError even with SingleProviderRetry")
	require.Len(t, repo.modelRateLimitCalls, 1,
		"429 should still set model rate limit even with SingleProviderRetry")
}

// TestHandleSmartRetry_503_ShortDelay_SingleProviderRetry_NoRateLimit
// 503 + retryDelay < 7s + SingleProviderRetry → 智能重试耗尽后直接返回 503，不设限流
// 使用 RATE_LIMIT_EXCEEDED，执行一次智能重试。
func TestHandleSmartRetry_503_ShortDelay_SingleProviderRetry_NoRateLimit(t *testing.T) {
	// 智能重试也返回 503
	failRespBody := `{
		"error": {
			"code": 503,
			"status": "RESOURCE_EXHAUSTED",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "gemini-3-flash"}, "reason": "RATE_LIMIT_EXCEEDED"},
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "0.1s"}
			]
		}
	}`
	failResp := &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(failRespBody)),
	}
	upstream := &mockSmartRetryUpstream{
		responses:  []*http.Response{failResp},
		errors:     []error{nil},
		repeatLast: true,
	}

	repo := &antigravityRetryStoreFixture{}
	provider := &acct.Record{
		ID:       4,
		Name:     "acc-short-503",
		Type:     capability.ProviderTypeOAuth,
		Platform: capability.PlatformAntigravity,
	}

	// 0.1s < 7s 阈值
	respBody := []byte(`{
		"error": {
			"code": 503,
			"status": "RESOURCE_EXHAUSTED",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "gemini-3-flash"}, "reason": "RATE_LIMIT_EXCEEDED"},
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "0.1s"}
			]
		}
	}`)
	resp := &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader(respBody)),
	}

	params := AntigravityRetryRequest{
		Context: context.Background(), SingleProvider: true,
		Prefix:      "[test]",
		Provider:    provider,
		AccessToken: "token",
		Action:      "generateContent",
		Body:        []byte(`{"input":"test"}`),
		Do: func(req *http.Request) (*http.Response, error) {
			return upstream.Do(req, "", provider.ID, provider.Concurrency)
		},
		ModelStore: repo,
		HandleError: func(int, http.Header, []byte) {
		},
	}

	availableURLs := []string{"https://ag-1.test"}

	svc := newAntigravityRetryFixture()
	adapter, input := svc.Bind(params)
	result := adapter.HandleSmartRetry(input, resp, respBody, "https://ag-1.test", 0, availableURLs)

	require.NotNil(t, result)
	require.Equal(t, antigravity.SmartRetryActionBreakWithResp, result.Action)
	// 关键断言：单提供商 503 模式下，智能重试耗尽后直接返回 503 响应，不切换
	require.NotNil(t, result.Resp, "should return 503 response directly for single provider mode")
	require.Equal(t, http.StatusServiceUnavailable, result.Resp.StatusCode)
	require.Nil(t, result.SwitchError, "should NOT switch provider in single provider mode")

	// 关键断言：不设模型限流
	require.Len(t, repo.modelRateLimitCalls, 0,
		"should NOT set model rate limit for 503 in single provider mode")
}

// TestHandleSmartRetry_503_ShortDelay_NoSingleProviderRetry_SetsRateLimit
// 对照组：503 + retryDelay < 7s + 无 SingleProviderRetry → 智能重试耗尽后照常设限流
// 使用 RATE_LIMIT_EXCEEDED，MODEL_CAPACITY_EXHAUSTED 使用独立的六十次重试流程。
func TestHandleSmartRetry_503_ShortDelay_NoSingleProviderRetry_SetsRateLimit(t *testing.T) {
	failRespBody := `{
		"error": {
			"code": 503,
			"status": "RESOURCE_EXHAUSTED",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "gemini-3-flash"}, "reason": "RATE_LIMIT_EXCEEDED"},
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "0.1s"}
			]
		}
	}`
	failResp := &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(failRespBody)),
	}
	upstream := &mockSmartRetryUpstream{
		responses: []*http.Response{failResp},
		errors:    []error{nil},
	}

	repo := &antigravityRetryStoreFixture{}
	provider := &acct.Record{
		ID:       5,
		Name:     "acc-multi-503",
		Type:     capability.ProviderTypeOAuth,
		Platform: capability.PlatformAntigravity,
	}

	respBody := []byte(`{
		"error": {
			"code": 503,
			"status": "RESOURCE_EXHAUSTED",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "gemini-3-flash"}, "reason": "RATE_LIMIT_EXCEEDED"},
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "0.1s"}
			]
		}
	}`)
	resp := &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader(respBody)),
	}

	params := AntigravityRetryRequest{
		Context:     context.Background(), // 无单提供商标记
		Prefix:      "[test]",
		Provider:    provider,
		AccessToken: "token",
		Action:      "generateContent",
		Body:        []byte(`{"input":"test"}`),
		Do: func(req *http.Request) (*http.Response, error) {
			return upstream.Do(req, "", provider.ID, provider.Concurrency)
		},
		ModelStore: repo,
		HandleError: func(int, http.Header, []byte) {
		},
	}

	availableURLs := []string{"https://ag-1.test"}

	svc := newAntigravityRetryFixture()
	adapter, input := svc.Bind(params)
	result := adapter.HandleSmartRetry(input, resp, respBody, "https://ag-1.test", 0, availableURLs)

	require.NotNil(t, result)
	require.Equal(t, antigravity.SmartRetryActionBreakWithResp, result.Action)
	// 对照：多提供商模式应返回 switchError
	require.NotNil(t, result.SwitchError, "multi-provider mode should return switchError for 503")
	// 对照：多提供商模式应设模型限流
	require.Len(t, repo.modelRateLimitCalls, 2,
		"multi-provider mode should set model rate limit")
	require.Equal(t, "gemini-3-flash", repo.modelRateLimitCalls[0].modelKey)
	require.Equal(t, "antigravity:gemini", repo.modelRateLimitCalls[1].modelKey)
}

// TestHandleSingleProviderRetryInPlace_Success 原地重试成功。
func TestHandleSingleProviderRetryInPlace_Success(t *testing.T) {
	successResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(`{"result":"ok"}`)),
	}
	upstream := &mockSmartRetryUpstream{
		responses: []*http.Response{successResp},
		errors:    []error{nil},
	}

	provider := &acct.Record{
		ID:          10,
		Name:        "acc-inplace-ok",
		Type:        capability.ProviderTypeOAuth,
		Platform:    capability.PlatformAntigravity,
		Concurrency: 1,
	}

	resp := &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Header:     http.Header{},
	}

	params := AntigravityRetryRequest{
		Context: context.Background(), SingleProvider: true,
		Prefix:      "[test]",
		Provider:    provider,
		AccessToken: "token",
		Action:      "generateContent",
		Body:        []byte(`{"input":"test"}`),
		Do: func(req *http.Request) (*http.Response, error) {
			return upstream.Do(req, "", provider.ID, provider.Concurrency)
		},
	}

	svc := newAntigravityRetryFixture()
	adapter, input := svc.Bind(params)
	result := adapter.HandleSingleProviderRetryInPlace(input, resp, nil, "https://ag-1.test", 1*time.Second, "gemini-3-pro")

	require.NotNil(t, result)
	require.Equal(t, antigravity.SmartRetryActionBreakWithResp, result.Action)
	require.NotNil(t, result.Resp, "should return successful response")
	require.Equal(t, http.StatusOK, result.Resp.StatusCode)
	require.Nil(t, result.SwitchError, "should not switch provider on success")
	require.Nil(t, result.Err)
}

// TestHandleSingleProviderRetryInPlace_AllRetriesFail 所有重试都失败，返回 503（不设限流）。
func TestHandleSingleProviderRetryInPlace_AllRetriesFail(t *testing.T) {
	// 构造 3 个 503 响应（对应 3 次原地重试）
	var responses []*http.Response
	var errors []error
	for range antigravity.AntigravitySingleProviderSmartRetryMaxAttempts {
		responses = append(responses, &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Header:     http.Header{},
			Body: io.NopCloser(strings.NewReader(`{
				"error": {
					"code": 503,
					"status": "UNAVAILABLE",
					"details": [
						{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "gemini-3-pro"}, "reason": "MODEL_CAPACITY_EXHAUSTED"},
						{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "0.1s"}
					]
				}
			}`)),
		})
		errors = append(errors, nil)
	}
	upstream := &mockSmartRetryUpstream{
		responses: responses,
		errors:    errors,
	}

	provider := &acct.Record{
		ID:          11,
		Name:        "acc-inplace-fail",
		Type:        capability.ProviderTypeOAuth,
		Platform:    capability.PlatformAntigravity,
		Concurrency: 1,
	}

	origBody := []byte(`{"error":{"code":503,"status":"UNAVAILABLE"}}`)
	resp := &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Header:     http.Header{"X-Test": {"original"}},
	}

	params := AntigravityRetryRequest{
		Context: context.Background(), SingleProvider: true,
		Prefix:      "[test]",
		Provider:    provider,
		AccessToken: "token",
		Action:      "generateContent",
		Body:        []byte(`{"input":"test"}`),
		Do: func(req *http.Request) (*http.Response, error) {
			return upstream.Do(req, "", provider.ID, provider.Concurrency)
		},
	}

	svc := newAntigravityRetryFixture()
	adapter, input := svc.Bind(params)
	result := adapter.HandleSingleProviderRetryInPlace(input, resp, origBody, "https://ag-1.test", 1*time.Second, "gemini-3-pro")

	require.NotNil(t, result)
	require.Equal(t, antigravity.SmartRetryActionBreakWithResp, result.Action)
	// 关键：返回 503 resp，不返回 switchError
	require.NotNil(t, result.Resp, "should return 503 response directly")
	require.Equal(t, http.StatusServiceUnavailable, result.Resp.StatusCode)
	require.Nil(t, result.SwitchError, "should NOT return switchError - let Handler handle it")
	require.Nil(t, result.Err)

	// 验证确实重试了指定次数
	require.Len(t, upstream.calls, antigravity.AntigravitySingleProviderSmartRetryMaxAttempts,
		"should have made exactly maxAttempts retry calls")
}

// TestHandleSingleProviderRetryInPlace_WaitDurationClamped 等待时间被限制在 [min, max] 范围。
func TestHandleSingleProviderRetryInPlace_WaitDurationClamped(t *testing.T) {
	// 用短延迟的成功响应，只验证不 panic
	successResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(`{"result":"ok"}`)),
	}
	upstream := &mockSmartRetryUpstream{
		responses: []*http.Response{successResp},
		errors:    []error{nil},
	}

	provider := &acct.Record{
		ID:          12,
		Name:        "acc-clamp",
		Type:        capability.ProviderTypeOAuth,
		Platform:    capability.PlatformAntigravity,
		Concurrency: 1,
	}

	resp := &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Header:     http.Header{},
	}

	params := AntigravityRetryRequest{
		Context: context.Background(), SingleProvider: true,
		Prefix:      "[test]",
		Provider:    provider,
		AccessToken: "token",
		Action:      "generateContent",
		Body:        []byte(`{"input":"test"}`),
		Do: func(req *http.Request) (*http.Response, error) {
			return upstream.Do(req, "", provider.ID, provider.Concurrency)
		},
	}

	svc := newAntigravityRetryFixture()

	// waitDuration=0 会被 clamp 到 antigravitySmartRetryMinWait=1s。
	// 首次重试即成功（200），总耗时 ~1s。
	adapter, input := svc.Bind(params)
	result := adapter.HandleSingleProviderRetryInPlace(input, resp, nil, "https://ag-1.test", 0, "gemini-3-pro")
	require.NotNil(t, result)
	require.Equal(t, antigravity.SmartRetryActionBreakWithResp, result.Action)
	require.NotNil(t, result.Resp)
	require.Equal(t, http.StatusOK, result.Resp.StatusCode)
}

// TestHandleSingleProviderRetryInPlace_ContextCanceled context 取消时立即返回。
func TestHandleSingleProviderRetryInPlace_ContextCanceled(t *testing.T) {
	upstream := &mockSmartRetryUpstream{
		responses: []*http.Response{nil},
		errors:    []error{nil},
	}

	provider := &acct.Record{
		ID:          13,
		Name:        "acc-cancel",
		Type:        capability.ProviderTypeOAuth,
		Platform:    capability.PlatformAntigravity,
		Concurrency: 1,
	}

	resp := &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Header:     http.Header{},
	}

	ctx, cancel := context.WithCancel(context.Background())

	cancel() // 立即取消

	params := AntigravityRetryRequest{
		Context: ctx, SingleProvider: true,
		Prefix:      "[test]",
		Provider:    provider,
		AccessToken: "token",
		Action:      "generateContent",
		Body:        []byte(`{"input":"test"}`),
		Do: func(req *http.Request) (*http.Response, error) {
			return upstream.Do(req, "", provider.ID, provider.Concurrency)
		},
	}

	svc := newAntigravityRetryFixture()
	adapter, input := svc.Bind(params)
	result := adapter.HandleSingleProviderRetryInPlace(input, resp, nil, "https://ag-1.test", 1*time.Second, "gemini-3-pro")

	require.NotNil(t, result)
	require.Equal(t, antigravity.SmartRetryActionBreakWithResp, result.Action)
	require.Error(t, result.Err, "should return context error")
	// 不应调用 upstream（因为在等待阶段就被取消了）
	require.Len(t, upstream.calls, 0, "should not call upstream when context is canceled")
}

// TestHandleSingleProviderRetryInPlace_NetworkError_ContinuesRetry 网络错误时继续重试。
func TestHandleSingleProviderRetryInPlace_NetworkError_ContinuesRetry(t *testing.T) {
	successResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(`{"result":"ok"}`)),
	}
	upstream := &mockSmartRetryUpstream{
		// 第 1 次网络错误（nil resp），第 2 次成功
		responses: []*http.Response{nil, successResp},
		errors:    []error{nil, nil},
	}

	provider := &acct.Record{
		ID:          14,
		Name:        "acc-net-retry",
		Type:        capability.ProviderTypeOAuth,
		Platform:    capability.PlatformAntigravity,
		Concurrency: 1,
	}

	resp := &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Header:     http.Header{},
	}

	params := AntigravityRetryRequest{
		Context: context.Background(), SingleProvider: true,
		Prefix:      "[test]",
		Provider:    provider,
		AccessToken: "token",
		Action:      "generateContent",
		Body:        []byte(`{"input":"test"}`),
		Do: func(req *http.Request) (*http.Response, error) {
			return upstream.Do(req, "", provider.ID, provider.Concurrency)
		},
	}

	svc := newAntigravityRetryFixture()
	adapter, input := svc.Bind(params)
	result := adapter.HandleSingleProviderRetryInPlace(input, resp, nil, "https://ag-1.test", 1*time.Second, "gemini-3-pro")

	require.NotNil(t, result)
	require.Equal(t, antigravity.SmartRetryActionBreakWithResp, result.Action)
	require.NotNil(t, result.Resp, "should return successful response after network error recovery")
	require.Equal(t, http.StatusOK, result.Resp.StatusCode)
	require.Len(t, upstream.calls, 2, "first call fails (network error), second succeeds")
}

// TestAntigravityRetryLoop_PreCheck_SingleProviderRetry_SkipsRateLimit
// 预检查中，如果有 SingleProviderRetry 标记，即使提供商已限流也跳过直接发请求。
func TestAntigravityRetryLoop_PreCheck_SingleProviderRetry_SkipsRateLimit(t *testing.T) {
	// 创建一个已设模型限流的提供商
	upstream := &recordingOKUpstream{}
	provider := &acct.Record{
		ID:          20,
		Name:        "acc-rate-limited",
		Type:        capability.ProviderTypeOAuth,
		Platform:    capability.PlatformAntigravity,
		Schedulable: true,
		Status:      billing.StatusActive,
		Concurrency: 1,
		Extra: map[string]any{
			"model_rate_limits": map[string]any{
				"claude-sonnet-4-5": map[string]any{
					"rate_limit_reset_at": time.Now().Add(30 * time.Second).Format(time.RFC3339),
				},
			},
		},
	}

	svc := newAntigravityRetryFixture()
	adapter, input := svc.Bind(AntigravityRetryRequest{
		Context: context.Background(), SingleProvider: true,
		Prefix:      "[test]",
		Provider:    provider,
		AccessToken: "token",
		Action:      "generateContent",
		Body:        []byte(`{"input":"test"}`),
		Do: func(req *http.Request) (*http.Response, error) {
			return upstream.Do(req, "", provider.ID, provider.Concurrency)
		},
		RequestedModel: "claude-sonnet-4-5",
		HandleError: func(int, http.Header, []byte) {
		},
	})
	result, err := adapter.AntigravityRetryLoop(input)

	require.NoError(t, err, "should not return error")
	require.NotNil(t, result, "should return result")
	require.NotNil(t, result.Resp, "should have response")
	require.Equal(t, http.StatusOK, result.Resp.StatusCode)
	// 关键：尽管限流了，有 SingleProviderRetry 标记时仍然到达了 upstream
	require.Equal(t, 1, upstream.calls, "should have reached upstream despite rate limit")
}

// TestAntigravityRetryLoop_PreCheck_NoSingleProviderRetry_SwitchesOnRateLimit
// 对照组：无 SingleProviderRetry + 已限流 → 预检查返回 switchError。
func TestAntigravityRetryLoop_PreCheck_NoSingleProviderRetry_SwitchesOnRateLimit(t *testing.T) {
	upstream := &recordingOKUpstream{}
	provider := &acct.Record{
		ID:          21,
		Name:        "acc-rate-limited-multi",
		Type:        capability.ProviderTypeOAuth,
		Platform:    capability.PlatformAntigravity,
		Schedulable: true,
		Status:      billing.StatusActive,
		Concurrency: 1,
		Extra: map[string]any{
			"model_rate_limits": map[string]any{
				"claude-sonnet-4-5": map[string]any{
					"rate_limit_reset_at": time.Now().Add(30 * time.Second).Format(time.RFC3339),
				},
			},
		},
	}

	svc := newAntigravityRetryFixture()
	adapter, input := svc.Bind(AntigravityRetryRequest{
		Context:     context.Background(), // 无单提供商标记
		Prefix:      "[test]",
		Provider:    provider,
		AccessToken: "token",
		Action:      "generateContent",
		Body:        []byte(`{"input":"test"}`),
		Do: func(req *http.Request) (*http.Response, error) {
			return upstream.Do(req, "", provider.ID, provider.Concurrency)
		},
		RequestedModel: "claude-sonnet-4-5",
		HandleError: func(int, http.Header, []byte) {
		},
	})
	result, err := adapter.AntigravityRetryLoop(input)

	require.Nil(t, result, "should not return result on rate limit switch")
	require.NotNil(t, err, "should return error")

	var switchErr *antigravity.AntigravityProviderSwitchError
	require.ErrorAs(t, err, &switchErr, "should return AntigravityProviderSwitchError")
	require.Equal(t, provider.ID, switchErr.OriginalProviderID)
	require.Equal(t, "claude-sonnet-4-5", switchErr.RateLimitedModel)

	// upstream 不应被调用（预检查就短路了）
	require.Equal(t, 0, upstream.calls, "upstream should NOT be called when pre-check blocks")
}

// TestHandleSmartRetry_503_SingleProvider_RetryInPlace_ThenSuccess_E2E
// 端到端场景：503 + 单提供商 + 原地重试第 2 次成功。
func TestHandleSmartRetry_503_SingleProvider_RetryInPlace_ThenSuccess_E2E(t *testing.T) {
	// 第 1 次原地重试仍返回 503，第 2 次成功
	fail503Body := `{
		"error": {
			"code": 503,
			"status": "UNAVAILABLE",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "gemini-3-pro"}, "reason": "MODEL_CAPACITY_EXHAUSTED"},
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "0.1s"}
			]
		}
	}`
	resp503 := &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(fail503Body)),
	}
	successResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(`{"result":"ok"}`)),
	}

	upstream := &mockSmartRetryUpstream{
		responses: []*http.Response{resp503, successResp},
		errors:    []error{nil, nil},
	}

	provider := &acct.Record{
		ID:          30,
		Name:        "acc-e2e",
		Type:        capability.ProviderTypeOAuth,
		Platform:    capability.PlatformAntigravity,
		Concurrency: 1,
	}

	resp := &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Header:     http.Header{},
	}

	params := AntigravityRetryRequest{
		Context: context.Background(), SingleProvider: true,
		Prefix:      "[test]",
		Provider:    provider,
		AccessToken: "token",
		Action:      "generateContent",
		Body:        []byte(`{"input":"test"}`),
		Do: func(req *http.Request) (*http.Response, error) {
			return upstream.Do(req, "", provider.ID, provider.Concurrency)
		},
	}

	svc := newAntigravityRetryFixture()
	adapter, input := svc.Bind(params)
	result := adapter.HandleSingleProviderRetryInPlace(input, resp, nil, "https://ag-1.test", 1*time.Second, "gemini-3-pro")

	require.NotNil(t, result)
	require.Equal(t, antigravity.SmartRetryActionBreakWithResp, result.Action)
	require.NotNil(t, result.Resp, "should return successful response after 2nd attempt")
	require.Equal(t, http.StatusOK, result.Resp.StatusCode)
	require.Nil(t, result.SwitchError)
	require.Len(t, upstream.calls, 2, "first 503, second OK")
}

// TestAntigravityRetryLoop_503_SingleProvider_InPlaceRetryUsed_E2E
// 通过 antigravityRetryLoop → handleSmartRetry → handleSingleProviderRetryInPlace 完整链路。
func TestAntigravityRetryLoop_503_SingleProvider_InPlaceRetryUsed_E2E(t *testing.T) {
	// 初始请求返回 503 + 长延迟
	initial503Body := []byte(`{
		"error": {
			"code": 503,
			"status": "UNAVAILABLE",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "gemini-3-pro"}, "reason": "MODEL_CAPACITY_EXHAUSTED"},
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "10s"}
			],
			"message": "No capacity available"
		}
	}`)
	initial503Resp := &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader(initial503Body)),
	}

	// 原地重试成功
	successResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(`{"result":"ok"}`)),
	}

	upstream := &mockSmartRetryUpstream{
		// 第 1 次调用（retryLoop 主循环）返回 503
		// 第 2 次调用（handleSingleProviderRetryInPlace 原地重试）返回 200
		responses: []*http.Response{initial503Resp, successResp},
		errors:    []error{nil, nil},
	}

	repo := &antigravityRetryStoreFixture{}
	provider := &acct.Record{
		ID:          31,
		Name:        "acc-e2e-loop",
		Type:        capability.ProviderTypeOAuth,
		Platform:    capability.PlatformAntigravity,
		Schedulable: true,
		Status:      billing.StatusActive,
		Concurrency: 1,
	}

	svc := newAntigravityRetryFixture()
	adapter, input := svc.Bind(AntigravityRetryRequest{
		Context: context.Background(), SingleProvider: true,
		Prefix:      "[test]",
		Provider:    provider,
		AccessToken: "token",
		Action:      "generateContent",
		Body:        []byte(`{"input":"test"}`),
		Do: func(req *http.Request) (*http.Response, error) {
			return upstream.Do(req, "", provider.ID, provider.Concurrency)
		},
		ModelStore: repo,
		HandleError: func(int, http.Header, []byte) {
		},
	})
	result, err := adapter.AntigravityRetryLoop(input)

	require.NoError(t, err, "should not return error on successful retry")
	require.NotNil(t, result, "should return result")
	require.NotNil(t, result.Resp, "should return response")
	require.Equal(t, http.StatusOK, result.Resp.StatusCode)

	// 验证未设模型限流
	require.Len(t, repo.modelRateLimitCalls, 0,
		"should NOT set model rate limit in single provider retry mode")
}

// TestHandleSmartRetry_URLLevelRateLimit 测试 URL 级别限流切换。
func TestHandleSmartRetry_URLLevelRateLimit(t *testing.T) {
	provider := &acct.Record{
		ID:       1,
		Name:     "acc-1",
		Type:     capability.ProviderTypeOAuth,
		Platform: capability.PlatformAntigravity,
	}

	respBody := []byte(`{"error":{"message":"Resource has been exhausted"}}`)
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader(respBody)),
	}

	params := AntigravityRetryRequest{
		Context:     context.Background(),
		Prefix:      "[test]",
		Provider:    provider,
		AccessToken: "token",
		Action:      "generateContent",
		Body:        []byte(`{"input":"test"}`),
		HandleError: func(int, http.Header, []byte) {
		},
	}

	availableURLs := []string{"https://ag-1.test", "https://ag-2.test"}

	svc := newAntigravityRetryFixture()
	adapter, input := svc.Bind(params)
	result := adapter.HandleSmartRetry(input, resp, respBody, "https://ag-1.test", 0, availableURLs)

	require.NotNil(t, result)
	require.Equal(t, antigravity.SmartRetryActionContinueURL, result.Action)
	require.Nil(t, result.Resp)
	require.Nil(t, result.Err)
	require.Nil(t, result.SwitchError)
}

func TestHandleSmartRetry_LongDelay_ReturnsSwitchError(t *testing.T) {
	repo := &antigravityRetryStoreFixture{}
	provider := &acct.Record{
		ID:       1,
		Name:     "acc-1",
		Type:     capability.ProviderTypeOAuth,
		Platform: capability.PlatformAntigravity,
	}

	// 15s >= 7s 阈值，应该返回 switchError
	respBody := []byte(`{
		"error": {
			"status": "RESOURCE_EXHAUSTED",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "claude-sonnet-4-5"}, "reason": "RATE_LIMIT_EXCEEDED"},
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "15s"}
			]
		}
	}`)
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader(respBody)),
	}

	params := AntigravityRetryRequest{
		Context:     context.Background(),
		Prefix:      "[test]",
		Provider:    provider,
		AccessToken: "token",
		Action:      "generateContent",
		Body:        []byte(`{"input":"test"}`),
		ModelStore:  repo,
		Sticky:      true,
		HandleError: func(int, http.Header, []byte) {
		},
	}

	availableURLs := []string{"https://ag-1.test"}

	svc := newAntigravityRetryFixture()
	adapter, input := svc.Bind(params)
	result := adapter.HandleSmartRetry(input, resp, respBody, "https://ag-1.test", 0, availableURLs)

	require.NotNil(t, result)
	require.Equal(t, antigravity.SmartRetryActionBreakWithResp, result.Action)
	require.Nil(t, result.Resp, "should not return resp when switchError is set")
	require.Nil(t, result.Err)
	require.NotNil(t, result.SwitchError, "should return switchError for long delay")
	require.Equal(t, provider.ID, result.SwitchError.OriginalProviderID)
	require.Equal(t, "claude-sonnet-4-5", result.SwitchError.RateLimitedModel)
	require.True(t, result.SwitchError.IsStickySession)

	// 验证模型限流已设置
	require.Len(t, repo.modelRateLimitCalls, 1)
	require.Equal(t, "claude-sonnet-4-5", repo.modelRateLimitCalls[0].modelKey)
}

func TestHandleSmartRetry_ShortDelay_SmartRetrySuccess(t *testing.T) {
	successResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(`{"result":"ok"}`)),
	}
	upstream := &mockSmartRetryUpstream{
		responses: []*http.Response{successResp},
		errors:    []error{nil},
	}

	provider := &acct.Record{
		ID:       1,
		Name:     "acc-1",
		Type:     capability.ProviderTypeOAuth,
		Platform: capability.PlatformAntigravity,
	}

	// 0.5s < 7s 阈值，应该触发智能重试
	respBody := []byte(`{
		"error": {
			"status": "RESOURCE_EXHAUSTED",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "claude-opus-4"}, "reason": "RATE_LIMIT_EXCEEDED"},
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "0.5s"}
			]
		}
	}`)
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader(respBody)),
	}

	params := AntigravityRetryRequest{
		Context:     context.Background(),
		UserAgent:   "probe-client/9.9",
		Prefix:      "[test]",
		Provider:    provider,
		AccessToken: "token",
		Action:      "generateContent",
		Body:        []byte(`{"input":"test"}`),
		Do: func(req *http.Request) (*http.Response, error) {
			return upstream.Do(req, "", provider.ID, provider.Concurrency)
		},
		HandleError: func(int, http.Header, []byte) {
		},
	}

	availableURLs := []string{"https://ag-1.test"}

	svc := newAntigravityRetryFixture()
	adapter, input := svc.Bind(params)
	result := adapter.HandleSmartRetry(input, resp, respBody, "https://ag-1.test", 0, availableURLs)

	require.NotNil(t, result)
	require.Equal(t, antigravity.SmartRetryActionBreakWithResp, result.Action)
	require.NotNil(t, result.Resp, "should return successful response")
	require.Equal(t, http.StatusOK, result.Resp.StatusCode)
	require.Nil(t, result.Err)
	require.Nil(t, result.SwitchError, "should not return switchError on success")
	require.Len(t, upstream.calls, 1, "should have made one retry call")
	require.Equal(t, "probe-client/9.9", upstream.userAgents[0])
}

func TestHandleSmartRetry_ShortDelay_SmartRetryFailed_ReturnsSwitchError(t *testing.T) {
	// 智能重试后仍然返回 429（需要提供 1 个响应，因为智能重试最多 1 次）
	failRespBody := `{
		"error": {
			"status": "RESOURCE_EXHAUSTED",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "gemini-3-flash"}, "reason": "RATE_LIMIT_EXCEEDED"},
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "0.1s"}
			]
		}
	}`
	failResp1 := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(failRespBody)),
	}
	upstream := &mockSmartRetryUpstream{
		responses: []*http.Response{failResp1},
		errors:    []error{nil},
	}

	repo := &antigravityRetryStoreFixture{}
	provider := &acct.Record{
		ID:       2,
		Name:     "acc-2",
		Type:     capability.ProviderTypeOAuth,
		Platform: capability.PlatformAntigravity,
	}

	// 3s < 7s 阈值，应该触发智能重试（最多 1 次）
	respBody := []byte(`{
		"error": {
			"status": "RESOURCE_EXHAUSTED",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "gemini-3-flash"}, "reason": "RATE_LIMIT_EXCEEDED"},
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "0.1s"}
			]
		}
	}`)
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader(respBody)),
	}

	params := AntigravityRetryRequest{
		Context:     context.Background(),
		Prefix:      "[test]",
		Provider:    provider,
		AccessToken: "token",
		Action:      "generateContent",
		Body:        []byte(`{"input":"test"}`),
		Do: func(req *http.Request) (*http.Response, error) {
			return upstream.Do(req, "", provider.ID, provider.Concurrency)
		},
		ModelStore: repo,
		Sticky:     false,
		HandleError: func(int, http.Header, []byte) {
		},
	}

	availableURLs := []string{"https://ag-1.test"}

	svc := newAntigravityRetryFixture()
	adapter, input := svc.Bind(params)
	result := adapter.HandleSmartRetry(input, resp, respBody, "https://ag-1.test", 0, availableURLs)

	require.NotNil(t, result)
	require.Equal(t, antigravity.SmartRetryActionBreakWithResp, result.Action)
	require.Nil(t, result.Resp, "should not return resp when switchError is set")
	require.Nil(t, result.Err)
	require.NotNil(t, result.SwitchError, "should return switchError after smart retry failed")
	require.Equal(t, provider.ID, result.SwitchError.OriginalProviderID)
	require.Equal(t, "gemini-3-flash", result.SwitchError.RateLimitedModel)
	require.False(t, result.SwitchError.IsStickySession)

	// 验证模型限流已设置：Gemini 同时写入精确模型和家族级 scope
	require.Len(t, repo.modelRateLimitCalls, 2)
	require.Equal(t, "gemini-3-flash", repo.modelRateLimitCalls[0].modelKey)
	require.Equal(t, "antigravity:gemini", repo.modelRateLimitCalls[1].modelKey)
	require.Len(t, upstream.calls, 1, "should have made one retry call (max attempts)")
}

func TestHandleSmartRetry_503_ModelCapacityExhausted_RetrySuccess(t *testing.T) {
	repo := &antigravityRetryStoreFixture{}
	provider := &acct.Record{
		ID:       3,
		Name:     "acc-3",
		Type:     capability.ProviderTypeOAuth,
		Platform: capability.PlatformAntigravity,
	}

	// 503 + MODEL_CAPACITY_EXHAUSTED + 39s（上游 retryDelay 应被忽略，使用固定 1s）
	respBody := []byte(`{
		"error": {
			"code": 503,
			"status": "UNAVAILABLE",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "gemini-3-pro-high"}, "reason": "MODEL_CAPACITY_EXHAUSTED"},
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "39s"}
			],
			"message": "No capacity available for model gemini-3-pro-high on the server"
		}
	}`)
	resp := &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader(respBody)),
	}

	// mock: 第 1 次重试返回 200 成功
	upstream := &mockSmartRetryUpstream{
		responses: []*http.Response{
			{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"ok":true}`))},
		},
		errors: []error{nil},
	}

	params := AntigravityRetryRequest{
		Context:     context.Background(),
		Prefix:      "[test]",
		Provider:    provider,
		AccessToken: "token",
		Action:      "generateContent",
		Body:        []byte(`{"input":"test"}`),
		ModelStore:  repo,
		Do: func(req *http.Request) (*http.Response, error) {
			return upstream.Do(req, "", provider.ID, provider.Concurrency)
		},
		Sticky: true,
		HandleError: func(int, http.Header, []byte) {
		},
	}

	availableURLs := []string{"https://ag-1.test"}

	svc := newAntigravityRetryFixture()
	adapter, input := svc.Bind(params)
	result := adapter.HandleSmartRetry(input, resp, respBody, "https://ag-1.test", 0, availableURLs)

	require.NotNil(t, result)
	require.Equal(t, antigravity.SmartRetryActionBreakWithResp, result.Action)
	require.NotNil(t, result.Resp, "should return successful response")
	require.Equal(t, http.StatusOK, result.Resp.StatusCode)
	require.Nil(t, result.Err)
	require.Nil(t, result.SwitchError, "MODEL_CAPACITY_EXHAUSTED should not return switchError")

	// 不应设置模型限流
	require.Empty(t, repo.modelRateLimitCalls, "MODEL_CAPACITY_EXHAUSTED should not set model rate limit")
	require.Len(t, upstream.calls, 1, "should have made one retry call before success")
}

func TestHandleSmartRetry_503_ModelCapacityExhausted_ContextCancel(t *testing.T) {
	repo := &antigravityRetryStoreFixture{}
	provider := &acct.Record{
		ID:       3,
		Name:     "acc-3",
		Type:     capability.ProviderTypeOAuth,
		Platform: capability.PlatformAntigravity,
	}

	respBody := []byte(`{
		"error": {
			"code": 503,
			"status": "UNAVAILABLE",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "gemini-3-pro-high"}, "reason": "MODEL_CAPACITY_EXHAUSTED"},
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "39s"}
			]
		}
	}`)
	resp := &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader(respBody)),
	}

	// 立即取消上下文，验证重试循环能正确退出
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	params := AntigravityRetryRequest{
		Context:     ctx,
		Prefix:      "[test]",
		Provider:    provider,
		AccessToken: "token",
		Action:      "generateContent",
		Body:        []byte(`{"input":"test"}`),
		ModelStore:  repo,
		HandleError: func(int, http.Header, []byte) {
		},
	}

	svc := newAntigravityRetryFixture()
	adapter, input := svc.Bind(params)
	result := adapter.HandleSmartRetry(input, resp, respBody, "https://ag-1.test", 0, []string{"https://ag-1.test"})

	require.NotNil(t, result)
	require.Equal(t, antigravity.SmartRetryActionBreakWithResp, result.Action)
	require.Error(t, result.Err, "should return context error")
	require.Nil(t, result.SwitchError, "should not return switchError on context cancel")
	require.Empty(t, repo.modelRateLimitCalls, "should not set model rate limit on context cancel")
}

func TestHandleSmartRetry_NonAntigravityProvider_ContinuesDefaultLogic(t *testing.T) {
	provider := &acct.Record{
		ID:       4,
		Name:     "acc-4",
		Type:     capability.ProviderTypeAPIKey, // 非 Antigravity 平台提供商
		Platform: capability.PlatformAnthropic,
	}

	// 即使是模型限流响应，非 OAuth 提供商也应该走默认逻辑
	respBody := []byte(`{
		"error": {
			"status": "RESOURCE_EXHAUSTED",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "claude-sonnet-4-5"}, "reason": "RATE_LIMIT_EXCEEDED"},
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "15s"}
			]
		}
	}`)
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader(respBody)),
	}

	params := AntigravityRetryRequest{
		Context:     context.Background(),
		Prefix:      "[test]",
		Provider:    provider,
		AccessToken: "token",
		Action:      "generateContent",
		Body:        []byte(`{"input":"test"}`),
		HandleError: func(int, http.Header, []byte) {
		},
	}

	availableURLs := []string{"https://ag-1.test"}

	svc := newAntigravityRetryFixture()
	adapter, input := svc.Bind(params)
	result := adapter.HandleSmartRetry(input, resp, respBody, "https://ag-1.test", 0, availableURLs)

	require.NotNil(t, result)
	require.Equal(t, antigravity.SmartRetryActionContinue, result.Action, "non-Antigravity platform provider should continue default logic")
	require.Nil(t, result.Resp)
	require.Nil(t, result.Err)
	require.Nil(t, result.SwitchError)
}

func TestHandleSmartRetry_NonModelRateLimit_ContinuesDefaultLogic(t *testing.T) {
	provider := &acct.Record{
		ID:       5,
		Name:     "acc-5",
		Type:     capability.ProviderTypeOAuth,
		Platform: capability.PlatformAntigravity,
	}

	// 429 但没有 RATE_LIMIT_EXCEEDED 或 MODEL_CAPACITY_EXHAUSTED
	respBody := []byte(`{
		"error": {
			"status": "RESOURCE_EXHAUSTED",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "5s"}
			],
			"message": "Quota exceeded"
		}
	}`)
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader(respBody)),
	}

	params := AntigravityRetryRequest{
		Context:     context.Background(),
		Prefix:      "[test]",
		Provider:    provider,
		AccessToken: "token",
		Action:      "generateContent",
		Body:        []byte(`{"input":"test"}`),
		HandleError: func(int, http.Header, []byte) {
		},
	}

	availableURLs := []string{"https://ag-1.test"}

	svc := newAntigravityRetryFixture()
	adapter, input := svc.Bind(params)
	result := adapter.HandleSmartRetry(input, resp, respBody, "https://ag-1.test", 0, availableURLs)

	require.NotNil(t, result)
	require.Equal(t, antigravity.SmartRetryActionContinue, result.Action, "non-model rate limit should continue default logic")
	require.Nil(t, result.Resp)
	require.Nil(t, result.Err)
	require.Nil(t, result.SwitchError)
}

func TestHandleSmartRetry_ExactlyAtThreshold_ReturnsSwitchError(t *testing.T) {
	repo := &antigravityRetryStoreFixture{}
	provider := &acct.Record{
		ID:       6,
		Name:     "acc-6",
		Type:     capability.ProviderTypeOAuth,
		Platform: capability.PlatformAntigravity,
	}

	// 刚好 7s = 7s 阈值，应该返回 switchError
	respBody := []byte(`{
		"error": {
			"status": "RESOURCE_EXHAUSTED",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "gemini-pro"}, "reason": "RATE_LIMIT_EXCEEDED"},
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "7s"}
			]
		}
	}`)
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader(respBody)),
	}

	params := AntigravityRetryRequest{
		Context:     context.Background(),
		Prefix:      "[test]",
		Provider:    provider,
		AccessToken: "token",
		Action:      "generateContent",
		Body:        []byte(`{"input":"test"}`),
		ModelStore:  repo,
		HandleError: func(int, http.Header, []byte) {
		},
	}

	availableURLs := []string{"https://ag-1.test"}

	svc := newAntigravityRetryFixture()
	adapter, input := svc.Bind(params)
	result := adapter.HandleSmartRetry(input, resp, respBody, "https://ag-1.test", 0, availableURLs)

	require.NotNil(t, result)
	require.Equal(t, antigravity.SmartRetryActionBreakWithResp, result.Action)
	require.Nil(t, result.Resp)
	require.NotNil(t, result.SwitchError, "exactly at threshold should return switchError")
	require.Equal(t, "gemini-pro", result.SwitchError.RateLimitedModel)
}

func TestAntigravityRetryLoop_HandleSmartRetry_SwitchError_Propagates(t *testing.T) {
	// 模拟 429 + 长延迟的响应
	respBody := []byte(`{
		"error": {
			"status": "RESOURCE_EXHAUSTED",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "claude-opus-4-6"}, "reason": "RATE_LIMIT_EXCEEDED"},
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "30s"}
			]
		}
	}`)
	rateLimitResp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader(respBody)),
	}
	upstream := &mockSmartRetryUpstream{
		responses: []*http.Response{rateLimitResp},
		errors:    []error{nil},
	}

	repo := &antigravityRetryStoreFixture{}
	provider := &acct.Record{
		ID:          7,
		Name:        "acc-7",
		Type:        capability.ProviderTypeOAuth,
		Platform:    capability.PlatformAntigravity,
		Schedulable: true,
		Status:      billing.StatusActive,
		Concurrency: 1,
	}

	svc := newAntigravityRetryFixture()
	adapter, input := svc.Bind(AntigravityRetryRequest{
		Context:     context.Background(),
		Prefix:      "[test]",
		Provider:    provider,
		AccessToken: "token",
		Action:      "generateContent",
		Body:        []byte(`{"input":"test"}`),
		Do: func(req *http.Request) (*http.Response, error) {
			return upstream.Do(req, "", provider.ID, provider.Concurrency)
		},
		ModelStore: repo,
		Sticky:     true,
		HandleError: func(int, http.Header, []byte) {
		},
	})
	result, err := adapter.AntigravityRetryLoop(input)

	require.Nil(t, result, "should not return result when switchError")
	require.NotNil(t, err, "should return error")

	var switchErr *antigravity.AntigravityProviderSwitchError
	require.ErrorAs(t, err, &switchErr, "error should be AntigravityProviderSwitchError")
	require.Equal(t, provider.ID, switchErr.OriginalProviderID)
	require.Equal(t, "claude-opus-4-6", switchErr.RateLimitedModel)
	require.True(t, switchErr.IsStickySession)
}

func TestHandleSmartRetry_NetworkError_ExhaustsRetry(t *testing.T) {
	// 唯一一次重试遇到网络错误（nil response）
	upstream := &mockSmartRetryUpstream{
		responses: []*http.Response{nil}, // 返回 nil（模拟网络错误）
		errors:    []error{nil},          // mock 不返回 error，靠 nil response 触发
	}

	repo := &antigravityRetryStoreFixture{}
	provider := &acct.Record{
		ID:       8,
		Name:     "acc-8",
		Type:     capability.ProviderTypeOAuth,
		Platform: capability.PlatformAntigravity,
	}

	// 0.1s < 7s 阈值，应该触发智能重试
	respBody := []byte(`{
		"error": {
			"status": "RESOURCE_EXHAUSTED",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "claude-sonnet-4-5"}, "reason": "RATE_LIMIT_EXCEEDED"},
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "0.1s"}
			]
		}
	}`)
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader(respBody)),
	}

	params := AntigravityRetryRequest{
		Context:     context.Background(),
		Prefix:      "[test]",
		Provider:    provider,
		AccessToken: "token",
		Action:      "generateContent",
		Body:        []byte(`{"input":"test"}`),
		Do: func(req *http.Request) (*http.Response, error) {
			return upstream.Do(req, "", provider.ID, provider.Concurrency)
		},
		ModelStore: repo,
		HandleError: func(int, http.Header, []byte) {
		},
	}

	availableURLs := []string{"https://ag-1.test"}

	svc := newAntigravityRetryFixture()
	adapter, input := svc.Bind(params)
	result := adapter.HandleSmartRetry(input, resp, respBody, "https://ag-1.test", 0, availableURLs)

	require.NotNil(t, result)
	require.Equal(t, antigravity.SmartRetryActionBreakWithResp, result.Action)
	require.Nil(t, result.Resp, "should not return resp when switchError is set")
	require.NotNil(t, result.SwitchError, "should return switchError after network error exhausted retry")
	require.Equal(t, provider.ID, result.SwitchError.OriginalProviderID)
	require.Equal(t, "claude-sonnet-4-5", result.SwitchError.RateLimitedModel)
	require.Len(t, upstream.calls, 1, "should have made one retry call")

	// 验证模型限流已设置
	require.Len(t, repo.modelRateLimitCalls, 1)
	require.Equal(t, "claude-sonnet-4-5", repo.modelRateLimitCalls[0].modelKey)
}

func TestHandleSmartRetry_NoRetryDelay_UsesDefaultRateLimit(t *testing.T) {
	repo := &antigravityRetryStoreFixture{}
	provider := &acct.Record{
		ID:       9,
		Name:     "acc-9",
		Type:     capability.ProviderTypeOAuth,
		Platform: capability.PlatformAntigravity,
	}

	// 429 + RATE_LIMIT_EXCEEDED + 无 retryDelay → 使用默认 1 分钟限流
	respBody := []byte(`{
		"error": {
			"status": "RESOURCE_EXHAUSTED",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "claude-sonnet-4-5"}, "reason": "RATE_LIMIT_EXCEEDED"}
			],
			"message": "You have exhausted your capacity on this model."
		}
	}`)
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader(respBody)),
	}

	params := AntigravityRetryRequest{
		Context:     context.Background(),
		Prefix:      "[test]",
		Provider:    provider,
		AccessToken: "token",
		Action:      "generateContent",
		Body:        []byte(`{"input":"test"}`),
		ModelStore:  repo,
		Sticky:      true,
		HandleError: func(int, http.Header, []byte) {
		},
	}

	availableURLs := []string{"https://ag-1.test"}

	svc := newAntigravityRetryFixture()
	adapter, input := svc.Bind(params)
	result := adapter.HandleSmartRetry(input, resp, respBody, "https://ag-1.test", 0, availableURLs)

	require.NotNil(t, result)
	require.Equal(t, antigravity.SmartRetryActionBreakWithResp, result.Action)
	require.Nil(t, result.Resp, "should not return resp when switchError is set")
	require.NotNil(t, result.SwitchError, "should return switchError for no retryDelay")
	require.Equal(t, "claude-sonnet-4-5", result.SwitchError.RateLimitedModel)
	require.True(t, result.SwitchError.IsStickySession)

	// 验证模型限流已设置
	require.Len(t, repo.modelRateLimitCalls, 1)
	require.Equal(t, "claude-sonnet-4-5", repo.modelRateLimitCalls[0].modelKey)
}

func TestSmartRetryMaxAttempts_VerifyConstant(t *testing.T) {
	require.Equal(t, 1, antigravity.AntigravitySmartRetryMaxAttempts,
		"antigravity.AntigravitySmartRetryMaxAttempts should be 1 to prevent repeated rate limiting")
}

func TestHandleSmartRetry_ShortDelay_StickySession_FailedRetry_ClearsSession(t *testing.T) {
	failRespBody := `{
		"error": {
			"status": "RESOURCE_EXHAUSTED",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "claude-sonnet-4-5"}, "reason": "RATE_LIMIT_EXCEEDED"},
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "0.1s"}
			]
		}
	}`
	failResp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(failRespBody)),
	}
	upstream := &mockSmartRetryUpstream{
		responses: []*http.Response{failResp},
		errors:    []error{nil},
	}

	repo := &antigravityRetryStoreFixture{}
	cache := &stubSmartRetryCache{}
	provider := &acct.Record{
		ID:       10,
		Name:     "acc-10",
		Type:     capability.ProviderTypeOAuth,
		Platform: capability.PlatformAntigravity,
	}

	respBody := []byte(`{
		"error": {
			"status": "RESOURCE_EXHAUSTED",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "claude-sonnet-4-5"}, "reason": "RATE_LIMIT_EXCEEDED"},
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "0.1s"}
			]
		}
	}`)
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader(respBody)),
	}

	params := AntigravityRetryRequest{
		Context:     context.Background(),
		Prefix:      "[test]",
		Provider:    provider,
		AccessToken: "token",
		Action:      "generateContent",
		Body:        []byte(`{"input":"test"}`),
		Do: func(req *http.Request) (*http.Response, error) {
			return upstream.Do(req, "", provider.ID, provider.Concurrency)
		},
		ModelStore:  repo,
		Sticky:      true,
		ClearSticky: func() { _ = cache.DeleteSessionProviderID(context.Background(), 42, "sticky-hash-abc") },
		HandleError: func(int, http.Header, []byte) {
		},
	}

	availableURLs := []string{"https://ag-1.test"}

	svc := newAntigravityRetryFixture()
	adapter, input := svc.Bind(params)
	result := adapter.HandleSmartRetry(input, resp, respBody, "https://ag-1.test", 0, availableURLs)

	// 验证返回 switchError
	require.NotNil(t, result)
	require.Equal(t, antigravity.SmartRetryActionBreakWithResp, result.Action)
	require.NotNil(t, result.SwitchError)
	require.True(t, result.SwitchError.IsStickySession, "switchError should carry IsStickySession=true")
	require.Equal(t, provider.ID, result.SwitchError.OriginalProviderID)

	// 核心断言：DeleteSessionProviderID 被调用，且参数正确
	require.Len(t, cache.deleteCalls, 1, "should call DeleteSessionProviderID exactly once")
	require.Equal(t, int64(42), cache.deleteCalls[0].groupID)
	require.Equal(t, "sticky-hash-abc", cache.deleteCalls[0].sessionHash)

	// 验证仅重试 1 次
	require.Len(t, upstream.calls, 1, "should make exactly 1 retry call (maxAttempts=1)")

	// 验证模型限流已设置
	require.Len(t, repo.modelRateLimitCalls, 1)
	require.Equal(t, "claude-sonnet-4-5", repo.modelRateLimitCalls[0].modelKey)
}

func TestHandleSmartRetry_ShortDelay_NonStickySession_FailedRetry_NoDeleteSession(t *testing.T) {
	failRespBody := `{
		"error": {
			"status": "RESOURCE_EXHAUSTED",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "gemini-3-flash"}, "reason": "RATE_LIMIT_EXCEEDED"},
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "0.1s"}
			]
		}
	}`
	failResp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(failRespBody)),
	}
	upstream := &mockSmartRetryUpstream{
		responses: []*http.Response{failResp},
		errors:    []error{nil},
	}

	repo := &antigravityRetryStoreFixture{}
	cache := &stubSmartRetryCache{}
	provider := &acct.Record{
		ID:       11,
		Name:     "acc-11",
		Type:     capability.ProviderTypeOAuth,
		Platform: capability.PlatformAntigravity,
	}

	respBody := []byte(`{
		"error": {
			"status": "RESOURCE_EXHAUSTED",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "gemini-3-flash"}, "reason": "RATE_LIMIT_EXCEEDED"},
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "0.1s"}
			]
		}
	}`)
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader(respBody)),
	}

	params := AntigravityRetryRequest{
		Context:     context.Background(),
		Prefix:      "[test]",
		Provider:    provider,
		AccessToken: "token",
		Action:      "generateContent",
		Body:        []byte(`{"input":"test"}`),
		Do: func(req *http.Request) (*http.Response, error) {
			return upstream.Do(req, "", provider.ID, provider.Concurrency)
		},
		ModelStore: repo,
		Sticky:     false,
		// 非粘性会话，sessionHash 为空
		HandleError: func(int, http.Header, []byte) {
		},
	}

	availableURLs := []string{"https://ag-1.test"}

	svc := newAntigravityRetryFixture()
	adapter, input := svc.Bind(params)
	result := adapter.HandleSmartRetry(input, resp, respBody, "https://ag-1.test", 0, availableURLs)

	require.NotNil(t, result)
	require.Equal(t, antigravity.SmartRetryActionBreakWithResp, result.Action)
	require.NotNil(t, result.SwitchError)
	require.False(t, result.SwitchError.IsStickySession)

	// 核心断言：sessionHash 为空时不应调用 DeleteSessionProviderID
	require.Len(t, cache.deleteCalls, 0, "should NOT call DeleteSessionProviderID when sessionHash is empty")
}

func TestHandleSmartRetry_ShortDelay_StickySession_FailedRetry_NilCache_NoPanic(t *testing.T) {
	failRespBody := `{
		"error": {
			"status": "RESOURCE_EXHAUSTED",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "claude-sonnet-4-5"}, "reason": "RATE_LIMIT_EXCEEDED"},
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "0.1s"}
			]
		}
	}`
	failResp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(failRespBody)),
	}
	upstream := &mockSmartRetryUpstream{
		responses: []*http.Response{failResp},
		errors:    []error{nil},
	}

	repo := &antigravityRetryStoreFixture{}
	provider := &acct.Record{
		ID:       12,
		Name:     "acc-12",
		Type:     capability.ProviderTypeOAuth,
		Platform: capability.PlatformAntigravity,
	}

	respBody := []byte(`{
		"error": {
			"status": "RESOURCE_EXHAUSTED",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "claude-sonnet-4-5"}, "reason": "RATE_LIMIT_EXCEEDED"},
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "0.1s"}
			]
		}
	}`)
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader(respBody)),
	}

	params := AntigravityRetryRequest{
		Context:     context.Background(),
		Prefix:      "[test]",
		Provider:    provider,
		AccessToken: "token",
		Action:      "generateContent",
		Body:        []byte(`{"input":"test"}`),
		Do: func(req *http.Request) (*http.Response, error) {
			return upstream.Do(req, "", provider.ID, provider.Concurrency)
		},
		ModelStore: repo,
		Sticky:     true,

		HandleError: func(int, http.Header, []byte) {
		},
	}

	availableURLs := []string{"https://ag-1.test"}

	// cache 为 nil，不应 panic
	svc := newAntigravityRetryFixture()
	require.NotPanics(t, func() {
		adapter, input := svc.Bind(params)
		result := adapter.HandleSmartRetry(input, resp, respBody, "https://ag-1.test", 0, availableURLs)
		require.NotNil(t, result)
		require.Equal(t, antigravity.SmartRetryActionBreakWithResp, result.Action)
		require.NotNil(t, result.SwitchError)
		require.True(t, result.SwitchError.IsStickySession)
	})
}

func TestHandleSmartRetry_ShortDelay_StickySession_SuccessRetry_NoDeleteSession(t *testing.T) {
	successResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(`{"result":"ok"}`)),
	}
	upstream := &mockSmartRetryUpstream{
		responses: []*http.Response{successResp},
		errors:    []error{nil},
	}

	cache := &stubSmartRetryCache{}
	provider := &acct.Record{
		ID:       13,
		Name:     "acc-13",
		Type:     capability.ProviderTypeOAuth,
		Platform: capability.PlatformAntigravity,
	}

	respBody := []byte(`{
		"error": {
			"status": "RESOURCE_EXHAUSTED",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "claude-opus-4"}, "reason": "RATE_LIMIT_EXCEEDED"},
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "0.5s"}
			]
		}
	}`)
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader(respBody)),
	}

	params := AntigravityRetryRequest{
		Context:     context.Background(),
		Prefix:      "[test]",
		Provider:    provider,
		AccessToken: "token",
		Action:      "generateContent",
		Body:        []byte(`{"input":"test"}`),
		Do: func(req *http.Request) (*http.Response, error) {
			return upstream.Do(req, "", provider.ID, provider.Concurrency)
		},
		Sticky:      true,
		ClearSticky: func() { _ = cache.DeleteSessionProviderID(context.Background(), 42, "sticky-hash-success") },
		HandleError: func(int, http.Header, []byte) {
		},
	}

	availableURLs := []string{"https://ag-1.test"}

	svc := newAntigravityRetryFixture()
	adapter, input := svc.Bind(params)
	result := adapter.HandleSmartRetry(input, resp, respBody, "https://ag-1.test", 0, availableURLs)

	require.NotNil(t, result)
	require.Equal(t, antigravity.SmartRetryActionBreakWithResp, result.Action)
	require.NotNil(t, result.Resp, "should return successful response")
	require.Equal(t, http.StatusOK, result.Resp.StatusCode)
	require.Nil(t, result.SwitchError, "should not return switchError on success")

	// 核心断言：重试成功时不应清除粘性会话
	require.Len(t, cache.deleteCalls, 0, "should NOT call DeleteSessionProviderID on successful retry")
}

func TestHandleSmartRetry_LongDelay_StickySession_ClearsSession(t *testing.T) {
	repo := &antigravityRetryStoreFixture{}
	cache := &stubSmartRetryCache{}
	provider := &acct.Record{
		ID:       14,
		Name:     "acc-14",
		Type:     capability.ProviderTypeOAuth,
		Platform: capability.PlatformAntigravity,
	}

	// 15s >= 7s 阈值 → 走长延迟路径
	respBody := []byte(`{
		"error": {
			"status": "RESOURCE_EXHAUSTED",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "claude-sonnet-4-5"}, "reason": "RATE_LIMIT_EXCEEDED"},
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "15s"}
			]
		}
	}`)
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader(respBody)),
	}

	params := AntigravityRetryRequest{
		Context:     context.Background(),
		Prefix:      "[test]",
		Provider:    provider,
		AccessToken: "token",
		Action:      "generateContent",
		Body:        []byte(`{"input":"test"}`),
		ModelStore:  repo,
		Sticky:      true,
		ClearSticky: func() { _ = cache.DeleteSessionProviderID(context.Background(), 42, "sticky-hash-long-delay") },
		HandleError: func(int, http.Header, []byte) {
		},
	}

	availableURLs := []string{"https://ag-1.test"}

	svc := newAntigravityRetryFixture()
	adapter, input := svc.Bind(params)
	result := adapter.HandleSmartRetry(input, resp, respBody, "https://ag-1.test", 0, availableURLs)

	require.NotNil(t, result)
	require.Equal(t, antigravity.SmartRetryActionBreakWithResp, result.Action)
	require.NotNil(t, result.SwitchError)
	require.True(t, result.SwitchError.IsStickySession)

	require.Len(t, cache.deleteCalls, 1, "long delay path should clear sticky session in handleSmartRetry")
	require.Equal(t, int64(42), cache.deleteCalls[0].groupID)
	require.Equal(t, "sticky-hash-long-delay", cache.deleteCalls[0].sessionHash)
}

func TestHandleSmartRetry_ShortDelay_NetworkError_StickySession_ClearsSession(t *testing.T) {
	upstream := &mockSmartRetryUpstream{
		responses: []*http.Response{nil}, // 网络错误
		errors:    []error{nil},
	}

	repo := &antigravityRetryStoreFixture{}
	cache := &stubSmartRetryCache{}
	provider := &acct.Record{
		ID:       15,
		Name:     "acc-15",
		Type:     capability.ProviderTypeOAuth,
		Platform: capability.PlatformAntigravity,
	}

	respBody := []byte(`{
		"error": {
			"status": "RESOURCE_EXHAUSTED",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "gemini-3-flash"}, "reason": "RATE_LIMIT_EXCEEDED"},
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "0.1s"}
			]
		}
	}`)
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader(respBody)),
	}

	params := AntigravityRetryRequest{
		Context:     context.Background(),
		Prefix:      "[test]",
		Provider:    provider,
		AccessToken: "token",
		Action:      "generateContent",
		Body:        []byte(`{"input":"test"}`),
		Do: func(req *http.Request) (*http.Response, error) {
			return upstream.Do(req, "", provider.ID, provider.Concurrency)
		},
		ModelStore:  repo,
		Sticky:      true,
		ClearSticky: func() { _ = cache.DeleteSessionProviderID(context.Background(), 99, "sticky-net-error") },
		HandleError: func(int, http.Header, []byte) {
		},
	}

	availableURLs := []string{"https://ag-1.test"}

	svc := newAntigravityRetryFixture()
	adapter, input := svc.Bind(params)
	result := adapter.HandleSmartRetry(input, resp, respBody, "https://ag-1.test", 0, availableURLs)

	require.NotNil(t, result)
	require.NotNil(t, result.SwitchError)
	require.True(t, result.SwitchError.IsStickySession)

	// 核心断言：网络错误耗尽重试后也应清除粘性绑定
	require.Len(t, cache.deleteCalls, 1, "should call DeleteSessionProviderID after network error exhausts retry")
	require.Equal(t, int64(99), cache.deleteCalls[0].groupID)
	require.Equal(t, "sticky-net-error", cache.deleteCalls[0].sessionHash)

	require.Len(t, repo.modelRateLimitCalls, 2)
	require.Equal(t, "gemini-3-flash", repo.modelRateLimitCalls[0].modelKey)
	require.Equal(t, "antigravity:gemini", repo.modelRateLimitCalls[1].modelKey)
}

func TestHandleSmartRetry_ShortDelay_503_StickySession_FailedRetry_ClearsSession(t *testing.T) {
	failRespBody := `{
		"error": {
			"code": 429,
			"status": "RESOURCE_EXHAUSTED",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "gemini-3-pro"}, "reason": "RATE_LIMIT_EXCEEDED"},
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "0.5s"}
			]
		}
	}`
	failResp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(failRespBody)),
	}
	upstream := &mockSmartRetryUpstream{
		responses: []*http.Response{failResp},
		errors:    []error{nil},
	}

	repo := &antigravityRetryStoreFixture{}
	cache := &stubSmartRetryCache{}
	provider := &acct.Record{
		ID:       16,
		Name:     "acc-16",
		Type:     capability.ProviderTypeOAuth,
		Platform: capability.PlatformAntigravity,
	}

	respBody := []byte(`{
		"error": {
			"code": 429,
			"status": "RESOURCE_EXHAUSTED",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "gemini-3-pro"}, "reason": "RATE_LIMIT_EXCEEDED"},
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "0.5s"}
			]
		}
	}`)
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader(respBody)),
	}

	params := AntigravityRetryRequest{
		Context:     context.Background(),
		Prefix:      "[test]",
		Provider:    provider,
		AccessToken: "token",
		Action:      "generateContent",
		Body:        []byte(`{"input":"test"}`),
		Do: func(req *http.Request) (*http.Response, error) {
			return upstream.Do(req, "", provider.ID, provider.Concurrency)
		},
		ModelStore:  repo,
		Sticky:      true,
		ClearSticky: func() { _ = cache.DeleteSessionProviderID(context.Background(), 77, "sticky-503-short") },
		HandleError: func(int, http.Header, []byte) {
		},
	}

	availableURLs := []string{"https://ag-1.test"}

	svc := newAntigravityRetryFixture()
	adapter, input := svc.Bind(params)
	result := adapter.HandleSmartRetry(input, resp, respBody, "https://ag-1.test", 0, availableURLs)

	require.NotNil(t, result)
	require.NotNil(t, result.SwitchError)
	require.True(t, result.SwitchError.IsStickySession)

	// 验证粘性绑定被清除
	require.Len(t, cache.deleteCalls, 1)
	require.Equal(t, int64(77), cache.deleteCalls[0].groupID)
	require.Equal(t, "sticky-503-short", cache.deleteCalls[0].sessionHash)

	// 验证模型限流已设置：Gemini 同时写入精确模型和家族级 scope
	require.Len(t, repo.modelRateLimitCalls, 2)
	require.Equal(t, "gemini-3-pro", repo.modelRateLimitCalls[0].modelKey)
	require.Equal(t, "antigravity:gemini", repo.modelRateLimitCalls[1].modelKey)
}

func TestAntigravityRetryLoop_SmartRetryFailed_StickySession_SwitchErrorPropagates(t *testing.T) {
	// 初始 429 响应
	initialRespBody := []byte(`{
		"error": {
			"status": "RESOURCE_EXHAUSTED",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "claude-opus-4-6"}, "reason": "RATE_LIMIT_EXCEEDED"},
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "0.1s"}
			]
		}
	}`)
	initialResp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader(initialRespBody)),
	}

	// 智能重试也返回 429
	retryRespBody := `{
		"error": {
			"status": "RESOURCE_EXHAUSTED",
			"details": [
				{"@type": "type.googleapis.com/google.rpc.ErrorInfo", "metadata": {"model": "claude-opus-4-6"}, "reason": "RATE_LIMIT_EXCEEDED"},
				{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "0.1s"}
			]
		}
	}`
	retryResp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(retryRespBody)),
	}

	upstream := &mockSmartRetryUpstream{
		responses: []*http.Response{initialResp, retryResp},
		errors:    []error{nil, nil},
	}

	repo := &antigravityRetryStoreFixture{}
	cache := &stubSmartRetryCache{}
	provider := &acct.Record{
		ID:          17,
		Name:        "acc-17",
		Type:        capability.ProviderTypeOAuth,
		Platform:    capability.PlatformAntigravity,
		Schedulable: true,
		Status:      billing.StatusActive,
		Concurrency: 1,
	}

	svc := newAntigravityRetryFixture()
	adapter, input := svc.Bind(AntigravityRetryRequest{
		Context:     context.Background(),
		Prefix:      "[test]",
		Provider:    provider,
		AccessToken: "token",
		Action:      "generateContent",
		Body:        []byte(`{"input":"test"}`),
		Do: func(req *http.Request) (*http.Response, error) {
			return upstream.Do(req, "", provider.ID, provider.Concurrency)
		},
		ModelStore:  repo,
		Sticky:      true,
		ClearSticky: func() { _ = cache.DeleteSessionProviderID(context.Background(), 55, "sticky-loop-test") },
		HandleError: func(int, http.Header, []byte) {
		},
	})
	result, err := adapter.AntigravityRetryLoop(input)

	require.Nil(t, result, "should not return result when switchError")
	require.NotNil(t, err, "should return error")

	var switchErr *antigravity.AntigravityProviderSwitchError
	require.ErrorAs(t, err, &switchErr, "error should be AntigravityProviderSwitchError")
	require.Equal(t, provider.ID, switchErr.OriginalProviderID)
	require.Equal(t, "claude-opus-4-6", switchErr.RateLimitedModel)
	require.True(t, switchErr.IsStickySession, "IsStickySession must propagate through retryLoop")

	// 验证粘性绑定被清除
	require.Len(t, cache.deleteCalls, 1, "should clear sticky session in handleSmartRetry")
	require.Equal(t, int64(55), cache.deleteCalls[0].groupID)
	require.Equal(t, "sticky-loop-test", cache.deleteCalls[0].sessionHash)
}

func (u *epFixedUpstream) Do(req *http.Request, proxyURL string, providerID int64, providerConcurrency int) (*http.Response, error) {
	u.calls++
	return &http.Response{
		StatusCode: u.statusCode,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(u.body)),
	}, nil
}

func (u *epFixedUpstream) DoWithTLS(req *http.Request, proxyURL string, providerID int64, providerConcurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, providerID, providerConcurrency)
}

func (r *epProviderRepo) SetTempUnschedulable(_ context.Context, _ int64, _ time.Time, _ string) error {
	r.tempCalls++
	return nil
}

func (r *epProviderRepo) SetError(_ context.Context, _ int64, _ string) error {
	r.setErrCalls++
	return nil
}

func saveAndSetBaseURLs(t *testing.T) {
	t.Helper()
	oldBaseURLs := append([]string(nil), antigravity.BaseURLs...)
	oldAvail := antigravity.DefaultURLAvailability
	antigravity.BaseURLs = []string{"https://ep-test.example"}
	antigravity.DefaultURLAvailability = antigravity.NewURLAvailability(time.Minute)
	t.Cleanup(func() {
		antigravity.BaseURLs = oldBaseURLs
		antigravity.DefaultURLAvailability = oldAvail
	})
}

func newRetryParams(provider *acct.Record, upstream *epFixedUpstream, handleError func(int, http.Header, []byte)) AntigravityRetryRequest {
	return AntigravityRetryRequest{
		Context:     context.Background(),
		Prefix:      "[ep-test]",
		Provider:    provider,
		AccessToken: "token",
		Action:      "generateContent",
		Body:        []byte(`{"input":"test"}`),
		Do: func(req *http.Request) (*http.Response, error) {
			return upstream.Do(req, "", provider.ID, provider.Concurrency)
		},
		RequestedModel: "claude-sonnet-4-5",
		HandleError:    handleError,
	}
}

func (r *epTrackingRepo) SetRateLimited(_ context.Context, id int64, _ time.Time) error {
	r.rateLimitedCalls++
	r.rateLimitedID = id
	return nil
}

func (r *epTrackingRepo) SetError(_ context.Context, id int64, _ string) error {
	r.setErrCalls++
	r.setErrID = id
	return nil
}

func (r *epTrackingRepo) SetTempUnschedulable(_ context.Context, _ int64, _ time.Time, _ string) error {
	r.tempCalls++
	return nil
}

// SetModelRateLimit 返回成功，测试分别检查提供商切换和状态写入次数。
func (r *epProviderRepo) SetModelRateLimit(context.Context, int64, string, time.Time, ...string) error {
	return nil
}

func (r *epTrackingRepo) SetModelRateLimit(context.Context, int64, string, time.Time, ...string) error {
	return nil
}

func (s *antigravityFamilyStoreFixture) SetModelRateLimit(_ context.Context, _ int64, key string, _ time.Time, _ ...string) error {
	s.modelRateLimitCalls = append(s.modelRateLimitCalls, struct{ modelKey string }{key})
	return nil
}

func (s *antigravityPolicyStoreFixture) SetTempUnschedulable(context.Context, int64, time.Time, string) error {
	s.tempCalls++
	return nil
}

func (s *antigravityPolicyStoreFixture) SetModelRateLimit(_ context.Context, _ int64, key string, _ time.Time, _ ...string) error {
	s.modelRateLimitCalls = append(s.modelRateLimitCalls, struct{ scope, modelKey string }{key, key})
	return nil
}

func (s *antigravityPolicyStoreFixture) UpdateExtra(context.Context, int64, map[string]any) error {
	return nil
}

func newAntigravityPolicyFixture(store *antigravityPolicyStoreFixture, policy *acct.HealthService) *AntigravityRetry {
	return &AntigravityRetry{Policy: policy, Health: &acct.AntigravityHealth{Store: store, ModelKeys: AntigravityModelLimitKeys, Info: func(string, ...any) {}, Logf: func(string, ...any) {}}}
}

func (s *stubAntigravityUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	url := req.URL.String()
	s.calls = append(s.calls, url)
	if strings.HasPrefix(url, s.firstBase) {
		return &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"Resource has been exhausted"}}`))}, nil
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("ok"))}, nil
}

func (r *recordingOKUpstream) Do(*http.Request, string, int64, int) (*http.Response, error) {
	r.calls++
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("ok"))}, nil
}

func (c *stubSmartRetryCache) DeleteSessionProviderID(_ context.Context, groupID int64, sessionHash string) error {
	c.deleteCalls = append(c.deleteCalls, deleteSessionCall{groupID: groupID, sessionHash: sessionHash})
	return nil
}

func (m *mockSmartRetryUpstream) Do(req *http.Request, proxyURL string, providerID int64, providerConcurrency int) (*http.Response, error) {
	idx := m.callIdx
	m.calls = append(m.calls, req.URL.String())
	m.userAgents = append(m.userAgents, req.Header.Get("User-Agent"))
	if req != nil && req.Body != nil {
		body, _ := io.ReadAll(req.Body)
		m.requestBodies = append(m.requestBodies, body)
		req.Body = io.NopCloser(bytes.NewReader(body))
	} else {
		m.requestBodies = append(m.requestBodies, nil)
	}
	m.callIdx++

	// 确定使用哪个索引
	respIdx := idx
	if respIdx >= len(m.responses) {
		if !m.repeatLast || len(m.responses) == 0 {
			return nil, nil
		}
		respIdx = len(m.responses) - 1
	}

	resp := m.responses[respIdx]
	respErr := m.errors[respIdx]
	if resp == nil {
		return nil, respErr
	}

	// 首次调用时缓存 body 字节
	if respIdx >= len(m.responseBodies) {
		for len(m.responseBodies) <= respIdx {
			m.responseBodies = append(m.responseBodies, nil)
		}
	}
	if m.responseBodies[respIdx] == nil && resp.Body != nil {
		bodyBytes, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		m.responseBodies[respIdx] = bodyBytes
	}

	// 用缓存的 body 重建 reader（支持重试场景多次读取）
	cloned := *resp
	if m.responseBodies[respIdx] != nil {
		cloned.Body = io.NopCloser(bytes.NewReader(m.responseBodies[respIdx]))
	}
	return &cloned, respErr
}

func (m *mockSmartRetryUpstream) DoWithTLS(req *http.Request, proxyURL string, providerID int64, providerConcurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	return m.Do(req, proxyURL, providerID, providerConcurrency)
}

// newAntigravityRetryFixture 组合平台重试组件和健康状态接口。
func newAntigravityRetryFixture() *AntigravityRetry {
	noop := func(string, ...any) {}
	return &AntigravityRetry{
		SafeURL: logredact.SafeUpstreamURL, TruncateString: logredact.TruncateUTF8, Health: &acct.AntigravityHealth{ModelKeys: AntigravityModelLimitKeys, Logf: noop, Info: noop, Warn: noop, Error: noop},
		BaseURL: func(v *acct.Record) string {
			return antigravity.ResolveAntigravityForwardBaseURL("", AntigravityPaidTier(v))
		}, BodyLimit: func() int64 { return 512 << 10 },
	}
}

func (s *antigravityRetryStoreFixture) SetModelRateLimit(_ context.Context, id int64, key string, at time.Time, _ ...string) error {
	s.modelRateLimitCalls = append(s.modelRateLimitCalls, struct {
		providerID int64
		modelKey   string
		resetAt    time.Time
	}{id, key, at})
	return nil
}

func (s *antigravityRetryStoreFixture) UpdateExtra(_ context.Context, _ int64, value map[string]any) error {
	s.extraUpdateCalls = append(s.extraUpdateCalls, value)
	return nil
}
