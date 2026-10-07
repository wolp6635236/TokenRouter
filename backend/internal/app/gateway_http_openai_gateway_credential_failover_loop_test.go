package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	keyhttp "github.com/TokenFlux/TokenRouter/internal/apikey/httpapi"
	authctx "github.com/TokenFlux/TokenRouter/internal/identity/httpapi/authctx"

	apikey "github.com/TokenFlux/TokenRouter/internal/apikey"
	"github.com/TokenFlux/TokenRouter/internal/billing"
	billingtestkit "github.com/TokenFlux/TokenRouter/internal/billing/testkit"
	forwardcore "github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	httptestkit "github.com/TokenFlux/TokenRouter/internal/gateway/httpapi/testkit"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	scheduler "github.com/TokenFlux/TokenRouter/internal/scheduler"

	identity "github.com/TokenFlux/TokenRouter/internal/identity"

	routing "github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"

	"github.com/TokenFlux/TokenRouter/internal/config"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"

	httpclient "github.com/TokenFlux/TokenRouter/internal/infra/httpclient"
	"github.com/TokenFlux/TokenRouter/internal/infra/httpclient/tlsfingerprint"
	logging "github.com/TokenFlux/TokenRouter/internal/infra/telemetry/logging"
	"github.com/TokenFlux/TokenRouter/internal/pkg/apperror"
	"github.com/TokenFlux/TokenRouter/internal/protocol"
	testassert "github.com/TokenFlux/TokenRouter/internal/testutil/assertion"

	xai "github.com/TokenFlux/TokenRouter/internal/upstream/grok"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type grokCredentialHandlerRepo struct {
	gatewayprovider.ExecutionProviderStore

	mu             sync.Mutex
	providers      []gatewayprovider.ExecutionProvider
	setErrorIDs    []int64
	setTempIDs     []int64
	rateLimitIDs   []int64
	updateExtraIDs []int64
	selectionCalls int
	setErrorErr    error
	setTempErr     error
	missingOnGet   map[int64]bool
}

func (r *grokCredentialHandlerRepo) ListSchedulableByPlatform(_ context.Context, platform string) ([]gatewayprovider.ExecutionProvider, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.selectionCalls++
	out := make([]gatewayprovider.ExecutionProvider, 0, len(r.providers))
	for _, provider := range r.providers {
		if (platform == "" || provider.Record.Platform == platform) && provider.View().IsSchedulable() {
			out = append(out, provider)
		}
	}
	return out, nil
}

func (r *grokCredentialHandlerRepo) ListSchedulableByGroupIDAndPlatform(ctx context.Context, _ int64, platform string) ([]gatewayprovider.ExecutionProvider, error) {
	return r.ListSchedulableByPlatform(ctx, platform)
}

func (r *grokCredentialHandlerRepo) ListSchedulableUngroupedByPlatform(ctx context.Context, platform string) ([]gatewayprovider.ExecutionProvider, error) {
	return r.ListSchedulableByPlatform(ctx, platform)
}

func (r *grokCredentialHandlerRepo) GetByID(_ context.Context, id int64) (*gatewayprovider.ExecutionProvider, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.missingOnGet[id] {
		return nil, nil
	}
	for _, provider := range r.providers {
		if provider.Record.ID == id {
			copy := provider
			copy.Record.Credentials = cloneCredentialMap(provider.Record.Credentials)
			return &copy, nil
		}
	}
	return nil, nil
}

func (r *grokCredentialHandlerRepo) SetError(_ context.Context, id int64, message string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.setErrorIDs = append(r.setErrorIDs, id)
	if r.setErrorErr != nil {
		return r.setErrorErr
	}
	for i := range r.providers {
		if r.providers[i].Record.ID == id {
			r.providers[i].Record.Status = providercore.StatusError
			r.providers[i].Record.Schedulable = false
			r.providers[i].Record.ErrorMessage = message
		}
	}
	return nil
}

func (r *grokCredentialHandlerRepo) SetTempUnschedulable(_ context.Context, id int64, until time.Time, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.setTempIDs = append(r.setTempIDs, id)
	if r.setTempErr != nil {
		return r.setTempErr
	}
	for i := range r.providers {
		if r.providers[i].Record.ID == id {
			value := until
			r.providers[i].Record.TempUnschedulableUntil = &value
		}
	}
	return nil
}

func (r *grokCredentialHandlerRepo) SetRateLimited(_ context.Context, id int64, resetAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rateLimitIDs = append(r.rateLimitIDs, id)
	for i := range r.providers {
		if r.providers[i].Record.ID != id {
			continue
		}
		now := time.Now()
		r.providers[i].Record.RateLimitedAt = &now
		value := resetAt
		r.providers[i].Record.RateLimitResetAt = &value
	}
	return nil
}

