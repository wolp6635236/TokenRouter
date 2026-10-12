package provider

import (
	"context"
	"strings"
	"time"
)

// CNConcurrencyLimitReason 保留并发限制状态的原来源标记。
const CNConcurrencyLimitReason = "cn_concurrency_limit"

// ApplyCNConcurrencyLimit 将 Kimi 并发限制写为短期临时不可调度，
// 当前请求继续切换提供商，此类错误的累计 403 次数保持不变。
func (s *HealthService) ApplyCNConcurrencyLimit(
	ctx context.Context,
	provider *Record, reason string,
) {
	until := s.options.Now().Add(time.Duration(OpenAI403CooldownMinutesDefault) * time.Minute)
	s.notifyProviderSchedulingBlocked(provider, until, CNConcurrencyLimitReason)
	if err := s.providerRepo.SetTempUnschedulable(ctx, provider.ID, until, reason); err != nil {
		s.options.Warn("cn_concurrency_limit_set_temp_unschedulable_failed", "provider_id", provider.ID, "error", err)
		return
	}
	s.options.Info("cn_provider_concurrency_limited",
		"provider_id", provider.ID,
		"platform", provider.Platform,
		"until", until.UTC(),
	)
}

// ApplyCNInsufficientBalance 把余额不足标记为可恢复的临时停调：
// 写入绑定身份的临时停调，冷却覆盖两个余额检测周期，
// 由周期任务在余额恢复后清除。返回前已通知调度阻塞。
func (s *HealthService) ApplyCNInsufficientBalance(
	ctx context.Context,
	provider *Record,
	upstreamMsg string,
) {
	identityHash := CNUsageMonitorIdentityFingerprint(provider)
	if identityHash == "" {
		identityHash = "unknown"
	}
	msg := CNUsageMonitorReason(identityHash)
	if upstreamMsg = strings.TrimSpace(upstreamMsg); upstreamMsg != "" {
		msg += ": " + upstreamMsg
	}

	until := s.options.Now().Add(CNBalanceCooldownDuration(s.options.CNIntervalMinutes))
	s.notifyProviderSchedulingBlocked(provider, until, "cn_insufficient_balance")
	if err := s.providerRepo.SetTempUnschedulable(ctx, provider.ID, until, msg); err != nil {
		s.options.Warn("cn_balance_set_temp_unschedulable_failed", "provider_id", provider.ID, "error", err)
		return
	}
	s.options.Info("cn_provider_insufficient_balance",
		"provider_id", provider.ID,
		"platform", provider.Platform,
		"until", until.UTC(),
	)
}

// CNBalanceCooldownDuration 返回余额不足临时停调的持续时长（= 2× 余额检测周期，
// 默认 20 分钟）。周期任务会在余额恢复后提前清除，故此处只需保证冷却覆盖到下一次
// 周期检测即可。
func CNBalanceCooldownDuration(intervalMinutes int) time.Duration {
	minutes := 10
	if intervalMinutes > 0 {
		minutes = intervalMinutes
	}
	cooldown := time.Duration(minutes) * time.Minute * 2
	if cooldown < time.Minute {
		cooldown = 10 * time.Minute
	}
	return cooldown
}

// CNProviderQuotaSnapshotReset 读取 Coding Plan 统一快照中最早一个仍在未来的窗口
// 重置时间（5h / weekly）。429 多数由 5h 滚动窗口触发，取较早的重置点可避免
// 把提供商冷却到 weekly 重置（可达数天）的过度停调；如果确是 weekly 窗口耗尽，
// 周期额度探测刷新快照后阈值评估会再次停调到正确的时间点。
// 无快照或均已过期返回 nil。
func CNProviderQuotaSnapshotReset(provider *Record, now time.Time) *time.Time {
	if provider == nil || !provider.IsCNProvider() || !provider.IsCodingPlan() {
		return nil
	}
	snapshot := ValidCNUsageMonitorSnapshot(provider)
	if snapshot == nil || snapshot.Mode != "limits" {
		return nil
	}
	var earliest *time.Time
	for _, limit := range snapshot.Limits {
		t := clonePointer(limit.ResetAt)
		if t == nil || !t.After(now) {
			continue
		}
		if earliest == nil || t.Before(*earliest) {
			earliest = t
		}
	}
	return earliest
}

func (s *HealthService) ApplyCNQuotaSnapshotCooldown(ctx context.Context, provider *Record) bool {
	if provider == nil || !provider.IsCNProvider() {
		return false
	}
	// 2) Coding Plan 窗口耗尽：冷却到快照中最早的窗口重置点（见
	// CNProviderQuotaSnapshotReset 取较早窗口的重置时间，429 通常来自 5h 窗口。
	if provider.IsCodingPlan() {
		if until := CNProviderQuotaSnapshotReset(provider, s.options.Now()); until != nil {
			s.notifyProviderSchedulingBlocked(provider, *until, "429")
			if err := s.providerRepo.SetRateLimited(ctx, provider.ID, *until); err != nil {
				s.options.Warn("rate_limit_set_failed", "provider_id", provider.ID, "error", err)
				return true
			}
			s.options.Info("cn_coding_plan_rate_limited",
				"provider_id", provider.ID,
				"platform", provider.Platform,
				"reset_at", *until,
			)
			return true
		}
	}
	return false
}
