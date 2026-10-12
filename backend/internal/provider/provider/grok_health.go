package provider

import (
	"context"
	"log/slog"
	"maps"
	"net/http"
	"time"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/upstream/grok"
)

const grokQuotaSnapshotExtraKey = "grok_usage_snapshot"

// GrokHealthStore 保留现有快照与状态写权限，扩展/恢复 CAS 仍由实际存储能力提供。
type GrokHealthStore interface {
	providercore.GrokRateLimitWriter
	UpdateExtra(context.Context, int64, map[string]any) error
	SetTempUnschedulable(context.Context, int64, time.Time, string) error
}

// GrokHealth 复用应用持有的节流与运行状态，执行 Grok 提供商健康规则。
// @project-doc docs/interfaces/grok_upstream.md#grok_account_contract
type GrokHealth struct {
	NormalizeModel func(*providercore.Record, string) string
	Store          GrokHealthStore
	Throttle       *providercore.WriteThrottle
	Runtime        *providercore.RuntimeBlockState
	Health         *UpstreamHealth
	ModelTransient *providercore.ModelTransientState
}

// ProviderStateContext 为提供商状态写入保留五秒独立预算。
func ProviderStateContext(ctx context.Context) (context.Context, context.CancelFunc) {
	base := context.Background()
	if ctx != nil {
		base = context.WithoutCancel(ctx)
	}
	return context.WithTimeout(base, 5*time.Second)
}

func (s *GrokHealth) StoreSnapshot(ctx context.Context, value *providercore.Record, snapshot *grok.QuotaSnapshot, installRateLimit bool, teamModel string) {
	if s == nil || value == nil || value.ID <= 0 || snapshot == nil {
		return
	}
	providerID := value.ID
	now := time.Now()
	resetAt, hasActiveLimit := providercore.GrokRateLimitResetAtForProvider(value, snapshot, now)
	if hasActiveLimit {
		providercore.NormalizeGrokExhaustedWindowResets(snapshot, resetAt, now)
	}
	recovery := providercore.IsSuccessfulGrokRateLimitRecovery(value, snapshot)
	critical := snapshot.StatusCode == http.StatusTooManyRequests || hasActiveLimit || recovery
	if s.Throttle != nil {
		allowed := s.Throttle.Allow(providerID, now)
		if !critical && !allowed {
			return
		}
	}

	updates := map[string]any{
		grokQuotaSnapshotExtraKey: snapshot,
	}
	// 同时派生 grokThresholdCandidates 评估器读取的调度阈值扩展字段 grok_sched_*。
	// 缺少此写入逻辑时，管理员配置的 Grok 自动暂停阈值无法触发。
	maps.Copy(updates, providercore.BuildGrokSchedulerExtraUpdates(snapshot))
	stateCtx := ctx
	if hasActiveLimit {
		var cancel context.CancelFunc
		stateCtx, cancel = ProviderStateContext(ctx)
		defer cancel()
	}
	// 请求路径中的 Provider 指针来自每次 Redis/DB 解码，不是进程内共享缓存；
	// 这里与 token 刷新和限流写入保持一致，调用方不得跨 goroutine 复用同一指针。
	if value.Extra == nil {
		value.Extra = map[string]any{}
	}
	value.Extra[grokQuotaSnapshotExtraKey] = snapshot
	if s.Store != nil {
		_ = s.Store.UpdateExtra(stateCtx, providerID, updates)
	}
	// 池模式由上游切换提供商，本地保存额度观测。
	// 非池模式将错误或成功响应中的耗尽窗口写入提供商限流。
	if installRateLimit && hasActiveLimit && !value.IsPoolMode() {
		s.RateLimit(stateCtx, value, resetAt, teamModel)
	} else if recovery {
		providercore.ClearGrokRateLimitAfterRecovery(stateCtx, s.Store, providercore.CloneRecord(value), slog.Warn)
	}
}

func (s *GrokHealth) RateLimit(ctx context.Context, value *providercore.Record, resetAt time.Time, teamModel string) {
	if s == nil || value == nil {
		return
	}
	now := time.Now()
	resetAt = providercore.NormalizeGrokRateLimitResetAt(value, resetAt, now)

	runtimeUntil := resetAt
	if value.TempUnschedulableUntil != nil && value.TempUnschedulableUntil.After(runtimeUntil) {
		runtimeUntil = *value.TempUnschedulableUntil
	}
	s.Runtime.BlockProviderScheduling(value, runtimeUntil, "429")
	providercore.PersistGrokRateLimit(ctx, s.Store, providercore.CloneRecord(value), resetAt, slog.Warn)

	// 扩散短期团队与模型冷却，使同一 xAI 团队的关联 OAuth 提供商跳过热点模型，
	// 无需等待每个提供商分别收到 429；空模型不扩散团队冷却。
	if teamModel != "" {
		providercore.MarkGrokTeamModelRateLimit(value, teamModel, providercore.ResolveGrokTeamRateLimitUntil(resetAt, now))
	}
}

func (s *GrokHealth) TempUnschedule(ctx context.Context, value *providercore.Record, cooldown time.Duration, reason string) {
	if s == nil || value == nil {
		return
	}
	until := time.Now().Add(cooldown)
	if value.TempUnschedulableUntil != nil && value.TempUnschedulableUntil.After(until) {
		until = *value.TempUnschedulableUntil
	}
	s.Runtime.BlockProviderScheduling(value, until, reason)
	if s.Store != nil {
		stateCtx, cancel := ProviderStateContext(ctx)
		defer cancel()
		_ = s.Store.SetTempUnschedulable(stateCtx, value.ID, until, reason)
	}
}

// ObserveResponse 保留先解析和标记、再写快照的顺序；没有额度头时只尝试精确恢复。
func (s *GrokHealth) ObserveResponse(ctx context.Context, value *providercore.Record, headers http.Header, status int, model string) {
	snapshot := grok.ParseQuotaObservation(headers, status, time.Now())
	if snapshot != nil {
		providercore.StampGrokQuotaPlan(providercore.CloneRecord(value), snapshot, model, grok.ResolveGrokTextResponsesModelID, grok.ApplyGrok45ResponsesPlanSignal)
		s.StoreSnapshot(ctx, value, snapshot, true, model)
		return
	}
	if providercore.IsSuccessfulGrokRateLimitRecovery(providercore.CloneRecord(value), &grok.QuotaSnapshot{StatusCode: status}) {
		providercore.ClearGrokRateLimitAfterRecovery(ctx, s.Store, providercore.CloneRecord(value), slog.Warn)
	}
}