func (r *grokCredentialHandlerRepo) SetRateLimitedIfLater(ctx context.Context, id int64, resetAt time.Time) error {
	r.mu.Lock()
	for i := range r.providers {
		if r.providers[i].Record.ID == id && r.providers[i].Record.RateLimitResetAt != nil && !resetAt.After(*r.providers[i].Record.RateLimitResetAt) {
			r.mu.Unlock()
			return nil
		}
	}
	r.mu.Unlock()
	return r.SetRateLimited(ctx, id, resetAt)
}

func (r *grokCredentialHandlerRepo) SetGrokCredentialErrorIfMatch(
	_ context.Context,
	id int64,
	snapshot providercore.CredentialMutationSnapshot,
	message string,
) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.providers {
		provider := &r.providers[i]
		if provider.Record.ID != id || !handlerGrokCredentialSnapshotMatches(provider, snapshot) {
			continue
		}
		r.setErrorIDs = append(r.setErrorIDs, id)
		if r.setErrorErr != nil {
			return false, r.setErrorErr
		}
		provider.Record.Status = providercore.StatusError
		provider.Record.Schedulable = false
		provider.Record.ErrorMessage = message
		return true, nil
	}
	return false, nil
}

func (r *grokCredentialHandlerRepo) SetGrokCredentialTempUnschedulableIfMatch(
	_ context.Context,
	id int64,
	snapshot providercore.CredentialMutationSnapshot,
	until time.Time,
	_ string,
) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.providers {
		provider := &r.providers[i]
		if provider.Record.ID != id || !handlerGrokCredentialSnapshotMatches(provider, snapshot) {
			continue
		}
		r.setTempIDs = append(r.setTempIDs, id)
		if r.setTempErr != nil {
			return false, r.setTempErr
		}
		value := until
		provider.Record.TempUnschedulableUntil = &value
		return true, nil
	}
	return false, nil
}

func handlerGrokCredentialSnapshotMatches(provider *gatewayprovider.ExecutionProvider, snapshot providercore.CredentialMutationSnapshot) bool {
	if provider == nil {
		return false
	}
	credentialsJSON, err := json.Marshal(provider.Record.Credentials)
	return err == nil && provider.View().IsGrokOAuth() && provider.View().IsSchedulable() && string(credentialsJSON) == snapshot.CredentialsJSON &&
		handlerGrokCredentialProxyIDsEqual(provider.Record.ProxyID, snapshot.ProxyID)
}

func handlerGrokCredentialProxyIDsEqual(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func (r *grokCredentialHandlerRepo) UpdateExtra(_ context.Context, id int64, updates map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.updateExtraIDs = append(r.updateExtraIDs, id)
	for i := range r.providers {
		if r.providers[i].Record.ID != id {
			continue
		}
		if r.providers[i].Record.Extra == nil {
			r.providers[i].Record.Extra = map[string]any{}
		}
		for key, value := range updates {
			r.providers[i].Record.Extra[key] = value
		}
	}
	return nil
}

func (r *grokCredentialHandlerRepo) errorIDs() []int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]int64(nil), r.setErrorIDs...)
}

func (r *grokCredentialHandlerRepo) selectorCalls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.selectionCalls
}

func (r *grokCredentialHandlerRepo) rateLimitedProviderIDs() []int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]int64(nil), r.rateLimitIDs...)
}

type grokCredentialHandlerTokenCache struct {
	providercore.AccessTokenCache
	mu        sync.Mutex
	deleteErr error
}

func (c *grokCredentialHandlerTokenCache) GetAccessToken(context.Context, string) (string, error) {
	return "", errors.New("not cached")
}

func (c *grokCredentialHandlerTokenCache) SetAccessToken(context.Context, string, string, time.Duration) error {
	return nil
}

func (c *grokCredentialHandlerTokenCache) DeleteAccessToken(context.Context, string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.deleteErr
}

func (c *grokCredentialHandlerTokenCache) AcquireRefreshLock(context.Context, string, time.Duration) (bool, error) {
	return true, nil
}

func (c *grokCredentialHandlerTokenCache) ReleaseRefreshLock(context.Context, string) error {
	return nil
}

