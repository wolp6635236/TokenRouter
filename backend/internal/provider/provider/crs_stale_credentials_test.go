package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/upstream/openai"
	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

// 在 CRS 交换 token 期间修改持久凭据，检查迟到结果的处理。
type crsStaleCredentialRepo struct {
	providercore.CRSProviderStore
	current *providercore.Record
}

func (r *crsStaleCredentialRepo) Create(_ context.Context, v *providercore.Record) error {
	v.ID = 77
	r.current = crsStaleCopy(v)
	return nil
}

func (r *crsStaleCredentialRepo) GetByCRSAccountID(context.Context, string) (*providercore.Record, error) {
	return nil, nil
}

func (r *crsStaleCredentialRepo) GetByID(context.Context, int64) (*providercore.Record, error) {
	return crsStaleCopy(r.current), nil
}

func (r *crsStaleCredentialRepo) UpdateCredentials(_ context.Context, _ int64, v map[string]any) error {
	r.current.Credentials = v
	return nil
}

func crsStaleCopy(v *providercore.Record) *providercore.Record {
	if v == nil {
		return nil
	}
	out := *v
	out.Credentials = map[string]any{}
	for k, x := range v.Credentials {
		out.Credentials[k] = x
	}
	return &out
}

type crsStaleOAuthClient struct {
	OpenAIOAuthClient
	repo  *crsStaleCredentialRepo
	calls int
}

func (c *crsStaleOAuthClient) RefreshTokenWithClientID(context.Context, string, string, string, ...openai.OAuthTokenRequestOptions) (*openai.TokenResponse, error) {
	c.calls++
	c.repo.current.Credentials = map[string]any{"access_token": "admin-new-at", "refresh_token": "admin-new-rt"}
	return &openai.TokenResponse{AccessToken: "late-at", RefreshToken: "late-rt", ExpiresIn: 3600}, nil
}

func TestCRSRefreshPreservesConcurrentAdministratorCredentials(t *testing.T) {
	repo := &crsStaleCredentialRepo{}
	client := &crsStaleOAuthClient{repo: repo}
	deps := &OpenAIAuthorizationDependencies{}
	oauth := newOpenAIAuthorizationForTest(t, nil, client, deps)
	deps.PrivacyFactory = func(string) (*req.Client, error) { return nil, errors.New("local fixture: no enrichment") }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/web/auth/login" {
			_, _ = w.Write([]byte(`{"success":true,"token":"local"}`))
			return
		}
		require.Equal(t, "/admin/sync/export-accounts", r.URL.Path)
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"openaiOAuthAccounts": []any{map[string]any{"id": "source-77", "name": "local", "isActive": true, "schedulable": true, "credentials": map[string]any{"access_token": "source-at", "refresh_token": "source-rt"}}}}}))
	}))
	defer server.Close()
	exchange := &providercore.CRSAuthorization{OpenAI: oauth}
	coordinator := providercore.NewOAuthRefreshAPI(repo, nil, providercore.RefreshOptions{})
	runtime := providercore.NewCRSSync(repo, nil, NewCRSClient(CRSClientOptions{Configured: true, AllowInsecureHTTP: true}), providercore.CRSOptions{Refresh: exchange.Coordinated(coordinator, GeminiTokenCacheKey)})
	result, err := runtime.SyncFromCRS(context.Background(), providercore.SyncFromCRSInput{BaseURL: server.URL, Username: "local", Password: "local"})
	require.NoError(t, err)
	require.Equal(t, 1, result.Created)
	require.Equal(t, 1, client.calls)
	require.Equal(t, "admin-new-at", repo.current.Credentials["access_token"])
	require.Equal(t, "admin-new-rt", repo.current.Credentials["refresh_token"])
}
