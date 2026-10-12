package provider

import (
	"strings"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

const (
	anthropicAPIKeyAuthSchemeExtraKey            = "anthropic_apikey_auth_scheme"
	AnthropicAPIKeyAuthSchemeXAPIKey             = "x_api_key"
	AnthropicAPIKeyAuthSchemeAuthorizationBearer = "authorization_bearer"
)

// GetAnthropicAPIKeyAuthScheme 返回 Anthropic API Key 提供商转发上游时使用的认证头方案。
func (r *Record) GetAnthropicAPIKeyAuthScheme() string {
	if r == nil || r.Type != capability.ProviderTypeAPIKey {
		return AnthropicAPIKeyAuthSchemeXAPIKey
	}
	if r.Platform != capability.PlatformAnthropic && !r.IsCNProvider() {
		return AnthropicAPIKeyAuthSchemeXAPIKey
	}

	switch strings.TrimSpace(r.GetExtraString(anthropicAPIKeyAuthSchemeExtraKey)) {
	case AnthropicAPIKeyAuthSchemeAuthorizationBearer:
		return AnthropicAPIKeyAuthSchemeAuthorizationBearer
	default:
		return AnthropicAPIKeyAuthSchemeXAPIKey
	}
}
