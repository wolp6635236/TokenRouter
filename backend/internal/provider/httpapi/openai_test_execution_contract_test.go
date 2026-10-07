package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/egress"
	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	upstreamcore "github.com/TokenFlux/TokenRouter/internal/upstream"

	"github.com/TokenFlux/TokenRouter/internal/upstream/openai"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// --- shared test helpers ---

// --- test functions ---

func TestProviderTestService_OpenAISuccessPersistsSnapshotFromHeaders(t *testing.T) {
	ctx, recorder := newTestContext()

	resp := newJSONResponse(http.StatusOK, "")
	resp.Body = io.NopCloser(strings.NewReader(`data: {"type":"response.completed"}

`))
	resp.Header.Set("x-codex-primary-used-percent", "88")
	resp.Header.Set("x-codex-primary-reset-after-seconds", "604800")
	resp.Header.Set("x-codex-primary-window-minutes", "10080")
	resp.Header.Set("x-codex-secondary-used-percent", "42")
	resp.Header.Set("x-codex-secondary-reset-after-seconds", "18000")
	resp.Header.Set("x-codex-secondary-window-minutes", "300")

	repo := &openAIProbeStore{}
	upstream := &queuedHTTPUpstream{responses: []*http.Response{resp}}
	svc := &provideradapter.OpenAIProviderTest{Store: repo, Transport: upstream}
	provider := &providercore.Record{
		ID:          89,
		Platform:    capability.PlatformOpenAI,
		Type:        capability.ProviderTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "test-token"},
	}

	err := executeOpenAIProbe(t, svc, ctx, provider, "gpt-5.4", "", "")
	require.NoError(t, err)
	require.Len(t, upstream.requests, 1)
	req := upstream.requests[0]
	require.Equal(t, upstreamcore.HTTPUpstreamProfileOpenAI, upstreamcore.HTTPUpstreamProfileFromContext(req.Context()))
	require.Equal(t, "responses=experimental", req.Header.Get("OpenAI-Beta"))
	require.Equal(t, openai.CodexDefaultOriginator, req.Header.Get("Originator"))
	require.Equal(t, openai.CodexCLIUserAgent, req.Header.Get("User-Agent"))
	require.NotEmpty(t, repo.updatedExtra)
	require.Equal(t, 42.0, repo.updatedExtra["codex_5h_used_percent"])
	require.Equal(t, 88.0, repo.updatedExtra["codex_7d_used_percent"])
	require.Contains(t, recorder.Body.String(), "test_complete")
}

// TestProviderTestService_OpenAIOAuthTestDoesNotRedirectBareGPT56 检查管理员测试未知模型名时按输入透传。
func TestProviderTestService_OpenAIOAuthTestDoesNotRedirectBareGPT56(t *testing.T) {
	ctx, _ := newTestContext()

	resp := newJSONResponse(http.StatusOK, "")
	resp.Body = io.NopCloser(strings.NewReader(`data: {"type":"response.completed"}

`))

	upstream := &queuedHTTPUpstream{responses: []*http.Response{resp}}
	svc := &provideradapter.OpenAIProviderTest{Transport: upstream}
	provider := &providercore.Record{
		ID:          90,
		Platform:    capability.PlatformOpenAI,
		Type:        capability.ProviderTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "test-token"},
	}

	err := executeOpenAIProbe(t, svc, ctx, provider, "gpt-5.6", "connection probe", "")
	require.NoError(t, err)
	require.Len(t, upstream.requests, 1)

	body, err := io.ReadAll(upstream.requests[0].Body)
	require.NoError(t, err)
	require.Equal(t, "gpt-5.6", gjson.GetBytes(body, "model").String())
	require.Equal(t, "connection probe", gjson.GetBytes(body, "input.0.content.0.text").String())
}

func TestProviderTestService_OpenAIOAuthUsesConfiguredUserAgent(t *testing.T) {
	ctx, _ := newTestContext()

	resp := newJSONResponse(http.StatusOK, "")
	resp.Body = io.NopCloser(strings.NewReader(`data: {"type":"response.completed"}

`))
	upstream := &queuedHTTPUpstream{responses: []*http.Response{resp}}
	svc := &provideradapter.OpenAIProviderTest{Transport: upstream}
	provider := &providercore.Record{
		ID:          890,
		Platform:    capability.PlatformOpenAI,
		Type:        capability.ProviderTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token": "test-token",
			"user_agent":   "codex-tui/9.9.0 test-terminal",
		},
	}

	err := executeOpenAIProbe(t, svc, ctx, provider, "gpt-5.4", "", "")
	require.NoError(t, err)
	require.Len(t, upstream.requests, 1)
	require.Equal(t, "codex-tui/9.9.0 test-terminal", upstream.requests[0].Header.Get("User-Agent"))
	require.Equal(t, "codex-tui", upstream.requests[0].Header.Get("Originator"))
}

