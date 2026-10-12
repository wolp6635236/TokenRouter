package googleforward_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	forwardcore "github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	gatewayhttp "github.com/TokenFlux/TokenRouter/internal/gateway/httpapi"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/provider/googleforward"
	"github.com/TokenFlux/TokenRouter/internal/ops"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/upstream/antigravity"
)

func TestIsPromptTooLongError(t *testing.T) {
	require.True(t, antigravity.IsPromptTooLongError([]byte(`{"error":{"message":"Prompt is too long"}}`)))
	require.True(t, antigravity.IsPromptTooLongError([]byte(`{"message":"Prompt is too long"}`)))
	require.False(t, antigravity.IsPromptTooLongError([]byte(`{"error":{"message":"other"}}`)))
}

func TestAntigravityGatewayService_Forward_PromptTooLong(t *testing.T) {
	writer := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(writer)

	body, err := json.Marshal(map[string]any{
		"model": "claude-opus-4-6-thinking",

		"messages": []map[string]any{
			{"role": "user", "content": "hi"},
		},

		"max_tokens": 1,

		"stream": false,
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	c.Request = req

	respBody := []byte(`{"error":{"message":"Prompt is too long"}}`)
	resp := &http.Response{
		StatusCode: http.StatusBadRequest,

		Header: http.Header{"X-Request-Id": []string{"req-1"}},

		Body: io.NopCloser(bytes.NewReader(respBody)),
	}

	svc := newAntigravityFixture(antigravityDependencies{
		settingService: newExecutionReadersFixture(&antigravitySettingRepoStub{}), options: fixtureOptions(&googleforward.Options{MaxLineSize: 500 * 1024 * 1024}),

		tokenProvider: newAntigravityTokenSourceForTest(nil),

		httpUpstream: &httpUpstreamStub{resp: resp},
	})

	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation,
			ID:           1,

			Name: "acc-1",

			Platform: capability.PlatformAntigravity,

			Type: capability.ProviderTypeOAuth,

			Status: billing.StatusActive,

			Concurrency: 1,

			Credentials: map[string]any{
				"access_token": "token",
				"project_id":   "proj",
			},
		},
	}

	result, err := svc.Forward(context.Background(), gatewayhttp.NewGoogleBoundary(c, svc.Options, true), provider, body, false)
	require.Nil(t, result)

	var promptErr *antigravity.PromptTooLongError
	require.ErrorAs(t, err, &promptErr)
	require.Equal(t, http.StatusBadRequest, promptErr.StatusCode)
	require.Equal(t, "req-1", promptErr.RequestID)
	require.NotEmpty(t, promptErr.Body)

	raw, ok := c.Get(gatewayhttp.OpsUpstreamErrorsKey)
	require.True(t, ok)
	events, ok := raw.([]*ops.OpsUpstreamErrorEvent)
	require.True(t, ok)
	require.Len(t, events, 1)
	require.Equal(t, "prompt_too_long", events[0].Kind)
}

// TestAntigravityGatewayService_Forward_ModelRateLimitTriggersFailover
// 验证：当提供商存在模型限流且剩余时间 >= antigravityRateLimitThreshold 时，
// Forward 方法应返回 UpstreamFailoverError，触发 Handler 切换提供商。
func TestAntigravityGatewayService_Forward_ModelRateLimitTriggersFailover(t *testing.T) {
	writer := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(writer)

	body, err := json.Marshal(map[string]any{
		"model": "claude-opus-4-6-thinking",

		"messages": []map[string]any{
			{"role": "user", "content": "hi"},
		},

		"max_tokens": 1,

		"stream": false,
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	c.Request = req

	// 不需要真正调用上游，因为预检查会直接返回切换信号
	svc := newAntigravityFixture(antigravityDependencies{
		tokenProvider: newAntigravityTokenSourceForTest(nil),

		httpUpstream: &httpUpstreamStub{resp: nil, err: nil},
	})

	// 设置模型限流：剩余时间 30 秒（> antigravityRateLimitThreshold 7s）
	futureResetAt := time.Now().Add(30 * time.Second).Format(time.RFC3339)
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation,
			ID:           1,

			Name: "acc-rate-limited",

			Platform: capability.PlatformAntigravity,

			Type: capability.ProviderTypeOAuth,

			Status: billing.StatusActive,

			Concurrency: 1,

			Credentials: map[string]any{
				"access_token": "token",
				"project_id":   "proj",
			},

			Extra: map[string]any{
				"model_rate_limits": map[string]any{
					"claude-opus-4-6-thinking": map[string]any{
						"rate_limit_reset_at": futureResetAt,
					},
				},
			},
		},
	}

	result, err := svc.Forward(context.Background(), gatewayhttp.NewGoogleBoundary(c, svc.Options, true), provider, body, false)
	require.Nil(t, result, "Forward should not return result when model rate limited")
	require.NotNil(t, err, "Forward should return error")

	// 检查错误类型是否为 UpstreamFailoverError。
	var failoverErr *forwardcore.UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr, "error should be UpstreamFailoverError to trigger provider switch")
	require.Equal(t, http.StatusServiceUnavailable, failoverErr.StatusCode)
	// 非粘性会话请求，ForceCacheBilling 应为 false
	require.False(t, failoverErr.ForceCacheBilling, "ForceCacheBilling should be false for non-sticky session")
}

