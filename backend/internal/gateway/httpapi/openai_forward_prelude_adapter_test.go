package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

func TestNormalizeOpenAICodexCompactReasoningEffortForProviderScopesCompatibility(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6-sol","input":"compact me","reasoning":{"effort":"max"}}`)

	tests := []struct {
		name     string
		path     string
		provider *gatewayprovider.ExecutionProvider
		changed  bool
		want     string
	}{
		{
			name:     "OpenAI OAuth compact 降级",
			path:     "/openai/v1/responses/compact",
			provider: &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth}},
			changed:  true,
			want:     "xhigh",
		},
		{
			name:     "OpenAI OAuth 普通请求保留",
			path:     "/openai/v1/responses",
			provider: &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth}},
			want:     "max",
		},
		{
			name:     "OpenAI API Key compact 保留",
			path:     "/openai/v1/responses/compact",
			provider: &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey}},
			want:     "max",
		},
		{
			name:     "Grok OAuth compact 保留",
			path:     "/openai/v1/responses/compact",
			provider: &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformGrok, Type: capability.ProviderTypeOAuth}},
			want:     "max",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, tt.path, nil)

			adapter := openAIForwardPreludeAdapter{c: c, provider: tt.provider}
			normalized, changed, err := adapter.CompactEffort(body)

			require.NoError(t, err)
			require.Equal(t, tt.changed, changed)
			require.Equal(t, tt.want, gjson.GetBytes(normalized, "reasoning.effort").String())
		})
	}
}
