package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/idempotency"
	idempotencytest "github.com/TokenFlux/TokenRouter/internal/idempotency/testkit"
	"github.com/TokenFlux/TokenRouter/internal/identity/httpapi/authctx"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

type duplicateProviderAdminServiceStub struct {
	ProviderManagement
	provider     *providercore.Record
	calls        int
	recoverCalls int
	providerID   int64
	actorScope   string
	operationKey string
	recoverScope string
	recoverKey   string
	recoverErr   error
	created      bool
}

type blockingDuplicateAdminServiceStub struct {
	ProviderManagement
	provider     *providercore.Record
	started      chan struct{}
	release      chan struct{}
	calls        atomic.Int32
	recoverCalls atomic.Int32
	recoverErr   error
}

type failOnceMarkSucceededRepo struct {
	*idempotencytest.MemoryStore
	failNext bool
}

func TestProviderCreateWithoutAutomaticGrokProbeServiceStillSucceeds(t *testing.T) {
	source := newGrokImportAdminService()
	presenter := NewRuntimePresenter(providercore.NewRuntimeStatusReader(providercore.RuntimeStatusOptions{}), source, nil)
	handler := NewManagementHandler(source, ManagementOptions{Presenter: presenter, Privacy: source})

	router := gin.New()
	router.POST("/api/v1/admin/providers", handler.Create)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/providers",
		strings.NewReader(`{"name":"grok-rt","platform":"grok","type":"oauth","credentials":{"refresh_token":"secret"}}`),
	)
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
}

func TestProviderUpdateDeprecatedOnlyIsNormalizedToNoExtraUpdate(t *testing.T) {
	stub := newManagementMutationFixture()
	handler := newMutationHandler(stub, nil)
	router := gin.New()
	router.PUT("/providers/:id", handler.Update)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/providers/1", bytes.NewBufferString(
		`{"extra":{"openai_long_context_billing_enabled":{"malformed":true}}}`,
	))
	request.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.NotNil(t, stub.updateProviderInput)
	require.Nil(t, stub.updateProviderInput.Extra)
}

