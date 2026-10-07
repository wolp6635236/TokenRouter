package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	keyhttp "github.com/TokenFlux/TokenRouter/internal/apikey/httpapi"
	"github.com/TokenFlux/TokenRouter/internal/protocol"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/upstream/gemini"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestGeminiV1BetaListUsesMixedGroupCapabilitiesAndAliases 检查 Gemini 与普通模型目录共用候选，自定义列表及 Key 别名取可用候选的交集。
func TestGeminiV1BetaListUsesMixedGroupCapabilitiesAndAliases(t *testing.T) {
	groupID := int64(42)
	source := &gatewayModelsProviderRepoStub{byGroup: map[int64][]provider.Record{groupID: {
		{ID: 1, Platform: "gemini", Type: "apikey", Credentials: map[string]any{"model_whitelist": []string{"gemini-2.5-pro", "gemini-custom", "gemini-2.5-flash"}}},
		{ID: 2, Platform: "anthropic", Type: "apikey", Credentials: map[string]any{"model_whitelist": []string{"claude-sonnet-4-6"}}},
	}}}
	handler := newGatewayModelsHandlerForTest(source)
	key := &apikey.APIKey{GroupID: &groupID, Group: &routing.Group{ID: groupID, AllowedProtocols: []protocol.ProtocolID{protocol.ProtocolGeminiGenerateContent}, ModelsListConfig: routing.GroupModelsListConfig{Enabled: true, Models: []string{"gemini-2.5-pro", "gemini-custom", "phantom", "claude-sonnet-4-6"}}}, ModelMapping: map[string]string{"my-gemini": "gemini-2.5-pro", "custom-alias": "gemini-custom", "unlisted-alias": "gemini-2.5-flash", "wildcard-*": "gemini-2.5-pro"}}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1beta/models", nil)
	c.Set(string(keyhttp.ContextKeyAPIKey), key)
	handler.GeminiV1BetaListModels(c)
	require.Equal(t, 200, rec.Code)
	var got gemini.ModelsListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got.Models, 4)
	pro, custom := gemini.FallbackModel("gemini-2.5-pro"), gemini.FallbackModel("gemini-custom")
	require.Equal(t, []gemini.Model{pro, custom}, got.Models[:2])
	pro.Name, pro.DisplayName = "models/my-gemini", "my-gemini"
	custom.Name, custom.DisplayName = "models/custom-alias", "custom-alias"
	require.ElementsMatch(t, []gemini.Model{pro, custom}, got.Models[2:])
	for _, test := range []struct {
		name   string
		status int
	}{{"my-gemini", 200}, {"gemini-2.5-pro", 200}, {"phantom", 404}, {"gemini-2.5-flash", 404}, {"claude-sonnet-4-6", 404}} {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodGet, "/v1beta/models/"+test.name, nil)
		c.Params = gin.Params{{Key: "model", Value: "/" + test.name}}
		c.Set(string(keyhttp.ContextKeyAPIKey), key)
		handler.GeminiV1BetaGetModel(c)
		require.Equal(t, test.status, rec.Code, test.name)
	}
}

func TestGeminiV1BetaCustomListCannotInventProviders(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1beta/models", nil)
	c.Set(string(keyhttp.ContextKeyAPIKey), &apikey.APIKey{Group: &routing.Group{ID: 42, AllowedProtocols: []protocol.ProtocolID{protocol.ProtocolGeminiGenerateContent}, ModelsListConfig: routing.GroupModelsListConfig{Enabled: true, Models: []string{"gemini-2.5-pro"}}}})
	provideModelsHTTP(nil, nil, nil, nil).GeminiV1BetaListModels(c)
	require.Equal(t, 200, rec.Code)
	require.JSONEq(t, `{"models":[]}`, rec.Body.String())
}

func TestGeminiV1BetaForcedAntigravityKeepsGroupRestrictions(t *testing.T) {
	groupID := int64(43)
	source := &gatewayModelsProviderRepoStub{byGroup: map[int64][]provider.Record{groupID: {
		{ID: 1, Platform: "antigravity", Type: "oauth", Credentials: map[string]any{"model_whitelist": []string{"gemini-3-flash"}}},
		{ID: 2, Platform: "gemini", Type: "apikey", Credentials: map[string]any{"model_whitelist": []string{"gemini-2.5-pro"}}},
	}}}
	handler := newGatewayModelsHandlerForTest(source)
	group := &routing.Group{ID: groupID, AllowedProtocols: []protocol.ProtocolID{protocol.ProtocolGeminiGenerateContent}, ModelsListConfig: routing.GroupModelsListConfig{Enabled: true, Models: []string{"gemini-3-flash", "gemini-2.5-pro"}}}
	for _, allowed := range []bool{true, false} {
		if !allowed {
			group.AllowedProtocols = nil
		}
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodGet, "/antigravity/v1beta/models", nil)
		c.Set(string(keyhttp.ContextKeyAPIKey), &apikey.APIKey{GroupID: &groupID, Group: group})
		c.Set(string(keyhttp.ContextKeyForcePlatform), "antigravity")
		handler.GeminiV1BetaListModels(c)
		if !allowed {
			require.Equal(t, 403, rec.Code)
			continue
		}
		require.Equal(t, 200, rec.Code)
		var got gemini.ModelsListResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		require.Len(t, got.Models, 1)
		require.Equal(t, "models/gemini-3-flash", got.Models[0].Name)
	}
}
