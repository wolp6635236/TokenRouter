package codeassist

import (
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"strings"

	infraerrors "github.com/TokenFlux/TokenRouter/internal/pkg/apperror"
	"github.com/TokenFlux/TokenRouter/internal/pkg/oauthpkce"
	"github.com/TokenFlux/TokenRouter/internal/protocol/google"
)

const (
	AIStudioBaseURL  = "https://generativelanguage.googleapis.com"
	GeminiCliBaseURL = "https://cloudcode-pa.googleapis.com"

	AuthorizeURL = "https://accounts.google.com/o/oauth2/v2/auth"
	TokenURL     = "https://oauth2.googleapis.com/token"

	// AIStudioOAuthRedirectURI 是 AI Studio OAuth 默认回调地址，供用户复制回调 URL。
	// Google OAuth 客户端需要登记此地址，支持 localhost 回调的客户端类型除外。
	AIStudioOAuthRedirectURI = "http://localhost:1455/auth/callback"

	// DefaultCodeAssistScopes 包含 Code Assist API 访问和用户信息权限。
	DefaultCodeAssistScopes = "https://www.googleapis.com/auth/cloud-platform https://www.googleapis.com/auth/userinfo.email https://www.googleapis.com/auth/userinfo.profile"

	// DefaultAIStudioScopes 配置普通 Google 账户访问 generativelanguage.googleapis.com 的 OAuth 权限。
	// 权限包含 cloud-platform 和 generative-language.retriever。
	// 参考：https://ai.google.dev/gemini-api/docs/oauth
	DefaultAIStudioScopes = "https://www.googleapis.com/auth/cloud-platform https://www.googleapis.com/auth/generative-language.retriever"

	// GeminiCLIRedirectURI 是 Gemini CLI 的 Code Assist OAuth 回调地址。
	GeminiCLIRedirectURI = "https://codeassist.google.com/authcode"

	// GeminiCLIOAuthClientID 是 Gemini CLI 使用的公开 OAuth 客户端 ID。
	// 内置凭据支持直接登录，可用权限由 Google 限制。
	GeminiCLIOAuthClientID = "681255809395-oo8ft2oprdrnp9e3aqf6av3hmdib135j.apps.googleusercontent.com"
	// GeminiCLIOAuthClientSecret 是 Gemini CLI 使用的公开 OAuth 客户端密钥。
	GeminiCLIOAuthClientSecret = "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"

	// GeminiCLIOAuthClientSecretEnv 是覆盖内置客户端密钥的环境变量名。
	GeminiCLIOAuthClientSecretEnv = "GEMINI_CLI_OAUTH_CLIENT_SECRET"

	// GeminiCLIUserAgent 是请求内部接口时使用的 Gemini CLI User-Agent。
	GeminiCLIUserAgent = "GeminiCLI/0.1.5 (Windows; AMD64)"
)

type OAuthConfig = google.OAuthConfig

func GenerateRandomBytes(n int) ([]byte, error) {
	return oauthpkce.RandomBytes(n)
}

func GenerateState() (string, error) {
	bytes, err := GenerateRandomBytes(32)
	if err != nil {
		return "", err
	}
	return base64URLEncode(bytes), nil
}

