package provider

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/pkg/querycache"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/upstream/openai"
)

type agentIdentityCredentialsRepo struct {
	ExecutionProviderStore

	credentials map[string]any
	provider    *ExecutionProvider
	mu          sync.Mutex
}

func newTestAgentIdentityKey(t *testing.T) (openai.AgentIdentityKey, string) {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(privateKey)
	require.NoError(t, err)
	return openai.AgentIdentityKey{
		RuntimeID:  "runtime-test",
		PrivateKey: privateKey,
		TaskID:     "task-test",
	}, base64.StdEncoding.EncodeToString(der)
}

func TestEnsureAgentIdentityTaskPersistsAndRedactsCredentials(t *testing.T) {
	key, privateKey := newTestAgentIdentityKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"task_id":"task-persisted"}`))
	}))
	defer server.Close()
	coordinator := &providercore.OpenAITaskCoordinator{}
	register := func(ctx context.Context, value *providercore.Record) (string, error) {
		return provideradapter.RegisterAgentIdentityTask(ctx, value, server.URL)
	}

	repo := &agentIdentityCredentialsRepo{}
	provider := &ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, ID: 7, Type: capability.ProviderTypeOAuth, Platform: capability.PlatformOpenAI, Credentials: map[string]any{
		"auth_mode":          providercore.OpenAIAuthModeAgentIdentity,
		"agent_runtime_id":   key.RuntimeID,
		"agent_private_key":  privateKey,
		"chatgpt_account_id": "provider-test",
	}}}
	service := NewExecutionAgentIdentity(coordinator, repo, register, nil)
	require.NoError(t, service.Ensure(context.Background(), provider, ""))
	require.Equal(t, "task-persisted", provider.View().GetCredential("task_id"))
	require.Equal(t, "task-persisted", repo.credentials["task_id"])
	require.True(t, providercore.IsSensitiveCredentialKey("agent_private_key"))
	redacted := make(map[string]any)
	for key, value := range provider.Record.Credentials {
		if !providercore.IsSensitiveCredentialKey(key) {
			redacted[key] = value
		}
	}
	require.NotContains(t, string(mustAgentIdentityJSON(t, redacted)), privateKey)
}

func TestEnsureAgentIdentityTaskSharesLockAcrossServicesForSameProvider(t *testing.T) {
	key, privateKey := newTestAgentIdentityKey(t)
	provider := &ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, ID: 9001, Type: capability.ProviderTypeOAuth, Platform: capability.PlatformOpenAI, Credentials: map[string]any{
		"auth_mode":         providercore.OpenAIAuthModeAgentIdentity,
		"agent_runtime_id":  key.RuntimeID,
		"agent_private_key": privateKey,
	}}}
	repo := &agentIdentityCredentialsRepo{provider: provider}
	registerCalls := 0
	var registerMu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		registerMu.Lock()
		registerCalls++
		registerMu.Unlock()
		_, _ = w.Write([]byte(`{"task_id":"task-shared"}`))
	}))
	defer server.Close()
	coordinator := &providercore.OpenAITaskCoordinator{}
	register := func(ctx context.Context, value *providercore.Record) (string, error) {
		return provideradapter.RegisterAgentIdentityTask(ctx, value, server.URL)
	}

	start := make(chan struct{})
	errors := make(chan error, 2)
	requests := []*ExecutionProvider{cloneAgentIdentityTestProvider(provider), cloneAgentIdentityTestProvider(provider)}
	for _, request := range requests {
		go func() {
			<-start
			errors <- NewExecutionAgentIdentity(coordinator, repo, register, nil).Ensure(context.Background(), request, "")
		}()
	}
	close(start)
	require.NoError(t, <-errors)
	require.NoError(t, <-errors)
	registerMu.Lock()
	defer registerMu.Unlock()
	require.Equal(t, 1, registerCalls)
	require.Equal(t, "task-shared", repo.provider.View().GetCredential("task_id"))
}

func cloneAgentIdentityTestProvider(provider *ExecutionProvider) *ExecutionProvider {
	copy := *provider
	copy.Record.Credentials = querycache.ShallowMap(provider.Record.Credentials)
	return &copy
}

func (r *agentIdentityCredentialsRepo) GetByID(_ context.Context, _ int64) (*ExecutionProvider, error) {
	return r.provider, nil
}

func (r *agentIdentityCredentialsRepo) UpdateCredentials(_ context.Context, _ int64, credentials map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.credentials = credentials
	return nil
}

func mustAgentIdentityJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	return encoded
}