func cloneCredentialMap(source map[string]any) map[string]any {
	cloned := make(map[string]any, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

type grokCredentialHandlerRefresher struct {
	mode    string
	started chan struct{}
	once    sync.Once
}

func (r *grokCredentialHandlerRefresher) CacheKey(provider *providercore.Record) string {
	return providercore.GrokTokenCacheKey(provider)
}

func (r *grokCredentialHandlerRefresher) CanRefresh(provider *providercore.Record) bool {
	return provider != nil && provider.IsGrokOAuth()
}

func (r *grokCredentialHandlerRefresher) NeedsRefresh(provider *providercore.Record, _ time.Duration) bool {
	return provider != nil && (provider.ID == 801 || r.mode == "all_revoked")
}

func (r *grokCredentialHandlerRefresher) Refresh(ctx context.Context, _ *providercore.Record) (map[string]any, error) {
	switch r.mode {
	case "revoked", "all_revoked", "mutation_set_error", "mutation_cache":
		return nil, apperror.New(http.StatusBadGateway, "GROK_OAUTH_TOKEN_REFRESH_FAILED", "invalid_grant")
	case "provider":
		return nil, apperror.New(http.StatusBadGateway, "GROK_OAUTH_TOKEN_REFRESH_FAILED", "invalid_client")
	case "cancel":
		r.once.Do(func() { close(r.started) })
		<-ctx.Done()
		return nil, ctx.Err()
	case "transient", "mutation_temp":
		return nil, errors.New("temporary refresh transport failure")
	default:
		return nil, nil
	}
}

type grokCredentialHandlerUpstream struct {
	httpclient.
		UpstreamTransport
	mu             sync.Mutex
	hits           []int64
	requestURLs    []string
	authorization  []string
	failProviderID int64
	rateLimitIDs   map[int64]bool
	failureStatus  map[int64]int
	cancelRequest  context.CancelFunc
}

func (u *grokCredentialHandlerUpstream) Do(req *http.Request, _ string, providerID int64, _ int) (*http.Response, error) {
	var requestBody []byte
	if req.Body != nil {
		requestBody, _ = io.ReadAll(req.Body)
	}
	u.mu.Lock()
	u.hits = append(u.hits, providerID)
	u.requestURLs = append(u.requestURLs, req.URL.String())
	u.authorization = append(u.authorization, req.Header.Get("Authorization"))
	failProviderID := u.failProviderID
	rateLimited := u.rateLimitIDs[providerID]
	failureStatus := u.failureStatus[providerID]
	cancelRequest := u.cancelRequest
	u.mu.Unlock()
	if rateLimited {
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header: http.Header{
				"Content-Type": []string{"application/json"},
				"Retry-After":  []string{"60"},
			},
			Body: io.NopCloser(bytes.NewBufferString(`{"error":{"message":"rate limited"}}`)),
		}, nil
	}
	if failureStatus > 0 {
		return &http.Response{
			StatusCode: failureStatus,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(`{"error":{"message":"upstream unavailable"}}`)),
		}, nil
	}
	if providerID == failProviderID {
		if cancelRequest != nil {
			cancelRequest()
		}
		return &http.Response{
			StatusCode: http.StatusPaymentRequired,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(`{"error":{"message":"payment required"}}`)),
		}, nil
	}
	if bytes.Contains(requestBody, []byte(`"stream":true`)) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: io.NopCloser(bytes.NewBufferString(
				"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_healthy\",\"model\":\"grok-4.5\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n",
			)),
		}, nil
	}
	if strings.Contains(req.URL.Path, "/chat/completions") {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(bytes.NewBufferString(
				`{"id":"chatcmpl_healthy","object":"chat.completion","model":"grok-4.5","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
			)),
		}, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(bytes.NewBufferString(
			`{"id":"resp_healthy","object":"response","model":"grok-4.5","status":"completed","output":[{"type":"message","id":"msg_healthy","status":"completed","role":"assistant","content":[{"type":"output_text","text":"ok","annotations":[]}]}],"usage":{"input_tokens":1,"output_tokens":1}}`,
		)),
	}, nil
}

func (u *grokCredentialHandlerUpstream) DoWithTLS(
	req *http.Request,
	proxyURL string,
	providerID int64,
	providerConcurrency int,
	_ *tlsfingerprint.Profile,
) (*http.Response, error) {
	return u.Do(req, proxyURL, providerID, providerConcurrency)
}

func (u *grokCredentialHandlerUpstream) providerHits() []int64 {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]int64(nil), u.hits...)
}

func (u *grokCredentialHandlerUpstream) requests() ([]string, []string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.requestURLs...), append([]string(nil), u.authorization...)
}

func TestResponsesCredentialFailoverLoop(t *testing.T) {
	t.Run("revoked provider selects healthy provider", func(t *testing.T) {
		h, repo, upstream, router, cleanup := newGrokCredentialFailoverHandler(t, "revoked")
		defer cleanup()
		_ = h

		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", bytes.NewBufferString(`{"model":"grok","input":"hello","stream":false}`))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, req)

		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		require.Contains(t, recorder.Body.String(), "resp_healthy")
		require.Equal(t, []int64{801}, repo.errorIDs())
		require.Equal(t, []int64{802}, upstream.providerHits())
		requestURLs, authorization := upstream.requests()
		require.Equal(t, []string{xai.DefaultCLIBaseURL + "/responses"}, requestURLs)
		require.Equal(t, []string{"Bearer healthy-access"}, authorization)
	})

	t.Run("provider configuration stops before healthy provider", func(t *testing.T) {
		h, repo, upstream, router, cleanup := newGrokCredentialFailoverHandler(t, "provider")
		defer cleanup()

		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", bytes.NewBufferString(`{"model":"grok","input":"hello","stream":false}`))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, req)

		require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
		require.Contains(t, recorder.Body.String(), forwardcore.GrokCredentialUnavailableClientMessage)
		require.Empty(t, repo.errorIDs())
		require.Empty(t, upstream.providerHits())
		require.Equal(t, 1, repo.selectorCalls())
		require.Zero(t, h.Input.Choices.SnapshotOpenAIProviderSchedulerMetrics().RuntimeStatsProviderCount,
			"provider-scoped auth failure must not penalize the selected provider")
	})

	t.Run("parent cancellation stops before healthy provider", func(t *testing.T) {
		_, repo, upstream, router, cleanup := newGrokCredentialFailoverHandler(t, "cancel")
		defer cleanup()

		ctx, cancel := context.WithCancel(context.Background())
		req := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", bytes.NewBufferString(`{"model":"grok","input":"hello","stream":false}`)).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		done := make(chan struct{})
		go func() {
			defer close(done)
			router.ServeHTTP(recorder, req)
		}()

		select {
		case <-time.After(2 * time.Second):
			t.Fatal("credential refresh did not start")
		case <-findHandlerRefresherStarted(router):
			cancel()
		}
		select {
		case <-time.After(2 * time.Second):
			t.Fatal("handler did not stop after cancellation")
		case <-done:
		}

		require.Empty(t, repo.errorIDs())
		require.Empty(t, upstream.providerHits())
	})

	t.Run("post-mapping cancellation stops before scheduler mutation or reselection", func(t *testing.T) {
		h, repo, upstream, router, cleanup := newGrokCredentialFailoverHandler(t, "postmap_cancel")
		defer cleanup()
		ctx, cancel := context.WithCancel(context.Background())
		upstream.mu.Lock()
		upstream.failProviderID = 801
		upstream.cancelRequest = cancel
		upstream.mu.Unlock()

		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", bytes.NewBufferString(`{"model":"grok","input":"hello","stream":false}`)).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, req)

		require.Equal(t, []int64{801}, upstream.providerHits())
		require.Empty(t, repo.errorIDs())
		require.Equal(t, 1, repo.selectorCalls())
		require.Zero(t, h.Input.Choices.SnapshotOpenAIProviderSchedulerMetrics().RuntimeStatsProviderCount)
	})

	t.Run("pre-cancelled request never invokes a provider selector", func(t *testing.T) {
		tests := []struct {
			name   string
			method string
			path   string
			body   string
		}{
			{name: "responses", method: http.MethodPost, path: "/openai/v1/responses", body: `{"model":"grok","input":"hello","stream":false}`},
			{name: "messages", method: http.MethodPost, path: "/openai/v1/messages", body: `{"model":"grok","max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`},
			{name: "chat completions", method: http.MethodPost, path: "/openai/v1/chat/completions", body: `{"model":"grok","messages":[{"role":"user","content":"hello"}],"stream":false}`},
			{name: "grok media", method: http.MethodGet, path: "/openai/v1/videos/request-1"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, repo, upstream, router, cleanup := newGrokCredentialFailoverHandler(t, "revoked")
				defer cleanup()
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				recorder := httptest.NewRecorder()
				req := httptest.NewRequest(tt.method, tt.path, bytes.NewBufferString(tt.body)).WithContext(ctx)
				req.Header.Set("Content-Type", "application/json")

				router.ServeHTTP(recorder, req)

				require.Zero(t, repo.selectorCalls())
				require.Empty(t, upstream.providerHits())
			})
		}
	})

	t.Run("credential state mutation failures stop before reselection", func(t *testing.T) {
		for _, mode := range []string{"mutation_set_error", "mutation_temp", "mutation_cache"} {
			t.Run(mode, func(t *testing.T) {
				_, repo, upstream, router, cleanup := newGrokCredentialFailoverHandler(t, mode)
				defer cleanup()

				recorder := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", bytes.NewBufferString(`{"model":"grok","input":"hello","stream":false}`))
				req.Header.Set("Content-Type", "application/json")
				router.ServeHTTP(recorder, req)

				require.Equal(t, http.StatusServiceUnavailable, recorder.Code, recorder.Body.String())
				require.Contains(t, recorder.Body.String(), forwardcore.GrokCredentialUnavailableClientMessage)
				require.Empty(t, upstream.providerHits())
				require.Equal(t, 1, repo.selectorCalls())
			})
		}
	})

	t.Run("missing credential provider stops before upstream or reselection", func(t *testing.T) {
		_, repo, upstream, router, cleanup := newGrokCredentialFailoverHandler(t, "nil_provider")
		defer cleanup()
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", bytes.NewBufferString(`{"model":"grok","input":"hello","stream":false}`))
		req.Header.Set("Content-Type", "application/json")

		router.ServeHTTP(recorder, req)

		require.Equal(t, http.StatusServiceUnavailable, recorder.Code, recorder.Body.String())
		require.Contains(t, recorder.Body.String(), forwardcore.GrokCredentialUnavailableClientMessage)
		require.Equal(t, 1, repo.selectorCalls())
		require.Empty(t, upstream.providerHits())
		require.Empty(t, repo.errorIDs())
	})
}