func TestProviderTestService_OpenAIShadowUsesParentCredentialsAndShadowModel(t *testing.T) {
	ctx, recorder := newTestContext()

	resp := newJSONResponse(http.StatusOK, "")
	resp.Body = io.NopCloser(strings.NewReader(`data: {"type":"response.completed"}

`))

	parentID := int64(100)
	parent := &providercore.Record{
		ID:       parentID,
		Platform: capability.PlatformOpenAI,
		Type:     capability.ProviderTypeOAuth,
		Status:   billing.StatusActive,
		Credentials: map[string]any{
			"access_token":       "parent-token",
			"chatgpt_account_id": "org-parent",
		},
	}
	shadow := &providercore.Record{
		ID:               200,
		Platform:         capability.PlatformOpenAI,
		Type:             capability.ProviderTypeOAuth,
		Status:           billing.StatusActive,
		ParentProviderID: &parentID,
		QuotaDimension:   providercore.QuotaDimensionSpark,
		Concurrency:      2,
		Credentials: map[string]any{
			"model_mapping": map[string]any{
				"gpt-5.3-codex-spark": "gpt-5.3-codex-spark",
			},
		},
	}

	repo := &openAIProbeStore{
		openAIProbeRecords: openAIProbeRecords{
			providersByID: map[int64]*providercore.Record{
				parentID: parent,
				200:      shadow,
			},
		},
	}
	upstream := &queuedHTTPUpstream{responses: []*http.Response{resp}}
	svc := &provideradapter.OpenAIProviderTest{Store: repo, Transport: upstream}

	err := executeOpenAIProbeRequest(t, svc, ctx, shadow.ID, "gpt-5.3-codex-spark", "", "")
	require.NoError(t, err)
	require.Len(t, upstream.requests, 1)
	req := upstream.requests[0]
	require.Equal(t, "Bearer parent-token", req.Header.Get("Authorization"))
	require.Equal(t, "org-parent", req.Header.Get("chatgpt-account-id"))
	body, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	require.Equal(t, "gpt-5.3-codex-spark", gjson.GetBytes(body, "model").String())
	require.Contains(t, recorder.Body.String(), `"success":true`)
}

func TestProviderTestService_OpenAIStreamEOFBeforeCompletedFails(t *testing.T) {
	ctx, recorder := newTestContext()

	resp := newJSONResponse(http.StatusOK, "")
	resp.Body = io.NopCloser(strings.NewReader(`data: {"type":"response.output_text.delta","delta":"hi"}

`))

	upstream := &queuedHTTPUpstream{responses: []*http.Response{resp}}
	svc := &provideradapter.OpenAIProviderTest{Transport: upstream}
	provider := &providercore.Record{
		ID:          90,
		Platform:    capability.PlatformOpenAI,
		Type:        capability.ProviderTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "test-token"},
	}

	err := executeOpenAIProbe(t, svc, ctx, provider, "gpt-5.4", "", "")
	require.Error(t, err)
	require.Contains(t, recorder.Body.String(), "response.completed")
	require.NotContains(t, recorder.Body.String(), `"success":true`)
}

func TestProviderTestService_RunTestBackgroundWithPromptAndUserAgentOverridesHeader(t *testing.T) {
	resp := newJSONResponse(http.StatusOK, "")
	resp.Body = io.NopCloser(strings.NewReader(`data: {"type":"response.completed"}

`))
	provider := providercore.Record{
		ID:          901,
		Platform:    capability.PlatformOpenAI,
		Type:        capability.ProviderTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token": "test-token",
		},
	}
	repo := &openAIProbeStore{openAIProbeRecords: openAIProbeRecords{providersByID: map[int64]*providercore.Record{provider.ID: &provider}}}
	upstream := &queuedHTTPUpstream{responses: []*http.Response{resp}}
	svc := &provideradapter.OpenAIProviderTest{
		Store:     repo,
		Transport: upstream,
	}

	result, err := openAIProbeCore(svc).RunTestBackgroundWithPromptAndUserAgent(context.Background(), provider.ID, "gpt-5.4", "hi", "codex-tui/9.9.1 test-terminal")

	require.NoError(t, err)
	require.Equal(t, "success", result.Status)
	require.Len(t, upstream.requests, 1)
	require.Equal(t, "codex-tui/9.9.1 test-terminal", upstream.requests[0].Header.Get("User-Agent"))
	require.Equal(t, "codex-tui", upstream.requests[0].Header.Get("Originator"))
}

