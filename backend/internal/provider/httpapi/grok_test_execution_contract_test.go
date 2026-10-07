package httpapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/egress"
	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/upstream/grok"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type grokProviderTestRateLimitRepo struct {
	*grokTestStoreFixture
	rateLimitedCalls int
	resetAt          time.Time
}

func (r *grokProviderTestRateLimitRepo) SetRateLimited(_ context.Context, _ int64, resetAt time.Time) error {
	r.rateLimitedCalls++
	r.resetAt = resetAt
	return nil
}

func TestProviderTestService_TestProviderConnection_GrokUsesXAIResponses(t *testing.T) {
	provider := &providercore.Record{
		ID:          13,
		Name:        "grok-oauth",
		Platform:    capability.PlatformGrok,
		Type:        capability.ProviderTypeOAuth,
		Status:      billing.StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":  "grok-access-token",
			"refresh_token": "grok-refresh-token",
			"expires_at":    time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339),
			"model_mapping": map[string]any{
				"grok": "grok-latest",
			},
		},
	}
	repo := &grokTestStoreFixture{
		providersByID: map[int64]*providercore.Record{provider.ID: provider},
	}
	upstream := &grokTestTransportFixture{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			"data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n" +
				"data: {\"type\":\"response.completed\"}\n\n",
		)),
	}}
	svc := &provideradapter.GrokProviderTest{
		Store:     repo,
		Tokens:    &providercore.GrokTokenSource{Repository: repo, Policy: providercore.GrokProviderRefreshPolicy()},
		Transport: upstream,
	}

	rec := httptest.NewRecorder()

	err := executeGrokProviderTest(t, svc, provider, rec, "grok", "", providercore.ProviderTestModeDefault)
	require.NoError(t, err)

	require.Equal(t, "https://cli-chat-proxy.grok.com/v1/responses", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer grok-access-token", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, grok.CLIClientVersion, upstream.lastReq.Header.Get("X-Grok-Client-Version"))
	require.Equal(t, "application/json, text/event-stream", upstream.lastReq.Header.Get("Accept"))
	require.Equal(t, "grok-latest", gjson.GetBytes(upstream.lastBody, "model").String())
	require.Equal(t, "hi", gjson.GetBytes(upstream.lastBody, "input").String())
	require.True(t, gjson.GetBytes(upstream.lastBody, "stream").Bool())
	require.False(t, gjson.GetBytes(upstream.lastBody, "max_output_tokens").Exists())
	require.False(t, gjson.GetBytes(upstream.lastBody, "store").Exists())
	require.NotContains(t, rec.Body.String(), "claude")
	require.Contains(t, rec.Body.String(), `"model":"grok-latest"`)
	require.Contains(t, rec.Body.String(), `"type":"test_complete"`)
}

func TestProviderTestService_TestProviderConnection_GrokUsesCustomTextPrompt(t *testing.T) {
	provider := &providercore.Record{
		ID:          17,
		Name:        "grok-custom-prompt",
		Platform:    capability.PlatformGrok,
		Type:        capability.ProviderTypeOAuth,
		Status:      billing.StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":  "grok-access-token",
			"refresh_token": "grok-refresh-token",
			"expires_at":    time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339),
		},
	}
	repo := &grokTestStoreFixture{providersByID: map[int64]*providercore.Record{provider.ID: provider}}
	upstream := &grokTestTransportFixture{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\"}\n\n")),
	}}
	svc := &provideradapter.GrokProviderTest{
		Store:     repo,
		Tokens:    &providercore.GrokTokenSource{Repository: repo, Policy: providercore.GrokProviderRefreshPolicy()},
		Transport: upstream,
	}
	recorder := httptest.NewRecorder()

	err := executeGrokProviderTest(t, svc, provider, recorder, "grok-4.5", "describe this provider in one sentence", providercore.ProviderTestModeDefault, providercore.ProviderTestTypeText)
	require.NoError(t, err)
	require.Equal(t, "describe this provider in one sentence", gjson.GetBytes(upstream.lastBody, "input").String())
}