func TestResponsesGrok429FailoverIsBounded(t *testing.T) {
	t.Run("first rate limited provider selects healthy provider", func(t *testing.T) {
		_, repo, upstream, router, cleanup := newGrokCredentialFailoverHandler(t, "first_429")
		defer cleanup()
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", bytes.NewBufferString(`{"model":"grok","input":"hello","stream":false}`))
		req.Header.Set("Content-Type", "application/json")

		router.ServeHTTP(recorder, req)

		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		require.Contains(t, recorder.Body.String(), "resp_healthy")
		require.Equal(t, []int64{801, 802}, upstream.providerHits())
		require.Equal(t, []int64{801}, repo.rateLimitedProviderIDs())
	})

	t.Run("two rate limited providers stop without sweeping the pool", func(t *testing.T) {
		_, repo, upstream, router, cleanup := newGrokCredentialFailoverHandler(t, "all_429")
		defer cleanup()
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", bytes.NewBufferString(`{"model":"grok","input":"hello","stream":false}`))
		req.Header.Set("Content-Type", "application/json")

		router.ServeHTTP(recorder, req)

		require.Equal(t, http.StatusTooManyRequests, recorder.Code, recorder.Body.String())
		require.Equal(t, []int64{801, 802}, upstream.providerHits())
		require.Equal(t, []int64{801, 802}, repo.rateLimitedProviderIDs())
		require.NotContains(t, recorder.Body.String(), "expired")
		require.NotContains(t, recorder.Body.String(), "healthy-access")
		require.NotContains(t, recorder.Body.String(), "rate limited")
	})
}

