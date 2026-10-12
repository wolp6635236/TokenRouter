package provider

import (
	"context"
	"maps"
	"strings"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

// UnauthorizedObservation 只含经供应商 Adapter 识别的认证失败信号。
type UnauthorizedObservation struct {
	Message                 string
	TokenRevoked            bool
	PermanentlyUnauthorized bool
}

// ApplyUnauthorized 保留凭据母提供商解析、令牌失效与刷新冷却的原次序。
func (s *HealthService) ApplyUnauthorized(ctx context.Context, provider *Record, observation UnauthorizedObservation) bool {
	// Spark 影子共用母提供商凭据。401 的缓存失效、refresh_token 检查、禁用和冷却均作用于母提供商，
	// 避免因影子没有 refresh_token 而误将其永久禁用。母提供商进入冷却后，调度健康检查会排除其影子。
	// 非影子直接使用自身记录；母提供商查找失败或不存在时，回退到当前记录。
	authProvider := provider
	if resolved, rerr := ResolveCredentialRecord(ctx, func(ctx context.Context, id int64) (*Record, error) { return s.providerRepo.GetByID(ctx, id) }, provider); rerr == nil && resolved != nil {
		authProvider = resolved
	}
	// OpenAI: token_invalidated / token_revoked 表示 token 被永久作废（非过期），直接标记 error
	if authProvider.Platform == capability.PlatformOpenAI && observation.TokenRevoked {
		msg := "Token revoked (401): provider authentication permanently revoked"
		if observation.Message != "" {
			msg = "Token revoked (401): " + observation.Message
		}
		s.ApplyAuthenticationFailure(ctx, authProvider, msg)
		return true
	}
	// OpenAI: {"detail":"Unauthorized"} 表示 token 完全无效（非标准 OpenAI 错误格式），直接标记 error
	if authProvider.Platform == capability.PlatformOpenAI && observation.PermanentlyUnauthorized {
		msg := "Unauthorized (401): provider authentication failed permanently"
		if observation.Message != "" {
			msg = "Unauthorized (401): " + observation.Message
		}
		s.ApplyAuthenticationFailure(ctx, authProvider, msg)
		return true
	}
	// OAuth 提供商收到 401 后临时停调，等待 token 刷新，其他类型调用 SetError。
	if authProvider.Type == capability.ProviderTypeOAuth {
		// 1. 失效缓存
		if s.options.InvalidateUnauthorizedToken != nil {
			if err := s.options.InvalidateUnauthorizedToken(ctx, authProvider); err != nil {
				s.options.Warn("oauth_401_invalidate_cache_failed", "provider_id", authProvider.ID, "error", err)
			}
		}
		// 缺少 refresh_token 的 OAuth 提供商无法在冷却期内自愈（后台刷新服务也会跳过），
		// 调用 SetError 禁用该提供商，后续调度将其排除。
		if strings.TrimSpace(authProvider.GetCredential("refresh_token")) == "" {
			msg := "Authentication failed (401): refresh_token missing, cannot recover"
			if observation.Message != "" {
				msg = "OAuth 401 (no refresh_token): " + observation.Message
			}
			s.ApplyAuthenticationFailure(ctx, authProvider, msg)
			return true
		}
		// 临时停调时保持 status=active，刷新任务仍可认领该提供商。
		// 此处使 token 缓存失效，凭据更新由刷新协调器在分布式锁内完成。
		// 请求开始时的凭据可能已过期，整列写回会覆盖并发刷新得到的 refresh_token。
		msg := "Authentication failed (401): invalid or expired credentials"
		if observation.Message != "" {
			msg = "OAuth 401: " + observation.Message
		}
		if authProvider.Platform == capability.PlatformAntigravity {
			extraUpdates := AntigravityForceTokenRefreshExtra("401_invalid")
			if err := s.options.SessionWindows.UpdateExtra(ctx, authProvider.ID, extraUpdates); err != nil {
				s.options.Warn("antigravity_401_force_refresh_mark_failed", "provider_id", authProvider.ID, "error", err)
			} else {
				if authProvider.Extra == nil {
					authProvider.Extra = make(map[string]any, len(extraUpdates))
				}
				maps.Copy(authProvider.Extra, extraUpdates)
				s.options.Info("antigravity_401_force_refresh_marked", "provider_id", authProvider.ID)
			}
		}
		cooldownMinutes := s.options.UnauthorizedCooldownMinutes
		if cooldownMinutes <= 0 {
			cooldownMinutes = 10
		}
		until := s.options.Now().Add(time.Duration(cooldownMinutes) * time.Minute)
		s.notifyProviderSchedulingBlocked(authProvider, until, "oauth_401")
		if err := s.providerRepo.SetTempUnschedulable(ctx, authProvider.ID, until, msg); err != nil {
			s.options.Warn("oauth_401_set_temp_unschedulable_failed", "provider_id", authProvider.ID, "error", err)
		}
		return true
	}
	// 非 OAuth：保持 SetError 行为
	msg := "Authentication failed (401): invalid or expired credentials"
	if observation.Message != "" {
		msg = "Authentication failed (401): " + observation.Message
	}
	s.ApplyAuthenticationFailure(ctx, authProvider, msg)
	return true
}
