package opencode

import (
	"net/http"
	"strings"
)

// headers.go installs OpenCode authentication and conversation identity on
// prepared inference requests. All five protocol families use the same bearer
// key, while the caller supplies its own agent identity and stable session
// header. Provider overrides are applied before this final authentication pass.
//
// Conversation values are selected from recognized client headers and copied
// without generating per-request identifiers. Missing session evidence leaves
// the header absent. Callers restrict ApplyHeaders to selected OpenCode
// providers, after validating their configured destination, so another provider
// cannot receive the key just because it accepts a compatible protocol; the
// session helpers also serve API-key relays addressed to the official host.

// ApplyHeaders replaces authentication and copies available client identity.
// The header map must be writable; read may be nil for administrator probes.
// Missing client User-Agent retains a configured value or uses TokenRouter.
//
// The selected provider owns authentication, so legacy protocol keys are removed
// before its bearer token is installed. A supplied conversation ID replaces all
// casing variants of the session header, including a provider-wide override.
// Missing session evidence retains an existing configured header and never
// creates a random request-scoped identity that would break conversation reuse.
func ApplyHeaders(headers http.Header, key string, read func(string) string) {
	for name := range headers {
		if strings.EqualFold(name, "authorization") || strings.EqualFold(name, "x-api-key") || strings.EqualFold(name, "x-goog-api-key") {
			delete(headers, name)
		}
	}
	headers.Set("Authorization", "Bearer "+strings.TrimSpace(key))
	agent := ""
	if read != nil {
		agent = strings.TrimSpace(read("User-Agent"))
	}
	if agent == "" {
		agent = headers.Get("User-Agent")
	}
	// Remove every wire spelling before publishing one final agent identity.
	// A caller agent wins over configured values; absent caller identity retains
	// the configured value even when its map key used noncanonical casing.
	for name, values := range headers {
		if strings.EqualFold(name, "user-agent") {
			if agent == "" && len(values) > 0 {
				agent = strings.TrimSpace(values[0])
			}
			delete(headers, name)
		}
	}
	if agent == "" {
		agent = "TokenRouter"
	}
	headers.Set("User-Agent", agent)
	SetSession(headers, SessionID(read))
}

// SessionID returns the first nonblank conversation ID among the recognized
// client headers, or empty when read is nil or no header carries one.
func SessionID(read func(string) string) string {
	if read == nil {
		return ""
	}
	for _, name := range []string{"x-opencode-session", "session-id", "session_id", "conversation_id", "x-session-affinity", "x-session-id"} {
		if session := strings.TrimSpace(read(name)); session != "" {
			return session
		}
	}
	return ""
}

// SetSession publishes session as the single X-OpenCode-Session value. An
// empty session leaves the map unchanged, so a configured header survives
// requests that carry no conversation evidence.
func SetSession(headers http.Header, session string) {
	if session == "" {
		return
	}
	// Header overrides may use noncanonical casing. Replace every spelling so
	// a fixed provider-wide value cannot override this conversation's identity.
	for name := range headers {
		if strings.EqualFold(name, "x-opencode-session") {
			delete(headers, name)
		}
	}
	headers.Set("X-OpenCode-Session", session)
}