func TestProviderTestService_OpenAI429PersistsSnapshotAndRateLimitState(t *testing.T) {
	ctx, _ := newTestContext()

	resp := newJSONResponse(http.StatusTooManyRequests, `{"error":{"type":"usage_limit_reached","message":"limit reached","resets_at":1777283883}}`)
	resp.Header.Set("x-codex-primary-used-percent", "100")
	resp.Header.Set("x-codex-primary-reset-after-seconds", "604800")
	resp.Header.Set("x-codex-primary-window-minutes", "10080")
	resp.Header.Set("x-codex-secondary-used-percent", "100")
	resp.Header.Set("x-codex-secondary-reset-after-seconds", "18000")
	resp.Header.Set("x-codex-secondary-window-minutes", "300")

	repo := &openAIProbeStore{}
	upstream := &queuedHTTPUpstream{responses: []*http.Response{resp}}
	svc := &provideradapter.OpenAIProviderTest{Store: repo, Transport: upstream}
	provider := &providercore.Record{
		ID:          88,
		Platform:    capability.PlatformOpenAI,
		Type:        capability.ProviderTypeOAuth,
		Status:      providercore.StatusError,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "test-token"},
	}

	err := executeOpenAIProbe(t, svc, ctx, provider, "gpt-5.4", "", "")
	require.Error(t, err)
	require.NotEmpty(t, repo.updatedExtra)
	require.Equal(t, 100.0, repo.updatedExtra["codex_5h_used_percent"])
	require.Equal(t, provider.ID, repo.rateLimitedID)
	require.NotNil(t, repo.rateLimitedAt)
	require.Equal(t, provider.ID, repo.clearedErrorID)
	require.Equal(t, billing.StatusActive, provider.Status)
	require.Empty(t, provider.ErrorMessage)
	require.NotNil(t, provider.RateLimitResetAt)
}

func TestProviderTestService_OpenAI429BodyOnlyPersistsRateLimitAndClearsStaleError(t *testing.T) {
	ctx, _ := newTestContext()

	resp := newJSONResponse(http.StatusTooManyRequests, `{"error":{"type":"usage_limit_reached","message":"limit reached","resets_at":"1777283883"}}`)

	repo := &openAIProbeStore{}
	upstream := &queuedHTTPUpstream{responses: []*http.Response{resp}}
	svc := &provideradapter.OpenAIProviderTest{Store: repo, Transport: upstream}
	provider := &providercore.Record{
		ID:           77,
		Platform:     capability.PlatformOpenAI,
		Type:         capability.ProviderTypeOAuth,
		Status:       providercore.StatusError,
		ErrorMessage: "Access forbidden (403): provider may be suspended or lack permissions",
		Concurrency:  1,
		Credentials:  map[string]any{"access_token": "test-token"},
	}

	err := executeOpenAIProbe(t, svc, ctx, provider, "gpt-5.4", "", "")
	require.Error(t, err)
	require.Equal(t, provider.ID, repo.rateLimitedID)
	require.NotNil(t, repo.rateLimitedAt)
	require.Equal(t, provider.ID, repo.clearedErrorID)
	require.Equal(t, billing.StatusActive, provider.Status)
	require.Empty(t, provider.ErrorMessage)
	require.NotNil(t, provider.RateLimitResetAt)
	require.Empty(t, repo.updatedExtra)
}

func TestProviderTestService_OpenAI429SyncsObservedPlanType(t *testing.T) {
	ctx, _ := newTestContext()

	resp := newJSONResponse(http.StatusTooManyRequests, `{"error":{"type":"usage_limit_reached","message":"limit reached","plan_type":"free","resets_at":1777283883}}`)

	repo := &openAIProbeStore{}
	upstream := &queuedHTTPUpstream{responses: []*http.Response{resp}}
	svc := &provideradapter.OpenAIProviderTest{Store: repo, Transport: upstream}
	provider := &providercore.Record{
		ID:          81,
		Platform:    capability.PlatformOpenAI,
		Type:        capability.ProviderTypeOAuth,
		Status:      billing.StatusActive,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "test-token", "plan_type": "plus"},
	}

	err := executeOpenAIProbe(t, svc, ctx, provider, "gpt-5.4", "", "")
	require.Error(t, err)
	require.Equal(t, []int64{provider.ID}, repo.bulkUpdatedIDs)
	require.Equal(t, "free", repo.bulkUpdatedPayload.Credentials["plan_type"])
	require.Equal(t, "free", provider.Credentials["plan_type"])
	require.Equal(t, provider.ID, repo.rateLimitedID)
	require.NotNil(t, provider.RateLimitResetAt)
}

