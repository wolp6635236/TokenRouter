package provider

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

// TestExecutionProviderPreservesNativeGraphAndIsolation copies a record whose
// group membership points back to that record. Root references follow the
// copied record, credential-map mutations stay isolated, and execution target
// JSON contains no record fields.
func TestExecutionProviderPreservesNativeGraphAndIsolation(t *testing.T) {
	source := &provider.Record{ID: 1, Credentials: map[string]any{"access_token": "original"}}
	source.ProviderGroups = []provider.GroupMembership{{ProviderID: 1, GroupID: 3, Provider: source}}
	target := NewExecutionProvider(source)
	require.Same(t, &target.Record, target.Record.ProviderGroups[0].Provider)
	target.Record.Credentials["access_token"] = "target"
	require.Equal(t, "original", source.GetCredential("access_token"))
	snapshot := ExecutionRecord(target)
	require.Same(t, snapshot, snapshot.ProviderGroups[0].Provider)
	snapshot.Credentials["access_token"] = "snapshot"
	require.Equal(t, "target", target.View().GetCredential("access_token"))
	payload, err := json.Marshal(target)
	require.NoError(t, err)
	require.JSONEq(t, `{}`, string(payload), "执行凭据与路线不能作为持久或公开载荷")
}

// TestExecutionProviderValueCopyAndBoundReader replaces credentials on an
// attempt value copied from another target. A previously bound reader observes
// ApplyExecutionRecord updates, and nil conversion entry points return nil.
func TestExecutionProviderValueCopyAndBoundReader(t *testing.T) {
	shared := NewExecutionProvider(&provider.Record{ID: 1, Credentials: map[string]any{"access_token": "shared"}})
	attempt := *shared
	attempt.Record.Credentials = map[string]any{"access_token": "attempt"}
	require.Equal(t, "shared", shared.View().GetCredential("access_token"))
	read := attempt.View().GetCredential
	ApplyExecutionRecord(&attempt, &provider.Record{ID: 1, Credentials: map[string]any{"access_token": "updated"}})
	require.Equal(t, "updated", read("access_token"))
	var absent *ExecutionProvider
	require.Nil(t, absent.View())
	require.Nil(t, ExecutionRecord(absent))
	require.Nil(t, ExecutionProviders(nil))
	require.Nil(t, ExecutionRecords(nil))
}

// TestFinalizeRequestHeadersSessionTrustBoundary varies target origin, provider
// type, and caller session text for a non-OpenCode provider. Only an API-key
// request to HTTPS opencode.ai receives the trimmed conversation ID, while
// lookalike, subdomain, insecure, OAuth, and empty cases add nothing.
func TestFinalizeRequestHeadersSessionTrustBoundary(t *testing.T) {
	for _, tt := range []struct {
		name      string // origin-policy scenario label
		typ       string // provider authentication type
		targetURL string // outgoing request target URL
		incoming  string // caller conversation-header value
		want      string // expected outgoing session value
	}{
		{"official origin", capability.ProviderTypeAPIKey, "https://opencode.ai/zen/v1/chat/completions", " conversation-123 ", "conversation-123"},
		{"lookalike origin", capability.ProviderTypeAPIKey, "https://opencode.ai.evil.example/v1/responses", "conversation-123", ""},
		{"subdomain is not implicitly trusted", capability.ProviderTypeAPIKey, "https://api.opencode.ai/v1/responses", "conversation-123", ""},
		{"insecure official origin", capability.ProviderTypeAPIKey, "http://opencode.ai/zen/v1/responses", "conversation-123", ""},
		{"missing caller value", capability.ProviderTypeAPIKey, "https://opencode.ai/zen/v1/responses", "", ""},
		{"oauth provider", capability.ProviderTypeOAuth, "https://opencode.ai/zen/v1/responses", "conversation-123", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			value := &ExecutionProvider{Record: provider.Record{Platform: capability.PlatformOpenAI, Type: tt.typ}}
			read := func(name string) string {
				if strings.EqualFold(name, "x-opencode-session") {
					return tt.incoming
				}
				return ""
			}
			headers := http.Header{"Authorization": {"Bearer existing"}}
			FinalizeRequestHeaders(value, tt.targetURL, read, headers)
			require.Equal(t, tt.want, headers.Get("X-OpenCode-Session"))
			require.Equal(t, "Bearer existing", headers.Get("Authorization"))
		})
	}
}

// TestFinalizeRequestHeadersOpenCodeOwnsAuthentication finalizes an OpenCode
// request whose builder wrote another protocol's key. The provider's bearer key
// replaces it and the client's session reaches the upstream.
func TestFinalizeRequestHeadersOpenCodeOwnsAuthentication(t *testing.T) {
	value := &ExecutionProvider{Record: provider.Record{Platform: capability.PlatformOpenCode, Type: capability.ProviderTypeAPIKey, Credentials: map[string]any{"api_key": "provider-key"}}}
	headers := http.Header{"X-Api-Key": {"builder-key"}}
	FinalizeRequestHeaders(value, "", func(name string) string {
		if name == "session-id" {
			return "session"
		}
		return ""
	}, headers)
	require.Empty(t, headers.Get("X-Api-Key"))
	require.Equal(t, "Bearer provider-key", headers.Get("Authorization"))
	require.Equal(t, "session", headers.Get("X-OpenCode-Session"))
}