func TestResponsesGrok402FailoverCooldown(t *testing.T) {
	_, repo, upstream, router, cleanup := newGrokCredentialFailoverHandler(t, "first_402")
	defer cleanup()

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", bytes.NewBufferString(`{"model":"grok","input":"hello","stream":false}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), "resp_healthy")
	require.Equal(t, []int64{801, 802}, upstream.providerHits())
	require.Equal(t, []int64{801}, repo.setTempIDs)
	before := repo.selectorCalls()

	second := httptest.NewRecorder()
	secondReq := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", bytes.NewBufferString(`{"model":"grok","input":"again","stream":false}`))
	secondReq.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(second, secondReq)

	require.Equal(t, http.StatusOK, second.Code, second.Body.String())
	require.Equal(t, before+1, repo.selectorCalls())
	require.Equal(t, []int64{801, 802, 802}, upstream.providerHits(), "cooldown must exclude the 402 provider from later requests")
}

func TestResponsesGrok429FailoverHandlesMixedStatuses(t *testing.T) {
	t.Run("429 then 500 stops after the bounded followup", func(t *testing.T) {
		_, _, upstream, router, cleanup := newGrokCredentialFailoverHandler(t, "mixed_429_500")
		defer cleanup()
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", bytes.NewBufferString(`{"model":"grok","input":"hello","stream":false}`))
		req.Header.Set("Content-Type", "application/json")

		router.ServeHTTP(recorder, req)

		require.Equal(t, http.StatusBadGateway, recorder.Code, recorder.Body.String())
		require.Equal(t, []int64{801, 802}, upstream.providerHits())
		require.NotContains(t, recorder.Body.String(), "upstream unavailable")
	})

	t.Run("500 then 429 permits one healthy followup", func(t *testing.T) {
		_, _, upstream, router, cleanup := newGrokCredentialFailoverHandler(t, "mixed_500_429")
		defer cleanup()
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", bytes.NewBufferString(`{"model":"grok","input":"hello","stream":false}`))
		req.Header.Set("Content-Type", "application/json")

		router.ServeHTTP(recorder, req)

		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		require.Equal(t, []int64{801, 802, 803}, upstream.providerHits())
	})

	t.Run("OAuth 429 then API-key failure cannot bypass the bound", func(t *testing.T) {
		_, _, upstream, router, cleanup := newGrokCredentialFailoverHandler(t, "oauth_429_apikey_500")
		defer cleanup()
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", bytes.NewBufferString(`{"model":"grok","input":"hello","stream":false}`))
		req.Header.Set("Content-Type", "application/json")

		router.ServeHTTP(recorder, req)

		require.Equal(t, http.StatusBadGateway, recorder.Code, recorder.Body.String())
		require.Equal(t, []int64{801, 802}, upstream.providerHits())
	})
}