func TestProviderTestService_TestProviderConnection_GrokExplicitImageUsesMediaEndpoint(t *testing.T) {
	provider := &providercore.Record{
		ID:          18,
		Name:        "grok-image-api-key",
		Platform:    capability.PlatformGrok,
		Type:        capability.ProviderTypeAPIKey,
		Status:      billing.StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{"api_key": "grok-api-key"},
	}
	repo := &grokTestStoreFixture{providersByID: map[int64]*providercore.Record{provider.ID: provider}}
	upstream := &grokTestTransportFixture{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"data":[{"b64_json":"aGVsbG8=","mime_type":"image/png"}]}`)),
	}}
	svc := &provideradapter.GrokProviderTest{Store: repo, Transport: upstream, OperatorValidator: (egress.OperatorURLPolicy{}).Validate}
	recorder := httptest.NewRecorder()

	err := executeGrokProviderTest(t, svc, provider, recorder, "custom-image-alias", "draw a lighthouse", providercore.ProviderTestModeDefault, providercore.ProviderTestTypeImage)
	require.NoError(t, err)
	require.Equal(t, "https://api.x.ai/v1/images/generations", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer grok-api-key", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, "custom-image-alias", gjson.GetBytes(upstream.lastBody, "model").String())
	require.Equal(t, "draw a lighthouse", gjson.GetBytes(upstream.lastBody, "prompt").String())
	require.Contains(t, recorder.Body.String(), "data:image/png;base64,aGVsbG8=")
}

func TestProviderTestService_TestProviderConnection_GrokDefaultsEmptyModelTo45(t *testing.T) {
	provider := &providercore.Record{
		ID:          16,
		Name:        "grok-oauth-default-model",
		Platform:    capability.PlatformGrok,
		Type:        capability.ProviderTypeOAuth,
		Status:      billing.StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":  "grok-access-token",
			"refresh_token": "grok-refresh-token",
			"expires_at":    time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339),
			// 空模型在提供商映射后使用默认值，因此跳过此处的映射键。
			"model_mapping": map[string]any{"grok-4.5": "grok-4.3"},
		},
	}
	repo := &grokTestStoreFixture{providersByID: map[int64]*providercore.Record{provider.ID: provider}}
	upstream := &grokTestTransportFixture{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			"data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n" +
				"data: {\"type\":\"response.completed\"}\n\n",
		)),
	}}
	svc := &provideradapter.GrokProviderTest{
		Store:     repo,
		Tokens:    &providercore.GrokTokenSource{Repository: repo, Policy: providercore.GrokProviderRefreshPolicy()},
		Transport: upstream,
	}
	recorder := httptest.NewRecorder()

	err := executeGrokProviderTest(t, svc, provider, recorder, "", "", providercore.ProviderTestModeDefault)

	require.NoError(t, err)
	require.Equal(t, grok.DefaultResponsesModel, gjson.GetBytes(upstream.lastBody, "model").String())
	require.Contains(t, recorder.Body.String(), `"model":"grok-4.5"`)
}

func TestProviderTestService_Grok429PersistsRateLimitReset(t *testing.T) {
	provider := &providercore.Record{
		ID:          14,
		Name:        "grok-oauth-limited",
		Platform:    capability.PlatformGrok,
		Type:        capability.ProviderTypeOAuth,
		Status:      billing.StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":  "grok-access-token",
			"refresh_token": "grok-refresh-token",
			"expires_at":    time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339),
		},
	}
	baseRepo := &grokTestStoreFixture{providersByID: map[int64]*providercore.Record{provider.ID: provider}}
	repo := &grokProviderTestRateLimitRepo{grokTestStoreFixture: baseRepo}
	upstream := &grokTestTransportFixture{resp: &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{"Retry-After": []string{"45"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"rate limited"}}`)),
	}}
	svc := &provideradapter.GrokProviderTest{
		Store:     repo,
		Tokens:    &providercore.GrokTokenSource{Repository: repo, Policy: providercore.GrokProviderRefreshPolicy()},
		Transport: upstream,
	}
	recorder := httptest.NewRecorder()

	err := executeGrokProviderTest(t, svc, provider, recorder, "grok", "", providercore.ProviderTestModeDefault)

	require.Error(t, err)
	require.Equal(t, 1, repo.rateLimitedCalls)
	require.WithinDuration(t, time.Now().Add(45*time.Second), repo.resetAt, time.Second)
}

