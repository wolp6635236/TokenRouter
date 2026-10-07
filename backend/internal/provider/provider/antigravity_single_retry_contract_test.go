package provider

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	acct "github.com/TokenFlux/TokenRouter/internal/provider"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/upstream/antigravity"
	"github.com/stretchr/testify/require"
)

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
// → 不设模型限流、不切换提供商，改为原地重试
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
// → 照常设模型限流 + 切换提供商
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
// → 单提供商原地重试仅针对 503，429 依然走切换提供商逻辑
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

// TestHandleSingleProviderRetryInPlace_Success 原地重试成功
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

// TestHandleSingleProviderRetryInPlace_AllRetriesFail 所有重试都失败，返回 503（不设限流）
func TestHandleSingleProviderRetryInPlace_AllRetriesFail(t *testing.T) {
	// 构造 3 个 503 响应（对应 3 次原地重试）
	var responses []*http.Response
	var errors []error
	for i := 0; i < antigravity.AntigravitySingleProviderSmartRetryMaxAttempts; i++ {
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

// TestHandleSingleProviderRetryInPlace_WaitDurationClamped 等待时间被限制在 [min, max] 范围
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

// TestHandleSingleProviderRetryInPlace_ContextCanceled context 取消时立即返回
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

// TestHandleSingleProviderRetryInPlace_NetworkError_ContinuesRetry 网络错误时继续重试
func TestHandleSingleProviderRetryInPlace_NetworkError_ContinuesRetry(t *testing.T) {
	successResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(`{"result":"ok"}`)),
	}
	upstream := &mockSmartRetryUpstream{
		// 第1次网络错误（nil resp），第2次成功
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
// 预检查中，如果有 SingleProviderRetry 标记，即使提供商已限流也跳过直接发请求
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
// 对照组：无 SingleProviderRetry + 已限流 → 预检查返回 switchError
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
// 端到端场景：503 + 单提供商 + 原地重试第2次成功
func TestHandleSmartRetry_503_SingleProvider_RetryInPlace_ThenSuccess_E2E(t *testing.T) {
	// 第1次原地重试仍返回 503，第2次成功
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
// 通过 antigravityRetryLoop → handleSmartRetry → handleSingleProviderRetryInPlace 完整链路
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
		// 第1次调用（retryLoop 主循环）返回 503
		// 第2次调用（handleSingleProviderRetryInPlace 原地重试）返回 200
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

// 预检查测试记录是否发送请求，成功响应使用固定报文。
type recordingOKUpstream struct{ calls int }

func (r *recordingOKUpstream) Do(*http.Request, string, int64, int) (*http.Response, error) {
	r.calls++
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("ok"))}, nil
}
