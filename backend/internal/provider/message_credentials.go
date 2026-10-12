package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

// MessageCredentialSource 绑定共享的 Claude 和 Vertex 凭据源。
type MessageCredentialSource struct {
	Claude func(context.Context, *Record) (string, error)
}

// Resolve 在原调用时点选择凭据来源，保留 Messages 与其他入口的资格差异。
// @project-doc docs/interfaces/anthropic_upstream.md#anthropic_account_and_transport
func (s *MessageCredentialSource) Resolve(ctx context.Context, provider *Record) (string, string, error) {
	switch provider.Type {
	case capability.ProviderTypeOAuth, capability.ProviderTypeSetupToken:
		// 分别按 OAuth 与 setup-token 的认证规则读取凭据。
		return s.oauth(ctx, provider)
	case capability.ProviderTypeAPIKey:
		apiKey := provider.GetCredential("api_key")
		if apiKey == "" {
			return "", "", errors.New("api_key not found in credentials")
		}
		return apiKey, "apikey", nil
	case capability.ProviderTypeBedrock:
		return "", "bedrock", nil // Bedrock 使用 SigV4 签名或 API Key，由 forwardBedrock 处理
	case capability.ProviderTypeServiceAccount:
		if provider.Platform != capability.PlatformAnthropic {
			return "", "", fmt.Errorf("unsupported service account platform: %s", provider.Platform)
		}
		if s == nil || s.Claude == nil {
			return "", "", errors.New("claude token provider not configured")
		}
		accessToken, err := s.Claude(ctx, provider)
		if err != nil {
			return "", "", err
		}
		return accessToken, "service_account", nil
	default:
		return "", "", fmt.Errorf("unsupported provider type: %s", provider.Type)
	}
}

func (s *MessageCredentialSource) oauth(ctx context.Context, provider *Record) (string, string, error) {
	// 对于 Anthropic OAuth 提供商，使用 ClaudeTokenProvider 获取缓存的 token
	if provider.Platform == capability.PlatformAnthropic && provider.Type == capability.ProviderTypeOAuth && s != nil && s.Claude != nil {
		accessToken, err := s.Claude(ctx, provider)
		if err != nil {
			return "", "", err
		}
		return accessToken, "oauth", nil
	}

	// Grok OAuth 优先使用凭据中的 access_token，后台刷新器负责保持其有效。
	if provider.Platform == capability.PlatformGrok && provider.Type == capability.ProviderTypeOAuth {
		accessToken, err := GrokStoredAccessToken(provider)
		if err != nil {
			return "", "", err
		}
		return accessToken, "oauth", nil
	}

	// 其他情况（Gemini 有自己的 TokenProvider，setup-token 类型等）直接从提供商读取
	accessToken := provider.GetCredential("access_token")
	if accessToken == "" {
		return "", "", errors.New("access_token not found in credentials")
	}
	// Token 刷新由后台 TokenRefreshService 处理，此处只返回当前 token
	return accessToken, "oauth", nil
}
