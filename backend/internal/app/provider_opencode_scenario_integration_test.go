//go:build integration

package app

import (
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/TokenFlux/TokenRouter/internal/app/lifecycle"
	"github.com/TokenFlux/TokenRouter/internal/config"
	"github.com/TokenFlux/TokenRouter/internal/modelcatalog"
	"github.com/TokenFlux/TokenRouter/internal/modelcatalog/testkit"
	"github.com/TokenFlux/TokenRouter/internal/protocol"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	providerhttp "github.com/TokenFlux/TokenRouter/internal/provider/httpapi"
	providerpostgres "github.com/TokenFlux/TokenRouter/internal/provider/postgres"
	"github.com/TokenFlux/TokenRouter/internal/routing"
)

// provider_opencode_scenario_integration_test.go verifies provider persistence,
// snapshot.go route resolution, and provider_testing.go probe assembly together.
// PostgreSQL reloads accepted catalog routes and manual overrides before real
// protocol targets send requests through a recording transport. Modes and
// one-hop aliases must survive that stored configuration boundary.

// TestOpenCodePersistedProbeAssembly reloads OpenCode credentials before probing
// all five protocols. Accepted catalog routes, mode defaults, and manual overrides
// must agree with the outbound model, authentication, and successful probe events.
func TestOpenCodePersistedProbeAssembly(t *testing.T) {
	f := newDatabaseFixture(t)
	store := providerpostgres.NewProviderStore(f.client, f.db, providerpostgres.ProviderStoreOptions{})
	transport := &nativeProviderTestTransport{}
	manager := lifecycle.New()
	t.Cleanup(func() { require.NoError(t, manager.Stop(context.Background())) })
	outputs := []string{"text"}
	profiles := &probeAttributeRepository{config: &routing.ModelAttributeConfig{
		Status: "active", Rules: []routing.ModelAttributeRule{{
			Models: []string{"*"}, Attributes: modelcatalog.Attributes{OutputModalities: &outputs},
		}},
	}}
	attributes := &routing.ModelAttributeService{Repo: profiles, Catalog: routing.ModelAttributeCatalog{View: testkit.OfflineView}}
	core := provideProviderTests(
		store, nil, nil, nil, nil, transport, &config.Config{},
		nil, nil, nil, nil, manager, nil, attributes, nil,
	)
	for _, tc := range []struct {
		name     string              // persisted configuration case name
		mode     string              // selected OpenCode service mode
		model    string              // final mapped upstream model
		protocol protocol.ProtocolID // expected selected protocol identity
		catalog  string              // optional accepted catalog encoding
		override string              // optional manual encoding override
		path     string              // expected upstream request path
		body     string              // supplied successful protocol response
	}{
		{
			"accepted responses", "go", "accepted-model",
			protocol.ProtocolOpenAIResponses, "openai_responses", "",
			"/inference/go/openai/v1/responses",
			"data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\ndata: {\"type\":\"response.completed\"}\n\n",
		},
		{
			"accepted chat", "go", "accepted-model",
			protocol.ProtocolOpenAIChatCompletions, "openai_chat_completions", "",
			"/inference/go/openai/v1/chat/completions",
			"data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n",
		},
		{
			"accepted messages", "go", "accepted-model",
			protocol.ProtocolAnthropicMessages, "anthropic_messages", "",
			"/inference/go/anthropic/v1/messages",
			"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\ndata: {\"type\":\"message_stop\"}\n\n",
		},
		{
			"accepted gemini", "go", "accepted-model",
			protocol.ProtocolGeminiGenerateContent, "gemini_generate_content", "",
			"/inference/go/google/v1beta/models/accepted-model:streamGenerateContent",
			"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]}}]}\n\ndata: [DONE]\n\n",
		},
		{
			"accepted decision", "go", "accepted-model",
			protocol.ProtocolSystemOne, "systemone", "",
			"/inference/go/systemone/v1/systemone",
			`{"model":"accepted-model","answers":{"available":{"type":"noul","noul":1}},"usage":{"input_tokens":2,"output_tokens":1}}`,
		},
		{
			"payg catalog default", "payg", "minimax-m3",
			protocol.ProtocolOpenAIChatCompletions, "", "",
			"/inference/openai/v1/chat/completions",
			"data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n",
		},
		{
			"go catalog default", "go", "minimax-m3",
			protocol.ProtocolAnthropicMessages, "", "",
			"/inference/go/anthropic/v1/messages",
			"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\ndata: {\"type\":\"message_stop\"}\n\n",
		},
		{
			"manual overrides accepted", "go", "accepted-model",
			protocol.ProtocolOpenAIResponses, "openai_chat_completions", "openai_responses",
			"/inference/go/openai/v1/responses",
			"data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\ndata: {\"type\":\"response.completed\"}\n\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			credentials := map[string]any{
				"api_key": "  persisted-secret  ", "provider_mode": tc.mode,
				"upstream_protocols": []string{string(tc.protocol)},
				"model_mapping":      map[string]any{"alias-*": tc.model, tc.model: "wrong-second-hop"},
			}
			if tc.catalog != "" {
				credentials[provider.OpenCodeCatalogProtocolsKey] = map[string]any{tc.mode: map[string]any{tc.model: tc.catalog}}
			}
			if tc.override != "" {
				credentials[provider.OpenCodeModelProtocolsKey] = map[string]any{tc.mode: map[string]any{tc.model: tc.override}}
			}
			// Save before loading both routing and probe inputs so no in-memory
			// credential map can substitute for the persisted provider configuration under test.
			row, err := f.client.Provider.Create().
				SetName(tc.name).
				SetPlatform(provider.PlatformOpenCode).
				SetType(provider.ProviderTypeAPIKey).
				SetCredentials(credentials).
				Save(t.Context())
			require.NoError(t, err)
			loaded, err := store.GetByID(t.Context(), row.ID)
			require.NoError(t, err)
			require.NoError(t, provider.ValidateOpenCodeCredentials(loaded))
			require.Equal(t, tc.mode, loaded.OpenCodeMode())
			snapshot := loaded.RoutingSnapshot()
			route, ok := snapshot.ResolveModelRoute("alias-fast", "", nil)
			require.True(t, ok)
			require.Equal(t, tc.model, route.Model)
			require.Equal(t, tc.protocol, route.Protocol)
			require.True(t, route.MappingMatched)
			if tc.catalog != "" {
				require.Equal(t, credentials[provider.OpenCodeCatalogProtocolsKey], loaded.Credentials[provider.OpenCodeCatalogProtocolsKey])
			}
			for _, automatic := range []bool{false, true} {
				transport.requests = nil
				transport.ids = nil
				transport.body = tc.body
				request := provider.TestRequest{
					ProviderID: row.ID, GroupID: 59, Model: "alias-fast",
					Prompt: "hello", UserAgent: "OpenCodeProbeAcceptance/1",
				}
				if automatic {
					result, err := core.RunTestBackgroundRequest(t.Context(), request)
					require.NoError(t, err)
					require.Equal(t, "success", result.Status, result.ErrorMessage)
				} else {
					recorder := httptest.NewRecorder()
					require.NoError(t, core.Test(t.Context(), request, providerhttp.NewTestEventSink(recorder)), recorder.Body.String())
					require.Contains(t, recorder.Body.String(), `"type":"test_start"`)
					completed := false
					for line := range strings.SplitSeq(recorder.Body.String(), "\n") {
						event := strings.TrimPrefix(line, "data: ")
						if gjson.Get(event, "type").String() == "test_complete" {
							completed = true
							require.True(t, gjson.Get(event, "success").Bool(), event)
						}
					}
					require.True(t, completed, recorder.Body.String())
				}
				require.Len(t, transport.requests, 1)
				require.Equal(t, []int64{row.ID}, transport.ids)
				sent := transport.requests[0]
				require.Equal(t, "opencode.ai", sent.URL.Host)
				require.Equal(t, tc.path, sent.URL.Path)
				require.Equal(t, "Bearer persisted-secret", sent.Header.Get("Authorization"))
				require.Equal(t, "OpenCodeProbeAcceptance/1", sent.Header.Get("User-Agent"))
				require.Empty(t, sent.Header.Get("X-Api-Key"))
				require.Empty(t, sent.Header.Get("X-Goog-Api-Key"))
				body, err := io.ReadAll(sent.Body)
				require.NoError(t, err)
				require.NotContains(t, string(body)+sent.URL.Path, "wrong-second-hop")
				if tc.protocol != protocol.ProtocolGeminiGenerateContent {
					require.Equal(t, tc.model, gjson.GetBytes(body, "model").String())
				}
			}
		})
	}
}
