package capability

const (
	// ProviderTypeOAuth 等常量定义能力目录中的提供商类型。
	ProviderTypeOAuth          = "oauth"           // OAuth类型提供商（full scope: profile + inference）
	ProviderTypeSetupToken     = "setup-token"     // Setup Token类型提供商（inference only scope）
	ProviderTypeAPIKey         = "apikey"          // API Key类型提供商
	ProviderTypeUpstream       = "upstream"        // 上游透传类型提供商（通过 Base URL + API Key 连接上游）
	ProviderTypeBedrock        = "bedrock"         // AWS Bedrock 类型提供商（通过 SigV4 签名或 API Key 连接 Bedrock，由 credentials.auth_mode 区分）
	ProviderTypeServiceAccount = "service_account" // Google Service Account 类型提供商（用于 Vertex AI）
	ProviderTypeCosy           = "cosy"            // Qoder COSY 协议提供商

	OpenAIAuthModePersonalAccessToken = "personalAccessToken"
	OpenAIAuthModeAgentIdentity       = "agentIdentity"
)

// PolicyScopeMatches 根据提供商认证类型判断规则作用域。
func PolicyScopeMatches(scope string, isOAuth bool, isBedrock bool) bool {
	switch scope {
	case "all":
		return true
	case "oauth":
		return isOAuth
	case "apikey":
		return !isOAuth && !isBedrock
	case "bedrock":
		return isBedrock
	default:
		return true // 未知作用域允许匹配。
	}
}