func TestProviderTestService_OpenAI429ActiveProviderDoesNotClearError(t *testing.T) {
	ctx, _ := newTestContext()

	resp := newJSONResponse(http.StatusTooManyRequests, `{"error":{"type":"usage_limit_reached","message":"limit reached","resets_in_seconds":3600}}`)

	repo := &openAIProbeStore{}
	upstream := &queuedHTTPUpstream{responses: []*http.Response{resp}}
	svc := &provideradapter.OpenAIProviderTest{Store: repo, Transport: upstream}
	provider := &providercore.Record{
		ID:          78,
		Platform:    capability.PlatformOpenAI,
		Type:        capability.ProviderTypeOAuth,
		Status:      billing.StatusActive,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "test-token"},
	}

	err := executeOpenAIProbe(t, svc, ctx, provider, "gpt-5.4", "", "")
	require.Error(t, err)
	require.Equal(t, provider.ID, repo.rateLimitedID)
	require.NotNil(t, repo.rateLimitedAt)
	require.Zero(t, repo.clearedErrorID)
	require.Equal(t, billing.StatusActive, provider.Status)
	require.NotNil(t, provider.RateLimitResetAt)
}

func TestProviderTestService_OpenAI429WithoutResetSignalDoesNotMutateRuntimeState(t *testing.T) {
	ctx, _ := newTestContext()

	resp := newJSONResponse(http.StatusTooManyRequests, `{"error":{"type":"usage_limit_reached","message":"limit reached"}}`)

	repo := &openAIProbeStore{}
	upstream := &queuedHTTPUpstream{responses: []*http.Response{resp}}
	svc := &provideradapter.OpenAIProviderTest{Store: repo, Transport: upstream}
	provider := &providercore.Record{
		ID:           79,
		Platform:     capability.PlatformOpenAI,
		Type:         capability.ProviderTypeOAuth,
		Status:       providercore.StatusError,
		ErrorMessage: "stale 403",
		Concurrency:  1,
		Credentials:  map[string]any{"access_token": "test-token"},
	}

	err := executeOpenAIProbe(t, svc, ctx, provider, "gpt-5.4", "", "")
	require.Error(t, err)
	require.Zero(t, repo.rateLimitedID)
	require.Nil(t, repo.rateLimitedAt)
	require.Zero(t, repo.clearedErrorID)
	require.Equal(t, providercore.StatusError, provider.Status)
	require.Equal(t, "stale 403", provider.ErrorMessage)
	require.Nil(t, provider.RateLimitResetAt)
}

func TestProviderTestService_OpenAI401SetsPermanentErrorOnly(t *testing.T) {
	ctx, _ := newTestContext()

	resp := newJSONResponse(http.StatusUnauthorized, `{"error":"bad token"}`)

	repo := &openAIProbeStore{}
	upstream := &queuedHTTPUpstream{responses: []*http.Response{resp}}
	svc := &provideradapter.OpenAIProviderTest{Store: repo, Transport: upstream}
	provider := &providercore.Record{
		ID:          80,
		Platform:    capability.PlatformOpenAI,
		Type:        capability.ProviderTypeOAuth,
		Status:      billing.StatusActive,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "test-token"},
	}

	err := executeOpenAIProbe(t, svc, ctx, provider, "gpt-5.4", "", "")
	require.Error(t, err)
	require.Equal(t, provider.ID, repo.setErrorID)
	require.Contains(t, repo.setErrorMsg, "Authentication failed (401)")
	require.Zero(t, repo.rateLimitedID)
	require.Zero(t, repo.clearedErrorID)
	require.Nil(t, provider.RateLimitResetAt)
}

