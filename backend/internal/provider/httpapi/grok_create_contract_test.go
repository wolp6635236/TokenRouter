package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/billing"

	"github.com/TokenFlux/TokenRouter/internal/provider"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type grokImportAdminService struct {
	*managementMutationFixture
	mu     sync.Mutex
	nextID int64
}

func newGrokImportAdminService() *grokImportAdminService {
	return &grokImportAdminService{
		managementMutationFixture: newManagementMutationFixture(),
		nextID:                    500,
	}
}

func (s *grokImportAdminService) CreateProvider(_ context.Context, input *provider.CreateProviderInput) (*provider.Record, error) {
	s.mu.Lock()
	s.nextID++
	id := s.nextID
	s.mu.Unlock()
	return &provider.Record{
		ID:          id,
		Name:        input.Name,
		Platform:    input.Platform,
		Type:        input.Type,
		Credentials: input.Credentials,
		Extra:       input.Extra,
		ProxyID:     input.ProxyID,
		Concurrency: input.Concurrency,
		Status:      billing.StatusActive,
		Schedulable: true,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}, nil
}

func TestProviderCreateWithoutAutomaticGrokProbeServiceStillSucceeds(t *testing.T) {
	source := newGrokImportAdminService()
	presenter := NewRuntimePresenter(provider.NewRuntimeStatusReader(provider.RuntimeStatusOptions{}), source, nil)
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