func TestProviderTestService_Grok429WithoutQuotaHeadersUsesFallback(t *testing.T) {
	provider := &providercore.Record{
		ID: 15, Name: "grok-oauth-limited-no-headers", Platform: capability.PlatformGrok,
		Type: capability.ProviderTypeOAuth, Status: billing.StatusActive, Schedulable: true, Concurrency: 1,
		Credentials: map[string]any{
			"access_token":  "grok-access-token",
			"refresh_token": "grok-refresh-token",
			"expires_at":    time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339),
		},
	}
	baseRepo := &grokTestStoreFixture{providersByID: map[int64]*providercore.Record{provider.ID: provider}}
	repo := &grokProviderTestRateLimitRepo{grokTestStoreFixture: baseRepo}
	upstream := &grokTestTransportFixture{resp: &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"quota exhausted"}}`)),
	}}
	svc := &provideradapter.GrokProviderTest{
		Store: repo, Tokens: &providercore.GrokTokenSource{Repository: repo, Policy: providercore.GrokProviderRefreshPolicy()}, Transport: upstream,
	}
	recorder := httptest.NewRecorder()
	before := time.Now()

	err := executeGrokProviderTest(t, svc, provider, recorder, "grok", "", providercore.ProviderTestModeDefault)

	require.Error(t, err)
	require.Equal(t, 1, repo.rateLimitedCalls)
	require.WithinDuration(t, before.Add(2*time.Minute), repo.resetAt, time.Second)
}

// 夹具记录存储写入和平台请求，提供商测试使用生产实现。
type grokTestStoreFixture struct {
	providersByID map[int64]*providercore.Record
}

func (s *grokTestStoreFixture) GetByID(_ context.Context, id int64) (*providercore.Record, error) {
	value, ok := s.providersByID[id]
	if !ok {
		return nil, fmt.Errorf("provider %d missing", id)
	}
	return providercore.CloneRecord(value), nil
}

func (*grokTestStoreFixture) UpdateExtra(context.Context, int64, map[string]any) error { return nil }

func (*grokTestStoreFixture) SetRateLimited(context.Context, int64, time.Time) error { return nil }

func (*grokTestStoreFixture) SetTempUnschedulable(context.Context, int64, time.Time, string) error {
	return nil
}

type grokTestTransportFixture struct {
	resp     *http.Response
	lastReq  *http.Request
	lastBody []byte
}

func (u *grokTestTransportFixture) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.lastReq = req
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	u.lastBody = body
	return u.resp, nil
}

type grokTestTargetLoader struct{ target providercore.TestTarget }

func (l grokTestTargetLoader) LoadTestTarget(context.Context, providercore.TestRequest) (providercore.TestTarget, error) {
	return l.target, nil
}

func executeGrokProviderTest(t *testing.T, executor *provideradapter.GrokProviderTest, value *providercore.Record, recorder *httptest.ResponseRecorder, model, prompt, mode string, types ...string) error {
	t.Helper()
	core := providercore.NewTestService(grokTestTargetLoader{target: executor.Target(value)}, providercore.TestOptions{Now: time.Now, Error: func(string) {}, WriteError: func(error) {}})
	request := providercore.TestRequest{ProviderID: value.ID, Model: model, Prompt: prompt, Mode: mode}
	if len(types) > 0 {
		request.Type = &types[0]
	}
	return core.Test(t.Context(), request, NewTestEventSink(recorder))
}