// TestProviderTestService_DeepSeekResponsesRoutesToOpenAIProbe 验证 CN Responses
// 提供商通过统一测试入口调用 OpenAI 探针。
func TestProviderTestService_DeepSeekResponsesRoutesToOpenAIProbe(t *testing.T) {
	ctx, _ := newTestContext()

	resp := newJSONResponse(http.StatusOK, "")
	resp.Body = io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\"}\n\n"))
	upstream := &queuedHTTPUpstream{responses: []*http.Response{resp}}
	svc := &provideradapter.OpenAIProviderTest{
		Transport:   upstream,
		ValidateURL: (egress.OperatorURLPolicy{}).Validate,
	}
	provider := &providercore.Record{
		ID:          93,
		Platform:    capability.PlatformDeepseek,
		Type:        capability.ProviderTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":      "sk-test",
			"base_url":     "https://relay.example.com/v1",
			"api_protocol": providercore.APIProtocolResponses,
		},
		Extra: map[string]any{},
	}
	repo := &openAIProbeStore{
		openAIProbeRecords: openAIProbeRecords{
			providersByID: map[int64]*providercore.Record{93: provider},
		},
	}
	svc.Store = repo

	err := executeOpenAIProbeRequest(t, svc, ctx, provider.ID, "gpt-5.4", "", "")
	require.NoError(t, err)
	require.Len(t, upstream.requests, 1)
	require.Equal(t, "https://relay.example.com/v1/responses", upstream.requests[0].URL.String())
}