// TestAntigravityGatewayService_Forward_StickySessionForceCacheBilling
// 验证：粘性会话切换时，UpstreamFailoverError.ForceCacheBilling 应为 true。
func TestAntigravityGatewayService_Forward_StickySessionForceCacheBilling(t *testing.T) {
	writer := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(writer)

	body, err := json.Marshal(map[string]any{
		"model":    "claude-opus-4-6-thinking",
		"messages": []map[string]string{{"role": "user", "content": "hello"}},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	c.Request = req

	svc := newAntigravityFixture(antigravityDependencies{
		tokenProvider: newAntigravityTokenSourceForTest(nil),

		httpUpstream: &httpUpstreamStub{resp: nil, err: nil},
	})

	// 设置模型限流：剩余时间 30 秒（> antigravityRateLimitThreshold 7s）
	futureResetAt := time.Now().Add(30 * time.Second).Format(time.RFC3339)
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation,
			ID:           3,

			Name: "acc-sticky-rate-limited",

			Platform: capability.PlatformAntigravity,

			Type: capability.ProviderTypeOAuth,

			Status: billing.StatusActive,

			Concurrency: 1,

			Credentials: map[string]any{
				"access_token": "token",
				"project_id":   "proj",
			},

			Extra: map[string]any{
				"model_rate_limits": map[string]any{
					"claude-opus-4-6-thinking": map[string]any{
						"rate_limit_reset_at": futureResetAt,
					},
				},
			},
		},
	}

	// 传入 isStickySession = true
	result, err := svc.Forward(context.Background(), gatewayhttp.NewGoogleBoundary(c, svc.Options, true), provider, body, true)
	require.Nil(t, result, "Forward should not return result when model rate limited")
	require.NotNil(t, err, "Forward should return error")

	// 核心验证：粘性会话切换时，ForceCacheBilling 应为 true
	var failoverErr *forwardcore.UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr, "error should be UpstreamFailoverError to trigger provider switch")
	require.Equal(t, http.StatusServiceUnavailable, failoverErr.StatusCode)
	require.True(t, failoverErr.ForceCacheBilling, "ForceCacheBilling should be true for sticky session switch")
}

// TestAntigravityGatewayService_Forward_BillsWithMappedModel
// 验证：Antigravity Claude 转发返回的计费模型使用映射后的模型。
func TestAntigravityGatewayService_Forward_BillsWithMappedModel(t *testing.T) {
	writer := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(writer)

	body, err := json.Marshal(map[string]any{
		"model": "claude-sonnet-4-5",

		"messages": []map[string]any{
			{"role": "user", "content": "hello"},
		},

		"max_tokens": 16,

		"stream": true,
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	c.Request = req

	upstreamBody := []byte("data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":8,\"candidatesTokenCount\":3}}}\n\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,

		Header: http.Header{"X-Request-Id": []string{"req-bill-1"}},

		Body: io.NopCloser(bytes.NewReader(upstreamBody)),
	}

	svc := newAntigravityFixture(antigravityDependencies{
		settingService: newExecutionReadersFixture(&antigravitySettingRepoStub{}), options: fixtureOptions(&googleforward.Options{MaxLineSize: 500 * 1024 * 1024}),

		tokenProvider: newAntigravityTokenSourceForTest(nil),

		httpUpstream: &httpUpstreamStub{resp: resp},
	})

	const mappedModel = "gemini-3-pro-high"
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation,
			ID:           5,

			Name: "acc-forward-billing",

			Platform: capability.PlatformAntigravity,

			Type: capability.ProviderTypeOAuth,

			Status: billing.StatusActive,

			Concurrency: 1,

			Credentials: map[string]any{
				"access_token": "token",

				"project_id": "proj",

				"model_mapping": map[string]any{
					"claude-sonnet-4-5": mappedModel,
				},
			},
		},
	}

	result, err := svc.Forward(context.Background(), gatewayhttp.NewGoogleBoundary(c, svc.Options, true), provider, body, false)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "claude-sonnet-4-5", result.Model)
	require.Equal(t, mappedModel, result.UpstreamModel)
}