func TestDuplicateProviderHandlerRedactsCredentials(t *testing.T) {
	svc := &duplicateProviderAdminServiceStub{
		provider: &providercore.Record{
			ID:          43,
			Name:        "primary (Copy)",
			Platform:    capability.PlatformAnthropic,
			Type:        capability.ProviderTypeAPIKey,
			Status:      billing.StatusActive,
			Schedulable: false,
			Credentials: map[string]any{"api_key": "top-secret-key"},
		},
	}
	router := setupDuplicateProviderRouter(t, svc)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/providers/42/duplicate", nil)

	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, svc.calls)
	require.Contains(t, recorder.Body.String(), `"name":"primary (Copy)"`)
	require.NotContains(t, recorder.Body.String(), "top-secret-key")
	var responseBody struct {
		Data struct {
			Credentials map[string]any `json:"credentials"`
			Schedulable bool           `json:"schedulable"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &responseBody))
	require.Empty(t, responseBody.Data.Credentials)
	require.False(t, responseBody.Data.Schedulable)
}

func TestDuplicateProviderHandlerRejectsInvalidID(t *testing.T) {
	svc := &duplicateProviderAdminServiceStub{}
	router := setupDuplicateProviderRouter(t, svc)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/providers/not-a-number/duplicate", nil)

	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Zero(t, svc.calls)
}

func TestDuplicateProviderHandlerReplaysSameIdempotencyKey(t *testing.T) {
	svc := &duplicateProviderAdminServiceStub{
		provider: &providercore.Record{
			ID:          43,
			Name:        "primary (Copy)",
			Platform:    capability.PlatformAnthropic,
			Type:        capability.ProviderTypeAPIKey,
			Status:      billing.StatusActive,
			Schedulable: false,
		},
	}
	repo := idempotencytest.NewMemoryStore()
	coordinator := idempotency.NewIdempotencyCoordinator(repo, idempotency.DefaultIdempotencyConfig())
	router := setupDuplicateProviderRouter(t, svc, coordinator)

	call := func() *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/providers/42/duplicate", nil)
		request.Header.Set("Idempotency-Key", "duplicate-provider-42")
		router.ServeHTTP(recorder, request)
		return recorder
	}

	first := call()
	second := call()

	require.Equal(t, http.StatusOK, first.Code)
	require.Equal(t, http.StatusOK, second.Code)
	require.Equal(t, 1, svc.calls)
	require.Equal(t, int64(42), svc.providerID)
	require.Equal(t, "admin:77", svc.actorScope)
	require.Equal(t, "duplicate-provider-42", svc.operationKey)
	require.Equal(t, "true", second.Header().Get("X-Idempotency-Replayed"))
}

func TestDuplicateProviderHandlerRecoversAfterMarkSucceededFailure(t *testing.T) {
	svc := &duplicateProviderAdminServiceStub{
		provider: &providercore.Record{
			ID:          43,
			Name:        "primary (Copy)",
			Platform:    capability.PlatformAnthropic,
			Type:        capability.ProviderTypeAPIKey,
			Status:      billing.StatusActive,
			Schedulable: false,
		},
	}
	repo := &failOnceMarkSucceededRepo{MemoryStore: idempotencytest.NewMemoryStore(), failNext: true}
	coordinator := idempotency.NewIdempotencyCoordinator(repo, idempotency.DefaultIdempotencyConfig())
	router := setupDuplicateProviderRouter(t, svc, coordinator)

	call := func() *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/providers/42/duplicate", nil)
		request.Header.Set("Idempotency-Key", "duplicate-provider-42-recovery")
		router.ServeHTTP(recorder, request)
		return recorder
	}

	first := call()
	second := call()

	require.Equal(t, http.StatusOK, first.Code)
	require.Equal(t, http.StatusOK, second.Code)
	require.Equal(t, "true", first.Header().Get("X-Idempotency-Recovered"))
	require.Equal(t, "true", second.Header().Get("X-Idempotency-Recovered"))
	require.Equal(t, 1, svc.calls, "ambiguous retries must not repeat the create side effect")
	require.Equal(t, 2, svc.recoverCalls)
	require.Equal(t, "admin:77", svc.recoverScope)
	require.Equal(t, "duplicate-provider-42-recovery", svc.recoverKey)
	require.Contains(t, second.Body.String(), `"id":43`)
}

func TestDuplicateProviderHandlerPreservesIdempotencyErrorWhenRecoveryLookupFails(t *testing.T) {
	svc := &duplicateProviderAdminServiceStub{
		provider: &providercore.Record{
			ID:          43,
			Name:        "primary (Copy)",
			Platform:    capability.PlatformAnthropic,
			Type:        capability.ProviderTypeAPIKey,
			Status:      billing.StatusActive,
			Schedulable: false,
		},
		recoverErr: errors.New("recovery database unavailable"),
	}
	repo := &failOnceMarkSucceededRepo{MemoryStore: idempotencytest.NewMemoryStore(), failNext: true}
	coordinator := idempotency.NewIdempotencyCoordinator(repo, idempotency.DefaultIdempotencyConfig())
	router := setupDuplicateProviderRouter(t, svc, coordinator)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/providers/42/duplicate", nil)
	request.Header.Set("Idempotency-Key", "duplicate-provider-42-recovery-error")

	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Contains(t, recorder.Body.String(), "IDEMPOTENCY_STORE_UNAVAILABLE")
	require.Equal(t, 1, svc.calls)
	require.Equal(t, 1, svc.recoverCalls)
	require.Equal(t, "admin:77", svc.recoverScope)
}

func TestDuplicateProviderHandlerDoesNotReexecuteWhileOriginalIsProcessing(t *testing.T) {
	svc := &blockingDuplicateAdminServiceStub{
		provider: &providercore.Record{
			ID:          43,
			Name:        "primary (Copy)",
			Platform:    capability.PlatformAnthropic,
			Type:        capability.ProviderTypeAPIKey,
			Status:      billing.StatusActive,
			Schedulable: false,
		},
		started:    make(chan struct{}),
		release:    make(chan struct{}),
		recoverErr: errors.New("recovery database unavailable"),
	}
	coordinator := idempotency.NewIdempotencyCoordinator(idempotencytest.NewMemoryStore(), idempotency.DefaultIdempotencyConfig())
	router := setupDuplicateProviderRouter(t, svc, coordinator)

	call := func() *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/providers/42/duplicate", nil)
		request.Header.Set("Idempotency-Key", "duplicate-provider-42-active")
		router.ServeHTTP(recorder, request)
		return recorder
	}
	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() { firstDone <- call() }()
	<-svc.started

	second := call()
	require.Equal(t, http.StatusConflict, second.Code)
	require.Contains(t, second.Body.String(), "IDEMPOTENCY_IN_PROGRESS")
	require.Equal(t, int32(1), svc.calls.Load())
	require.Equal(t, int32(1), svc.recoverCalls.Load())

	close(svc.release)
	select {
	case first := <-firstDone:
		require.Equal(t, http.StatusOK, first.Code)
	case <-time.After(time.Second):
		t.Fatal("original duplicate request did not finish")
	}
}

func TestProviderHandler_Create_AnthropicAPIKeyPassthroughExtraForwarded(t *testing.T) {
	adminSvc := &managementCreateFixture{}
	handler := newManagementCreateFixtureHandler(adminSvc)

	router := gin.New()
	router.POST("/api/v1/admin/providers", handler.Create)

	body := map[string]any{
		"name":     "anthropic-key-1",
		"platform": "anthropic",
		"type":     "apikey",
		"credentials": map[string]any{
			"api_key":  "sk-ant-xxx",
			"base_url": "https://api.anthropic.com",
		},
		"extra": map[string]any{
			"anthropic_passthrough": true,
		},
		"concurrency": 1,
		"priority":    1,
	}
	raw, err := json.Marshal(body)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/providers", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, adminSvc.createdProviders, 1)

	created := adminSvc.createdProviders[0]
	require.Equal(t, "anthropic", created.Platform)
	require.Equal(t, "apikey", created.Type)
	require.NotNil(t, created.Extra)
	require.Equal(t, true, created.Extra["anthropic_passthrough"])
}

func (r *failOnceMarkSucceededRepo) MarkSucceeded(ctx context.Context, id int64, responseStatus int, responseBody string, expiresAt time.Time) error {
	if r.failNext {
		r.failNext = false
		return errors.New("mark succeeded failed")
	}
	return r.MemoryStore.MarkSucceeded(ctx, id, responseStatus, responseBody, expiresAt)
}

func (s *duplicateProviderAdminServiceStub) DuplicateProvider(_ context.Context, providerID int64, actorScope, operationKey string) (*providercore.Record, error) {
	s.calls++
	s.providerID = providerID
	s.actorScope = actorScope
	s.operationKey = operationKey
	s.created = true
	return s.provider, nil
}

func (s *duplicateProviderAdminServiceStub) RecoverDuplicateProvider(_ context.Context, _ int64, actorScope, operationKey string) (*providercore.Record, error) {
	s.recoverCalls++
	s.recoverScope = actorScope
	s.recoverKey = operationKey
	if s.recoverErr != nil {
		return nil, s.recoverErr
	}
	if !s.created {
		return nil, nil
	}
	return s.provider, nil
}

func (s *blockingDuplicateAdminServiceStub) DuplicateProvider(_ context.Context, _ int64, _, _ string) (*providercore.Record, error) {
	s.calls.Add(1)
	close(s.started)
	<-s.release
	return s.provider, nil
}

func (s *blockingDuplicateAdminServiceStub) RecoverDuplicateProvider(_ context.Context, _ int64, _, _ string) (*providercore.Record, error) {
	s.recoverCalls.Add(1)
	return nil, s.recoverErr
}

func setupDuplicateProviderRouter(t *testing.T, svc ProviderManagement, coordinators ...*idempotency.IdempotencyCoordinator) *gin.Engine {
	t.Helper()

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(authctx.ContextKeyUser), authctx.AuthSubject{UserID: 77})
		c.Next()
	})
	handler := NewManagementHandler(svc, ManagementOptions{Presenter: NewRuntimePresenter(providercore.NewRuntimeStatusReader(providercore.RuntimeStatusOptions{}), svc, nil)})
	if len(coordinators) > 0 {
		handler.BindIdempotency(coordinators[0])
	}
	router.POST("/api/v1/admin/providers/:id/duplicate", handler.Duplicate)
	return router
}