func TestGrokMedia429FailoverIsBounded(t *testing.T) {
	t.Run("first 429 selects one healthy followup", func(t *testing.T) {
		_, _, upstream, router, cleanup := newGrokCredentialFailoverHandler(t, "first_429")
		defer cleanup()
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/openai/v1/videos/generations", bytes.NewBufferString(`{"model":"grok-imagine-video","prompt":"waves"}`))
		req.Header.Set("Content-Type", "application/json")

		router.ServeHTTP(recorder, req)

		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		require.Equal(t, []int64{801, 802}, upstream.providerHits())
	})

	t.Run("second 429 stops without sweeping a third provider", func(t *testing.T) {
		_, _, upstream, router, cleanup := newGrokCredentialFailoverHandler(t, "all_429")
		defer cleanup()
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/openai/v1/videos/generations", bytes.NewBufferString(`{"model":"grok-imagine-video","prompt":"waves"}`))
		req.Header.Set("Content-Type", "application/json")

		router.ServeHTTP(recorder, req)

		require.Equal(t, http.StatusTooManyRequests, recorder.Code, recorder.Body.String())
		require.Equal(t, []int64{801, 802}, upstream.providerHits())
		require.NotContains(t, recorder.Body.String(), "rate limited")
	})
}

func TestGrokOAuthCredentialFailoverAcrossHTTPHandlers(t *testing.T) {
	endpoints := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "messages", method: http.MethodPost, path: "/openai/v1/messages", body: `{"model":"grok","max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`},
		{name: "chat completions", method: http.MethodPost, path: "/openai/v1/chat/completions", body: `{"model":"grok","messages":[{"role":"user","content":"hello"}],"stream":false}`},
		{name: "chat completions raw fallback", method: http.MethodPost, path: "/openai/v1/chat/completions", body: `{"model":"grok","messages":[{"role":"user","content":"hello"}],"stop":["END"],"stream":false}`},
		{name: "grok media", method: http.MethodPost, path: "/openai/v1/videos/generations", body: `{"model":"grok-imagine-video","prompt":"waves"}`},
	}

	for _, endpoint := range endpoints {
		t.Run(endpoint.name+" revoked selects healthy", func(t *testing.T) {
			_, repo, upstream, router, cleanup := newGrokCredentialFailoverHandler(t, "revoked")
			defer cleanup()
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(endpoint.method, endpoint.path, bytes.NewBufferString(endpoint.body))
			req.Header.Set("Content-Type", "application/json")

			router.ServeHTTP(recorder, req)

			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			require.Equal(t, []int64{801}, repo.errorIDs())
			require.Equal(t, []int64{802}, upstream.providerHits())
		})

		t.Run(endpoint.name+" all providers exhausted safely", func(t *testing.T) {
			_, repo, upstream, router, cleanup := newGrokCredentialFailoverHandler(t, "all_revoked")
			defer cleanup()
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(endpoint.method, endpoint.path, bytes.NewBufferString(endpoint.body))
			req.Header.Set("Content-Type", "application/json")

			router.ServeHTTP(recorder, req)

			require.Equal(t, http.StatusServiceUnavailable, recorder.Code, recorder.Body.String())
			require.Contains(t, recorder.Body.String(), forwardcore.GrokCredentialUnavailableClientMessage)
			require.NotContains(t, recorder.Body.String(), "revoked-refresh")
			require.NotContains(t, recorder.Body.String(), "healthy-refresh")
			require.Equal(t, []int64{801, 802}, repo.errorIDs())
			require.Empty(t, upstream.providerHits())
		})
	}
}

func TestGrokOAuthMissingSelectedRowRetriesHealthyProviderWithoutMutation(t *testing.T) {
	_, repo, upstream, router, cleanup := newGrokCredentialFailoverHandler(t, "missing_row")
	defer cleanup()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", bytes.NewBufferString(`{"model":"grok","input":"hello","stream":false}`))
	req.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, []int64{802}, upstream.providerHits())
	require.Empty(t, repo.errorIDs())
	require.Empty(t, repo.setTempIDs)
}