func GenerateSessionID() (string, error) {
	bytes, err := GenerateRandomBytes(16)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// GenerateCodeVerifier returns an RFC 7636 compatible code verifier (43+ chars).
func GenerateCodeVerifier() (string, error) {
	return oauthpkce.Verifier()
}

func GenerateCodeChallenge(verifier string) string {
	return oauthpkce.Challenge(verifier)
}

func base64URLEncode(data []byte) string {
	return oauthpkce.Base64URL(data)
}

// EffectiveOAuthConfig returns the effective OAuth configuration.
// oauthType: "code_assist" or "ai_studio" (defaults to "code_assist" if empty).
//
// If ClientID/ClientSecret is not provided, this falls back to the built-in Gemini CLI OAuth client.
//
// Note: The built-in Gemini CLI OAuth client is restricted and may reject some scopes (e.g.
// https://www.googleapis.com/auth/generative-language), which will surface as
// "restricted_client" / "Unregistered scope(s)" errors during browser authorization.
func EffectiveOAuthConfig(cfg OAuthConfig, oauthType string) (OAuthConfig, error) {
	effective := OAuthConfig{
		ClientID:     strings.TrimSpace(cfg.ClientID),
		ClientSecret: strings.TrimSpace(cfg.ClientSecret),
		Scopes:       strings.TrimSpace(cfg.Scopes),
	}

	// Normalize scopes: allow comma-separated input but send space-delimited scopes to Google.
	if effective.Scopes != "" {
		effective.Scopes = strings.Join(strings.Fields(strings.ReplaceAll(effective.Scopes, ",", " ")), " ")
	}

	// Fall back to built-in Gemini CLI OAuth client when not configured.
	// SECURITY: This repo does not embed the built-in client secret; it must be provided via env.
	if effective.ClientID == "" && effective.ClientSecret == "" {
		secret := strings.TrimSpace(GeminiCLIOAuthClientSecret)
		if secret == "" {
			if v, ok := os.LookupEnv(GeminiCLIOAuthClientSecretEnv); ok {
				secret = strings.TrimSpace(v)
			}
		}
		if secret == "" {
			return OAuthConfig{}, infraerrors.Newf(infraerrors.CategoryBadRequest, "GEMINI_CLI_OAUTH_CLIENT_SECRET_MISSING", "built-in Gemini CLI OAuth client_secret is not configured; set %s or provide a custom OAuth client", GeminiCLIOAuthClientSecretEnv)
		}
		effective.ClientID = GeminiCLIOAuthClientID
		effective.ClientSecret = secret
	} else if effective.ClientID == "" || effective.ClientSecret == "" {
		return OAuthConfig{}, infraerrors.New(infraerrors.CategoryBadRequest, "GEMINI_OAUTH_CLIENT_NOT_CONFIGURED", "OAuth client not configured: please set both client_id and client_secret (or leave both empty to use the built-in Gemini CLI client)")
	}

	isBuiltinClient := effective.ClientID == GeminiCLIOAuthClientID

	if effective.Scopes == "" {
		// Use different default scopes based on OAuth type
		switch oauthType {
		case "ai_studio":
			// Built-in client can't request some AI Studio scopes (notably generative-language).
			if isBuiltinClient {
				effective.Scopes = DefaultCodeAssistScopes
			} else {
				effective.Scopes = DefaultAIStudioScopes
			}
		case "google_one":
			// Google One always uses built-in Gemini CLI client (same as code_assist)
			// Built-in client can't request restricted scopes like generative-language.retriever or drive.readonly
			effective.Scopes = DefaultCodeAssistScopes
		default:
			// Default to Code Assist scopes
			effective.Scopes = DefaultCodeAssistScopes
		}
	} else if (oauthType == "ai_studio" || oauthType == "google_one") && isBuiltinClient {
		// If user overrides scopes while still using the built-in client, strip restricted scopes.
		parts := strings.Fields(effective.Scopes)
		filtered := make([]string, 0, len(parts))
		for _, s := range parts {
			if hasRestrictedScope(s) {
				continue
			}
			filtered = append(filtered, s)
		}
		if len(filtered) == 0 {
			effective.Scopes = DefaultCodeAssistScopes
		} else {
			effective.Scopes = strings.Join(filtered, " ")
		}
	}

	// Backward compatibility: normalize older AI Studio scope to the currently documented one.
	if oauthType == "ai_studio" && effective.Scopes != "" {
		parts := strings.Fields(effective.Scopes)
		for i := range parts {
			if parts[i] == "https://www.googleapis.com/auth/generative-language" {
				parts[i] = "https://www.googleapis.com/auth/generative-language.retriever"
			}
		}
		effective.Scopes = strings.Join(parts, " ")
	}

	return effective, nil
}

func hasRestrictedScope(scope string) bool {
	return strings.HasPrefix(scope, "https://www.googleapis.com/auth/generative-language") ||
		strings.HasPrefix(scope, "https://www.googleapis.com/auth/drive")
}

func BuildAuthorizationURL(cfg OAuthConfig, state, codeChallenge, redirectURI, projectID, oauthType string) (string, error) {
	effectiveCfg, err := EffectiveOAuthConfig(cfg, oauthType)
	if err != nil {
		return "", err
	}
	redirectURI = strings.TrimSpace(redirectURI)
	if redirectURI == "" {
		return "", fmt.Errorf("redirect_uri is required")
	}

	params := url.Values{}
	params.Set("response_type", "code")
	params.Set("client_id", effectiveCfg.ClientID)
	params.Set("redirect_uri", redirectURI)
	params.Set("scope", effectiveCfg.Scopes)
	params.Set("state", state)
	params.Set("code_challenge", codeChallenge)
	params.Set("code_challenge_method", "S256")
	params.Set("access_type", "offline")
	params.Set("prompt", "consent")
	params.Set("include_granted_scopes", "true")
	if strings.TrimSpace(projectID) != "" {
		params.Set("project_id", strings.TrimSpace(projectID))
	}

	return fmt.Sprintf("%s?%s", AuthorizeURL, params.Encode()), nil
}
