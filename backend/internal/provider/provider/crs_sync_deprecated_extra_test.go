package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

type crsDeprecatedExtraProviderRepo struct {
	providercore.CRSProviderStore
	providers map[string]*providercore.Record
	nextID    int64
}

type crsOpenAIDeprecatedExtraSource struct {
	collection  string
	credentials map[string]any
	extra       map[string]any
}

func newCRSDeprecatedExtraProviderRepo(existing ...*providercore.Record) *crsDeprecatedExtraProviderRepo {
	repo := &crsDeprecatedExtraProviderRepo{providers: make(map[string]*providercore.Record)}
	for _, provider := range existing {
		if provider == nil {
			continue
		}
		crsID, _ := provider.Extra["crs_account_id"].(string)
		repo.providers[crsID] = provider
		if provider.ID > repo.nextID {
			repo.nextID = provider.ID
		}
	}
	return repo
}

func (r *crsDeprecatedExtraProviderRepo) Create(_ context.Context, provider *providercore.Record) error {
	r.nextID++
	provider.ID = r.nextID
	crsID, _ := provider.Extra["crs_account_id"].(string)
	r.providers[crsID] = provider
	return nil
}

func (r *crsDeprecatedExtraProviderRepo) Update(_ context.Context, provider *providercore.Record) error {
	crsID, _ := provider.Extra["crs_account_id"].(string)
	r.providers[crsID] = provider
	return nil
}

func (r *crsDeprecatedExtraProviderRepo) GetByCRSAccountID(_ context.Context, crsID string) (*providercore.Record, error) {
	return r.providers[crsID], nil
}

func (r *crsDeprecatedExtraProviderRepo) ListShadowsByParent(_ context.Context, _ int64) ([]*providercore.Record, error) {
	return nil, nil
}

func TestCRSSyncDiscardsDeprecatedOpenAILongContextBillingExtra(t *testing.T) {
	tests := []struct {
		name          string
		collection    string
		credentials   map[string]any
		sourceValue   any
		existingValue any
		wantAction    string
	}{
		{name: "OAuth create discards true", collection: "openaiOAuthAccounts", credentials: map[string]any{"access_token": "oauth-token"}, sourceValue: true, wantAction: "created"},
		{name: "OAuth create discards malformed", collection: "openaiOAuthAccounts", credentials: map[string]any{"access_token": "oauth-token"}, sourceValue: "false", wantAction: "created"},
		{name: "API key create discards false", collection: "openaiResponsesAccounts", credentials: map[string]any{"api_key": "sk-test"}, sourceValue: false, wantAction: "created"},
		{name: "OAuth update discards existing", collection: "openaiOAuthAccounts", credentials: map[string]any{"access_token": "oauth-token"}, existingValue: true, wantAction: "updated"},
		{name: "API key update discards source and existing", collection: "openaiResponsesAccounts", credentials: map[string]any{"api_key": "sk-test"}, sourceValue: []bool{true}, existingValue: false, wantAction: "updated"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const crsID = "crs-openai-1"
			var existing *providercore.Record
			if tt.existingValue != nil {
				providerType := capability.ProviderTypeOAuth
				if tt.collection == "openaiResponsesAccounts" {
					providerType = capability.ProviderTypeAPIKey
				}
				existing = &providercore.Record{
					ID:       41,
					Platform: capability.PlatformOpenAI,
					Type:     providerType,
					Extra: map[string]any{
						"crs_account_id":                      crsID,
						"openai_long_context_billing_enabled": tt.existingValue,
						"existing_preserved":                  true,
					},
				}
			}
			repo := newCRSDeprecatedExtraProviderRepo(existing)
			sourceExtra := map[string]any{"source_preserved": true}
			if tt.sourceValue != nil {
				sourceExtra["openai_long_context_billing_enabled"] = tt.sourceValue
			}
			result := runCRSOpenAIDeprecatedExtraSync(t, repo, crsOpenAIDeprecatedExtraSource{
				collection:  tt.collection,
				credentials: tt.credentials,
				extra:       sourceExtra,
			})

			require.Len(t, result.Items, 1)
			require.Equal(t, tt.wantAction, result.Items[0].Action)
			stored := repo.providers[crsID]
			require.NotNil(t, stored)
			require.NotContains(t, stored.Extra, "openai_long_context_billing_enabled")
			require.Equal(t, true, stored.Extra["source_preserved"])
			if existing != nil {
				require.Equal(t, true, stored.Extra["existing_preserved"])
			}
		})
	}
}

func runCRSOpenAIDeprecatedExtraSync(t *testing.T, repo providercore.CRSProviderStore, source crsOpenAIDeprecatedExtraSource) *providercore.SyncFromCRSResult {
	t.Helper()
	provider := map[string]any{
		"kind":        "openai",
		"id":          "crs-openai-1",
		"name":        "OpenAI CRS",
		"isActive":    true,
		"schedulable": true,
		"credentials": source.credentials,
		"extra":       source.extra,
	}

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/web/auth/login" {
			_, _ = response.Write([]byte(`{"success":true,"token":"admin-token"}`))
			return
		}
		require.Equal(t, "/admin/sync/export-accounts", request.URL.Path)
		require.NoError(t, json.NewEncoder(response).Encode(map[string]any{
			"success": true,
			"data":    map[string]any{source.collection: []any{provider}},
		}))
	}))
	t.Cleanup(server.Close)

	service := providercore.NewCRSSync(repo, nil, NewCRSClient(CRSClientOptions{Configured: true, AllowInsecureHTTP: true}), providercore.CRSOptions{})
	result, err := service.SyncFromCRS(context.Background(), providercore.SyncFromCRSInput{
		BaseURL:  server.URL,
		Username: "admin",
		Password: "password",
	})
	require.NoError(t, err)
	return result
}

// UpdateConfiguration 夹具保存本次配置更新，调用由测试顺序执行。
func (r *crsDeprecatedExtraProviderRepo) UpdateConfiguration(ctx context.Context, record *providercore.Record, _ providercore.ConfigurationChange) error {
	return r.Update(ctx, record)
}
