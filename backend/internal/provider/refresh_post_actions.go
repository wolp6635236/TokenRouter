package provider

import (
	"context"
	"maps"
	"strings"
	"time"
)

// RefreshPostActions 依次执行刷新成功后的清理、缓存失效、同步和隐私设置。
type RefreshPostActions struct {
	Now                       func() time.Time
	Info, Warn, Debug         func(string, ...any)
	Privacy                   *PrivacyService
	RequestClearer            RefreshRequestClearer
	ClearError, ClearCooldown func(context.Context, *Record) (bool, error)
	Invalidate, SyncProvider  func(context.Context, *Record) error
	DeleteCooldown            func(context.Context, int64) error
	ClearBlock                func(int64)
	NeedsReauth               func(*Record) bool
	ClearReauth               func(context.Context, *Record)
}

// Run 刷新成功后的后续动作（清除错误状态、缓存失效、调度器同步等）。
func (s *RefreshPostActions) Run(ctx context.Context, provider *Record) {
	syncActions := *s
	changed := false
	s.ClearRefreshRequest(ctx, provider, "success")

	// Antigravity 提供商：如果之前是因为缺少 project_id 而标记为 error，现在成功获取到了，清除错误状态
	if provider.Platform == PlatformAntigravity &&
		provider.Status == StatusError &&
		strings.Contains(provider.ErrorMessage, "missing_project_id:") {
		if applied, clearErr := s.ClearError(ctx, provider); clearErr != nil {
			s.Warn("token_refresh.clear_provider_error_failed",
				"provider_id", provider.ID,
				"error", clearErr,
			)
		} else if applied {
			provider.Status = StatusActive
			provider.ErrorMessage = ""
			s.Info("token_refresh.cleared_missing_project_id_error", "provider_id", provider.ID)
			s.ClearBlock(provider.ID)
		} else {
			changed = true
		}
	}
	// 刷新成功后清除临时不可调度状态（处理 OAuth 401 恢复场景）
	if provider.TempUnschedulableUntil != nil && s.Now().Before(*provider.TempUnschedulableUntil) {
		applied, clearErr := s.ClearCooldown(ctx, provider)
		if clearErr != nil {
			s.Warn("token_refresh.clear_temp_unschedulable_failed", "provider_id", provider.ID, "error", clearErr)
		} else if applied {
			provider.TempUnschedulableUntil = nil
			provider.TempUnschedulableReason = ""
			s.Info("token_refresh.cleared_temp_unschedulable", "provider_id", provider.ID)
			s.ClearBlock(provider.ID)
		} else {
			changed = true
		}
		// 原存储失败仍尽力清缓存；明确的身份/窗口冲突不能清掉新状态的缓存。
		if (applied || clearErr != nil) && s.DeleteCooldown != nil {
			if err := s.DeleteCooldown(ctx, provider.ID); err != nil {
				s.Warn("token_refresh.clear_temp_unsched_cache_failed", "provider_id", provider.ID, "error", err)
			}
		}
	}
	// 身份或健康窗口冲突后清理已交换的 token 缓存，随后结束本次后置处理。
	if changed {
		syncActions.SyncProvider = nil
	}
	syncActions.Sync(ctx, provider)
	if changed {
		return
	}
	// OpenAI OAuth: 刷新成功后，检查是否已设置 privacy_mode，未设置则尝试关闭训练数据共享
	s.Privacy.RefreshOpenAIPrivacy(ctx, provider)
	// Antigravity OAuth: 刷新成功后，检查是否已设置 privacy_mode，未设置则调用 setUserSettings
	s.Privacy.RefreshAntigravityPrivacy(ctx, provider)
	// Grok 凭证刷新成功后清除软性重新认证标记。
	if provider != nil && provider.Platform == PlatformGrok && s.NeedsReauth(provider) {
		s.ClearReauth(ctx, provider)
	}
}

func (s *RefreshPostActions) SyncWithCleanup(parent context.Context, provider *Record) {
	cleanupParent := context.Background()
	if parent != nil {
		cleanupParent = context.WithoutCancel(parent)
	}
	ctx, cancel := context.WithTimeout(cleanupParent, DefaultTokenRefreshCleanupTimeout)
	defer cancel()
	s.Sync(ctx, provider)
}

func (s *RefreshPostActions) Sync(ctx context.Context, provider *Record) {
	// 对所有 OAuth 提供商调用缓存失效（InvalidateToken 内部根据平台判断是否需要处理）
	if s.Invalidate != nil && (provider.Type == ProviderTypeOAuth || provider.IsQoderCosy()) {
		if err := s.Invalidate(ctx, provider); err != nil {
			s.Warn("token_refresh.invalidate_token_cache_failed",
				"provider_id", provider.ID,
				"error", err,
			)
		} else {
			s.Debug("token_refresh.token_cache_invalidated", "provider_id", provider.ID)
		}
	}
	// 同步更新调度缓存中的提供商凭据。
	if s.SyncProvider != nil {
		if err := s.SyncProvider(ctx, provider); err != nil {
			s.Warn("token_refresh.sync_scheduler_cache_failed",
				"provider_id", provider.ID,
				"error", err,
			)
		} else {
			s.Debug("token_refresh.scheduler_cache_synced", "provider_id", provider.ID)
		}
	}
}

// ClearRefreshRequest 在刷新完成或确定不可恢复后清除一次性强制刷新标记。
func (s *RefreshPostActions) ClearRefreshRequest(ctx context.Context, provider *Record, outcome string) {
	if s == nil || provider == nil || !NeedsAntigravityRefreshRequest(provider) {
		return
	}
	updates := ClearedAntigravityRefreshRequest()
	clearer := s.RequestClearer
	ok := clearer != nil
	if !ok {
		s.Warn("token_refresh.clear_antigravity_force_refresh_failed", "provider_id", provider.ID, "outcome", outcome, "error", "conditional refresh request clearer is not configured")
		return
	}
	version := FailureVersion(provider).CredentialVersion
	if outcome == "non_retryable" {
		version.Status = StatusError
	}
	applied, err := clearer.ClearAntigravityRefreshRequest(ctx, version)
	if err != nil {
		s.Warn("token_refresh.clear_antigravity_force_refresh_failed",
			"provider_id", provider.ID,
			"outcome", outcome,
			"error", err,
		)
		return
	}
	if !applied {
		s.Info("token_refresh.antigravity_force_refresh_clear_skipped_stale_credentials", "provider_id", provider.ID)
		return
	}
	if provider.Extra != nil {
		maps.Copy(provider.Extra, updates)
	}
	s.Info("token_refresh.cleared_antigravity_force_refresh",
		"provider_id", provider.ID,
		"outcome", outcome,
	)
}
