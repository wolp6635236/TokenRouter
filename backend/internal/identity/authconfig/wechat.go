package authconfig

import "strings"

// wechat.go normalizes WeChat channel selection and authorization scopes for
// startup configuration and runtime settings. Both consumers accept the same
// mode spellings and channel-specific scopes, while each consumer retains its
// own rules for loading capabilities and credentials. The pure rules here take
// those resolved capabilities as input and do not establish login readiness.
//
// Stored mode selection preserves an enabled request, then considers Open, MP,
// and Mobile in order. An entirely disabled capability set retains the normalized
// request for readback. Scope selection follows the resulting mode, so an MP
// consent scope cannot survive selection of a different channel.

const (
	// DefaultWeChatConnectMode and DefaultWeChatConnectScopes supply the Open
	// channel defaults used by startup loading and runtime settings readback.
	DefaultWeChatConnectMode   = "open"
	DefaultWeChatConnectScopes = "snsapi_login"
)

// NormalizeWeChatConnectStoredMode selects a display mode consistent with the
// available WeChat mode flags. The normalized requested mode wins when its
// corresponding flag is enabled.
//
// If that mode is disabled, available modes are tried in Open, MP, then Mobile
// order. With no enabled mode the normalized request is returned, allowing
// later validation and feature availability to handle the inactive
// configuration. This keeps mode selection separate from the overall Enabled
// value used by its caller.
func NormalizeWeChatConnectStoredMode(openEnabled, mpEnabled, mobileEnabled bool, mode string) string {
	mode = NormalizeWeChatConnectMode(mode)
	switch mode {
	case "open":
		if openEnabled {
			return "open"
		}
	case "mp":
		if mpEnabled {
			return "mp"
		}
	case "mobile":
		if mobileEnabled {
			return "mobile"
		}
	}
	// The requested mode has no enabled path. Select an available login path in
	// priority order so the stored display mode follows usable mode flags.
	switch {
	case openEnabled:
		return "open"
	case mpEnabled:
		return "mp"
	case mobileEnabled:
		return "mobile"
	default:
		return mode
	}
}

// NormalizeWeChatConnectScopes chooses compatible scope text for the normalized
// mode. MP accepts trimmed exact snsapi_base or snsapi_userinfo and defaults
// otherwise; Mobile ignores supplied scope and returns empty, while Open always
// selects its login scope.
//
// Callers supply the channel that will consume the scope and separately check
// capability flags and credentials before authorizing. Scope normalization also
// accepts disabled configuration, allowing settings readback to apply the same
// channel rules as startup loading without enabling a login flow.
func NormalizeWeChatConnectScopes(raw, mode string) string {
	switch NormalizeWeChatConnectMode(mode) {
	case "mp":
		switch strings.TrimSpace(raw) {
		case "snsapi_base":
			return "snsapi_base"
		case "snsapi_userinfo":
			return "snsapi_userinfo"
		default:
			// MP accepts the two recognized consent scopes exactly. Replace unsupported
			// scope text with the mode default before this value reaches login settings.
			return DefaultWeChatConnectScopesForMode(mode)
		}
	case "mobile":
		return ""
	default:
		return DefaultWeChatConnectScopes
	}
}

// DefaultWeChatConnectScopesForMode returns the built-in scope text for the
// normalized login mode. MP uses snsapi_userinfo, Mobile uses empty text, and
// Open or unknown input uses snsapi_login.
func DefaultWeChatConnectScopesForMode(mode string) string {
	switch NormalizeWeChatConnectMode(mode) {
	case "mp":
		return "snsapi_userinfo"
	case "mobile":
		return ""
	default:
		return DefaultWeChatConnectScopes
	}
}

// NormalizeWeChatConnectMode returns mp or mobile for trimmed case-insensitive
// matches, otherwise the default open mode. Unknown and empty input share that
// default before stored-mode availability is considered.
func NormalizeWeChatConnectMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "mp":
		return "mp"
	case "mobile":
		return "mobile"
	default:
		return DefaultWeChatConnectMode
	}
}