func TestProviderTestService_OpenAIAPIKeySelectedChatUsesChatCompletionsPath(t *testing.T) {
	ctx, recorder := newTestContext()

	upstreamBody := strings.Join([]string{
		`data: {"id":"chatcmpl_test","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"pong"},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl_test","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &openAIProbeTransport{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}}
	svc := &provideradapter.OpenAIProviderTest{
		Transport:   upstream,
		ValidateURL: (egress.OperatorURLPolicy{}).Validate,
	}
	provider := &providercore.Record{
		ID:          91,
		Platform:    capability.PlatformOpenAI,
		Type:        capability.ProviderTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "sk-test",
			"base_url": "https://compat-upstream.example/v1",
		},
		Extra: map[string]any{providercore.ExtraKeyTextRouteMode: string(providercore.TextRouteModeForceChatCompletions)},
	}

	err := executeOpenAIProbe(t, svc, ctx, provider, "gpt-5.4", "hello", "")
	require.NoError(t, err)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, upstreamcore.HTTPUpstreamProfileOpenAI, upstreamcore.HTTPUpstreamProfileFromContext(upstream.lastReq.Context()))
	require.Equal(t, "https://compat-upstream.example/v1/chat/completions", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer sk-test", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, "text/event-stream", upstream.lastReq.Header.Get("Accept"))
	require.Equal(t, "gpt-5.4", gjson.GetBytes(upstream.lastBody, "model").String())
	require.True(t, gjson.GetBytes(upstream.lastBody, "stream").Bool())
	require.Equal(t, "hello", gjson.GetBytes(upstream.lastBody, "messages.0.content").String())
	require.False(t, gjson.GetBytes(upstream.lastBody, "input").Exists())
	body := recorder.Body.String()
	require.Contains(t, body, "pong")
	require.Contains(t, body, "已通过 /v1/chat/completions 验证")
	require.Contains(t, body, `"success":true`)
	require.NotContains(t, body, "当前测试接口仅支持 Responses API 路径")
}

func TestProviderTestService_OpenAIChatCompletionsPathReturns4xx(t *testing.T) {
	ctx, recorder := newTestContext()

	upstream := &openAIProbeTransport{resp: newJSONResponse(http.StatusBadRequest, `{"error":{"message":"bad request"}}`)}
	svc := &provideradapter.OpenAIProviderTest{
		Transport:   upstream,
		ValidateURL: (egress.OperatorURLPolicy{}).Validate,
	}
	provider := &providercore.Record{
		ID:          92,
		Platform:    capability.PlatformOpenAI,
		Type:        capability.ProviderTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "sk-test",
			"base_url": "https://compat-upstream.example",
		},
		Extra: map[string]any{providercore.ExtraKeyTextRouteMode: string(providercore.TextRouteModeForceChatCompletions)},
	}

	err := executeOpenAIProbe(t, svc, ctx, provider, "gpt-5.4", "", "")
	require.Error(t, err)
	require.Equal(t, "https://compat-upstream.example/v1/chat/completions", upstream.lastReq.URL.String())
	require.Contains(t, err.Error(), "Chat Completions API (/v1/chat/completions) returned 400")
	require.Contains(t, recorder.Body.String(), "/v1/chat/completions")
	require.NotContains(t, recorder.Body.String(), `"success":true`)
}

func TestProviderTestService_OpenAIChatCompletionsPathTimeout(t *testing.T) {
	ctx, recorder := newTestContext()

	upstream := &openAIProbeTransport{err: context.DeadlineExceeded}
	svc := &provideradapter.OpenAIProviderTest{
		Transport:   upstream,
		ValidateURL: (egress.OperatorURLPolicy{}).Validate,
	}
	provider := &providercore.Record{
		ID:          93,
		Platform:    capability.PlatformOpenAI,
		Type:        capability.ProviderTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "sk-test",
			"base_url": "https://compat-upstream.example",
		},
		Extra: map[string]any{providercore.ExtraKeyTextRouteMode: string(providercore.TextRouteModeForceChatCompletions)},
	}

	err := executeOpenAIProbe(t, svc, ctx, provider, "gpt-5.4", "", "")
	require.Error(t, err)
	require.Equal(t, "https://compat-upstream.example/v1/chat/completions", upstream.lastReq.URL.String())
	require.Contains(t, err.Error(), "Chat Completions API (/v1/chat/completions) request failed")
	require.Contains(t, err.Error(), context.DeadlineExceeded.Error())
	require.Contains(t, recorder.Body.String(), "/v1/chat/completions")
	require.NotContains(t, recorder.Body.String(), `"success":true`)
}

func TestProviderTestService_OpenAIChatCompletionsPathRejectsNonJSONStream(t *testing.T) {
	ctx, recorder := newTestContext()

	upstream := &openAIProbeTransport{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("data: not-json\n\n")),
	}}
	svc := &provideradapter.OpenAIProviderTest{
		Transport:   upstream,
		ValidateURL: (egress.OperatorURLPolicy{}).Validate,
	}
	provider := &providercore.Record{
		ID:          94,
		Platform:    capability.PlatformOpenAI,
		Type:        capability.ProviderTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "sk-test",
			"base_url": "https://compat-upstream.example",
		},
		Extra: map[string]any{providercore.ExtraKeyTextRouteMode: string(providercore.TextRouteModeForceChatCompletions)},
	}

	err := executeOpenAIProbe(t, svc, ctx, provider, "gpt-5.4", "", "")
	require.Error(t, err)
	require.Equal(t, "https://compat-upstream.example/v1/chat/completions", upstream.lastReq.URL.String())
	require.Contains(t, err.Error(), "Invalid Chat Completions response from /v1/chat/completions")
	require.Contains(t, recorder.Body.String(), "/v1/chat/completions")
	require.NotContains(t, recorder.Body.String(), `"success":true`)
}

// TestProviderTestServiceExplicitProtocolDoesNotMutateProvider 检查本次测试按选择的协议执行，持久化路由配置保持不变。
func TestProviderTestServiceExplicitProtocolDoesNotMutateProvider(t *testing.T) {
	for _, protocol := range []string{"responses", "chat_completions"} {
		t.Run(protocol, func(t *testing.T) {
			provider := providercore.Record{ID: 901, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey, Credentials: map[string]any{"api_key": "sk-test", "base_url": "https://example.com/v1"}, Extra: map[string]any{"openai_text_route_mode": "force_chat_completions"}}
			if protocol == "chat_completions" {
				provider.Extra["openai_text_route_mode"] = "force_responses"
			}
			original := provider.Extra["openai_text_route_mode"]
			upstream := &openAIProbeTransport{resp: &http.Response{StatusCode: http.StatusBadRequest, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":"capture"}`))}}
			svc := &provideradapter.OpenAIProviderTest{Store: &openAIProbeStore{openAIProbeRecords: openAIProbeRecords{providersByID: map[int64]*providercore.Record{provider.ID: &provider}}}, Transport: upstream, ValidateURL: (egress.OperatorURLPolicy{}).Validate}
			c, _ := newTestContext()
			c.Request = httptest.NewRequest(http.MethodPost, "/test", nil)
			require.Error(t, executeOpenAIProbeRequestType(t, svc, c, provider.ID, "gpt-5.4", "hi", "text", "default", protocol))
			path := "/v1/responses"
			if protocol == "chat_completions" {
				path = "/v1/chat/completions"
			}
			require.Equal(t, path, upstream.lastReq.URL.Path)
			require.Equal(t, original, provider.Extra["openai_text_route_mode"])
		})
	}
}
