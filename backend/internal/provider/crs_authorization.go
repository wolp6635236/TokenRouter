package provider

import (
	"context"
	"maps"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

// CRSAuthorization 按平台补全导入凭据。
type CRSAuthorization struct {
	Claude *ClaudeAuthorization
	OpenAI *OpenAIAuthorization
	Gemini *GeminiAuthorization
}

// Coordinated 复用导入路径的原锁键与条件写入，不改变导入的状态资格。
func (s *CRSAuthorization) Coordinated(coordinator *OAuthRefreshAPI, geminiKey func(*Record) string) func(context.Context, *Record) error {
	return func(ctx context.Context, record *Record) error {
		var key string
		switch record.Platform {
		case capability.PlatformAnthropic:
			if s.Claude == nil {
				return nil
			}
			key = ClaudeTokenCacheKey(record)
		case capability.PlatformOpenAI:
			if s.OpenAI == nil {
				return nil
			}
			key = OpenAITokenCacheKey(record)
		case capability.PlatformGemini:
			if s.Gemini == nil {
				return nil
			}
			key = geminiKey(record)
		default:
			return nil
		}
		return coordinator.RefreshImported(ctx, record, key, s.Refresh)
	}
}

// Refresh 保留导入的尽力交换，失败不改变已落库的同步结果。
func (s *CRSAuthorization) Refresh(ctx context.Context, provider *Record) map[string]any {
	if provider.Type != capability.ProviderTypeOAuth {
		return nil
	}

	var newCredentials map[string]any
	var err error

	switch provider.Platform {
	case capability.PlatformAnthropic:
		if s.Claude == nil {
			return nil
		}
		tokenInfo, refreshErr := s.Claude.RefreshProviderToken(ctx, provider)
		if refreshErr != nil {
			err = refreshErr
		} else {
			// 保留现有非令牌配置。
			newCredentials = make(map[string]any)
			maps.Copy(newCredentials, provider.Credentials)
			// 覆盖本次返回的令牌字段。
			newCredentials["access_token"] = tokenInfo.AccessToken
			newCredentials["token_type"] = tokenInfo.TokenType
			newCredentials["expires_in"] = tokenInfo.ExpiresIn
			newCredentials["expires_at"] = tokenInfo.ExpiresAt
			if tokenInfo.RefreshToken != "" {
				newCredentials["refresh_token"] = tokenInfo.RefreshToken
			}
			if tokenInfo.Scope != "" {
				newCredentials["scope"] = tokenInfo.Scope
			}
		}
	case capability.PlatformOpenAI:
		if s.OpenAI == nil {
			return nil
		}
		tokenInfo, refreshErr := s.OpenAI.RefreshProviderToken(ctx, provider)
		if refreshErr != nil {
			err = refreshErr
		} else {
			newCredentials = BuildOpenAIProviderCredentials(tokenInfo)
			// 保留响应未覆盖的非令牌配置。
			for k, v := range provider.Credentials {
				if _, exists := newCredentials[k]; !exists {
					newCredentials[k] = v
				}
			}
		}
	case capability.PlatformGemini:
		if s.Gemini == nil {
			return nil
		}
		tokenInfo, refreshErr := s.Gemini.RefreshProviderToken(ctx, provider)
		if refreshErr != nil {
			err = refreshErr
		} else {
			newCredentials = s.Gemini.BuildProviderCredentials(tokenInfo)
			for k, v := range provider.Credentials {
				if _, exists := newCredentials[k]; !exists {
					newCredentials[k] = v
				}
			}
		}
	default:
		return nil
	}

	if err != nil {
		// 刷新失败不改变已完成的同步结果。
		return nil
	}

	return newCredentials
}