func TestResponsesWebSocketCredentialFailoverLoop(t *testing.T) {
	dial := func(t *testing.T, router *gin.Engine) (*coderws.Conn, func()) {
		t.Helper()
		server := httptest.NewServer(router)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		conn, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/openai/v1/responses", nil)
		cancel()
		require.NoError(t, err)
		return conn, func() {
			_ = conn.CloseNow()
			server.Close()
		}
	}
	writeFirst := func(t *testing.T, conn *coderws.Conn) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		require.NoError(t, conn.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"grok","input":"hello","stream":false}`)))
	}

	t.Run("revoked provider selects healthy provider", func(t *testing.T) {
		_, repo, upstream, router, cleanup := newGrokCredentialFailoverHandler(t, "revoked")
		defer cleanup()
		conn, closeConn := dial(t, router)
		defer closeConn()
		writeFirst(t, conn)

		readCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, payload, err := conn.Read(readCtx)
		cancel()
		require.NoError(t, err)
		require.Contains(t, string(payload), "resp_healthy")
		require.Equal(t, []int64{801}, repo.errorIDs())
		require.Equal(t, 2, repo.selectorCalls())
		require.Equal(t, []int64{802}, upstream.providerHits())
	})

	t.Run("provider configuration stops", func(t *testing.T) {
		_, repo, upstream, router, cleanup := newGrokCredentialFailoverHandler(t, "provider")
		defer cleanup()
		conn, closeConn := dial(t, router)
		defer closeConn()
		writeFirst(t, conn)

		readCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, _, err := conn.Read(readCtx)
		cancel()
		var closeErr coderws.CloseError
		require.ErrorAs(t, err, &closeErr)
		require.Contains(t, closeErr.Reason, forwardcore.GrokCredentialUnavailableClientMessage)
		require.Equal(t, 1, repo.selectorCalls())
		require.Empty(t, upstream.providerHits())
	})

	t.Run("parent cancellation prevents reselection", func(t *testing.T) {
		_, repo, upstream, router, cleanup := newGrokCredentialFailoverHandler(t, "cancel")
		defer cleanup()
		conn, closeConn := dial(t, router)
		writeFirst(t, conn)
		select {
		case <-findHandlerRefresherStarted(router):
		case <-time.After(2 * time.Second):
			t.Fatal("credential refresh did not start")
		}
		closeConn()

		require.Eventually(t, func() bool { return repo.selectorCalls() == 1 }, 2*time.Second, 20*time.Millisecond)
		require.Empty(t, repo.errorIDs())
		require.Empty(t, upstream.providerHits())
	})
}

var handlerRefresherStarted sync.Map

func findHandlerRefresherStarted(router *gin.Engine) <-chan struct{} {
	value, _ := handlerRefresherStarted.Load(router)
	return testassert.MustType[chan struct{}](value)
}

func newGrokCredentialFailoverHandler(t *testing.T, mode string) (*gatewayHTTPEndpointsFixture, *grokCredentialHandlerRepo, *grokCredentialHandlerUpstream, *gin.Engine, func()) {
	t.Helper()
	groupID := int64(901)
	providers := []gatewayprovider.ExecutionProvider{
		{
			Record: providercore.Record{
				LoadLocation: time.LoadLocation, ID: 801, Name: "revoked", Platform: capability.PlatformGrok, Type: capability.ProviderTypeOAuth,
				Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 1,
				Credentials: map[string]any{
					"access_token": "expired", "refresh_token": "revoked-refresh",
					"expires_at": time.Now().Add(-time.Minute).UTC().Format(time.RFC3339),
				},
				Extra: map[string]any{providercore.GrokMediaEligibleExtraKey: true},
			},
		},
		{
			Record: providercore.Record{
				LoadLocation: time.LoadLocation, ID: 802, Name: "healthy", Platform: capability.PlatformGrok, Type: capability.ProviderTypeOAuth,
				Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 2,
				Credentials: map[string]any{
					"access_token": "healthy-access", "refresh_token": "healthy-refresh",
					"expires_at": time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339),
				},
				Extra: map[string]any{providercore.GrokMediaEligibleExtraKey: true},
			},
		},
	}
	if mode == "postmap_cancel" || mode == "first_402" || mode == "first_429" || mode == "all_429" || mode == "mixed_429_500" || mode == "mixed_500_429" || mode == "oauth_429_apikey_500" {
		providers[0].Record.Credentials["expires_at"] = time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	}
	if mode == "all_429" || mode == "mixed_429_500" || mode == "mixed_500_429" || mode == "oauth_429_apikey_500" {
		providers = append(providers, gatewayprovider.ExecutionProvider{
			Record: providercore.Record{
				LoadLocation: time.LoadLocation, ID: 803, Name: "untried-healthy", Platform: capability.PlatformGrok, Type: capability.ProviderTypeOAuth,
				Status: billing.StatusActive, Schedulable: true, Concurrency: 1, Priority: 3,
				Credentials: map[string]any{
					"access_token": "untried-healthy-access", "refresh_token": "untried-healthy-refresh",
					"expires_at": time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339),
				},
				Extra: map[string]any{providercore.GrokMediaEligibleExtraKey: true},
			},
		})
	}
	if mode == "oauth_429_apikey_500" {
		providers[1].Record.Type = capability.ProviderTypeAPIKey
		providers[1].Record.Credentials = map[string]any{"api_key": "third-party-key"}
	}
	if mode == "all_revoked" {
		providers[1].Record.Credentials["expires_at"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	}
	// 凭据恢复夹具在模型配置中开放请求别名。
	for i := range providers {
		providers[i].Record.Credentials["model_whitelist"] = []string{"*"}
		providers[i].Record.GroupIDs = []int64{groupID}
	}
	repo := &grokCredentialHandlerRepo{providers: providers, missingOnGet: map[int64]bool{}}
	if mode == "missing_row" {
		repo.missingOnGet[801] = true
	}
	if mode == "mutation_set_error" {
		repo.setErrorErr = errors.New("database write failed")
	}
	if mode == "mutation_temp" {
		repo.setTempErr = errors.New("database write failed")
	}
	refresher := &grokCredentialHandlerRefresher{mode: mode, started: make(chan struct{})}
	tokenCache := &grokCredentialHandlerTokenCache{}
	if mode == "mutation_cache" {
		tokenCache.deleteErr = errors.New("cache delete failed")
	}
	var provider *providercore.GrokTokenSource
	if mode != "nil_provider" {
		refresh := providercore.NewOAuthRefreshAPI(grokCredentialTokenReader{repo}, tokenCache, providercore.RefreshOptions{Now: time.Now, Warn: slog.Warn, Info: slog.Info, Error: slog.Error, Platform: providercore.ProviderRefreshPlatformPolicy()})
		provider = &providercore.GrokTokenSource{
			Repository: grokCredentialTokenReader{repo}, Cache: tokenCache,
			Policy: providercore.GrokProviderRefreshPolicy(),
			Refresh: func(ctx context.Context, record *providercore.Record, window time.Duration) (*providercore.OAuthRefreshResult, error) {
				return refresh.RefreshIfNeeded(ctx, record, refresher, window)
			},
		}
	}
	upstream := &grokCredentialHandlerUpstream{}
	switch mode {
	case "first_402":
		upstream.failProviderID = 801
	case "first_429":
		upstream.rateLimitIDs = map[int64]bool{801: true}
	case "all_429":
		upstream.rateLimitIDs = map[int64]bool{801: true, 802: true}
	case "mixed_429_500":
		upstream.rateLimitIDs = map[int64]bool{801: true}
		upstream.failureStatus = map[int64]int{802: http.StatusInternalServerError}
	case "mixed_500_429":
		upstream.failureStatus = map[int64]int{801: http.StatusInternalServerError}
		upstream.rateLimitIDs = map[int64]bool{802: true}
	case "oauth_429_apikey_500":
		upstream.rateLimitIDs = map[int64]bool{801: true}
		upstream.failureStatus = map[int64]int{802: http.StatusInternalServerError}
	}
	cfg := &config.Config{}
	cfg.Gateway.MaxProviderSwitches = 3
	billingCache := newBillingEligibilityFixture(cfg)
	billingCache.Start()
	completionInput2 := billingtestkit.Calculator(nil, nil)
	completionInput3 := &providercore.DeferredService{}
	gateway, gatewayChoices, gatewayCredentialPort := newOpenAIExecutionAndSelectionFixture(
		repo, nil, cfg, nil, nil, nil, upstream,
		nil, completionInput3, newOpenAIExecutionCredentialsForTest(repo,
			provider), provider, nil, nil, nil, nil, responseHeaderFilterForTest(cfg), nil, nil, nil,
	)
	gateway.Recorder = newHTTPCompletionFixture(cfg, nil, completionInput2, billingCache, completionInput3, nil, nil, true)

	cache := &httptestkit.ConcurrencyHooks{
		AcquireUserSlotFn:     func(context.Context, int64, int, string) (bool, error) { return true, nil },
		AcquireProviderSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil },
	}
	h := newGatewayHTTPEndpointsFromDeps(gateway, gatewayCredentialPort, scheduler.NewConcurrencyService(cache, scheduler.Diagnostics{
		Logf:  logging.LegacyPrintf,
		Event: logging.Event,
	},
	), newFundingAdmissionFixture(billingCache, cfg), &apikey.APIKeyService{}, nil, nil, nil, nil, cfg, nil, newExecutionAvailabilityForTest(repo,

		nil, cfg), gatewayChoices,
	)
	apiKey := &apikey.APIKey{
		ID: 902, GroupID: &groupID,
		User: &identity.User{ID: 903, Status: billing.StatusActive},
		Group: &routing.Group{
			ID:                   groupID,
			Status:               billing.StatusActive,
			AllowImageGeneration: true,
			AllowedProtocols: []protocol.ProtocolID{
				protocol.ProtocolAnthropicMessages,
				protocol.ProtocolOpenAIResponses,
				protocol.ProtocolOpenAIChatCompletions,
			},
		},
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(keyhttp.ContextKeyAPIKey), apiKey)
		c.Set(string(authctx.ContextKeyUser), authctx.AuthSubject{UserID: apiKey.User.ID, Concurrency: 1})
		c.Next()
	})
	router.POST("/openai/v1/responses", h.Responses)
	router.GET("/openai/v1/responses", h.ResponsesWebSocket)
	router.POST("/openai/v1/messages", h.Messages)
	router.POST("/openai/v1/chat/completions", h.ChatCompletions)
	router.POST("/openai/v1/videos/generations", h.GrokVideoGeneration)
	router.GET("/openai/v1/videos/:request_id", h.GrokVideoStatus)
	handlerRefresherStarted.Store(router, refresher.started)
	cleanup := func() {
		handlerRefresherStarted.Delete(router)
		billingCache.Stop()
	}
	return h, repo, upstream, router, cleanup
}
