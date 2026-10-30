package opencode

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestApplyHeadersRetainsClientIdentity replaces conflicting credentials while
// carrying the actual client agent and stable native session. Missing caller
// sessions stay absent rather than becoming random request identities.
func TestApplyHeadersRetainsClientIdentity(t *testing.T) {
	headers := http.Header{"authorization": []string{"wrong"}, "x-api-key": []string{"wrong"}, "x-goog-api-key": []string{"wrong"}}
	incoming := http.Header{"User-Agent": []string{"my-agent/2"}, "Session-Id": []string{"conversation"}}
	ApplyHeaders(headers, "secret", incoming.Get)
	require.Equal(t, "Bearer secret", headers.Get("Authorization"))
	require.Equal(t, "my-agent/2", headers.Get("User-Agent"))
	require.Equal(t, "conversation", headers.Get("X-OpenCode-Session"))
	require.Empty(t, headers.Get("x-api-key"))
	require.Empty(t, headers.Get("x-goog-api-key"))
	empty := make(http.Header)
	ApplyHeaders(empty, "secret", nil)
	require.Empty(t, empty.Get("X-OpenCode-Session"))
	require.Equal(t, "TokenRouter", empty.Get("User-Agent"))
}

// TestApplyHeadersFinalizesOneNormalizedIdentity starts with differently cased
// credentials and agent overrides. The final pass must emit one trimmed bearer
// credential and one caller agent while retaining a configured session when absent.
func TestApplyHeadersFinalizesOneNormalizedIdentity(t *testing.T) {
	headers := http.Header{"authorization": {"old"}, "X-Api-Key": {"old"}, "x-goog-api-key": {"old"}, "user-agent": {"configured"}, "x-opencode-session": {"saved-session"}}
	ApplyHeaders(headers, "  fixture-key\n", func(name string) string {
		if name == "User-Agent" {
			return "caller/2"
		}
		return ""
	})
	require.Equal(t, "Bearer fixture-key", headers.Get("Authorization"))
	require.Equal(t, "caller/2", headers.Get("User-Agent"))
	require.Subset(t, headers, http.Header{"x-opencode-session": []string{"saved-session"}})
	for _, wanted := range []string{"authorization", "user-agent"} {
		count := 0
		for name := range headers {
			if strings.EqualFold(name, wanted) {
				count++
			}
		}
		require.Equal(t, 1, count)
	}
	fallback := http.Header{"user-agent": {"configured"}}
	ApplyHeaders(fallback, "key", nil)
	require.Equal(t, "configured", fallback.Get("User-Agent"))
}
