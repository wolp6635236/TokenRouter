package httpapi

import (
	"context"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/provider/httpapi/dto"
)

// ProviderWithConcurrency 包含管理端的实时并发与调度展示字段。
type ProviderWithConcurrency struct {
	*dto.Provider
	CurrentConcurrency int                           `json:"current_concurrency"`
	SchedulerScore     *ProviderSchedulerScore       `json:"scheduler_score,omitempty"`
	SchedulerScores    []ProviderSchedulerGroupScore `json:"scheduler_scores,omitempty"`
	// 以下字段仅对 Anthropic OAuth/SetupToken 提供商有效，且仅在启用相应功能时返回
	CurrentWindowCost *float64 `json:"current_window_cost,omitempty"` // 当前窗口费用
	ActiveSessions    *int     `json:"active_sessions,omitempty"`     // 当前活跃会话数
	CurrentRPM        *int     `json:"current_rpm,omitempty"`         // 当前分钟 RPM 计数
}

// 提供商管理用例计算调度展示值，HTTP 将其编码为 JSON。
type (
	ProviderSchedulerScore      = providercore.ProviderSchedulerScore
	ProviderSchedulerGroupScore = providercore.ProviderSchedulerGroupScore
)

// RuntimePresenter 将运行数据和母提供商信息转换为管理 DTO，查询和阈值计算由提供商用例执行。
type RuntimePresenter struct {
	status  *providercore.RuntimeStatusReader
	parents interface {
		GetProvidersByIDs(context.Context, []int64) ([]*providercore.Record, error)
	}
	ollama *providercore.OllamaCloudUsageService
}

func NewRuntimePresenter(status *providercore.RuntimeStatusReader, parents interface {
	GetProvidersByIDs(context.Context, []int64) ([]*providercore.Record, error)
}, ollama *providercore.OllamaCloudUsageService,
) *RuntimePresenter {
	return &RuntimePresenter{status, parents, ollama}
}

func (p *RuntimePresenter) Present(ctx context.Context, v *providercore.Record) ProviderWithConcurrency {
	state := p.status.Read(ctx, v)
	item := p.Project(state)
	p.EnrichShadowParents(ctx, []ProviderWithConcurrency{item})
	return item
}

// EnrichShadowParentInfo 把母提供商的展示信息回填到影子行的 parent_* 字段。
// 纯函数：仅依赖传入的母提供商 map，便于单测；非影子或母提供商缺失时优雅留空。
func EnrichShadowParentInfo(items []ProviderWithConcurrency, parents map[int64]*providercore.Record) {
	for i := range items {
		a := items[i].Provider
		if a == nil || a.ParentProviderID == nil {
			continue
		}
		p := parents[*a.ParentProviderID]
		if p == nil {
			continue
		}
		a.ParentEmail = p.GetCredential("email")
		a.ParentPlanType = p.GetCredential("plan_type")
		a.ParentSubscriptionExpiresAt = p.GetCredential("subscription_expires_at")
		a.ParentChatGPTAccountID = p.GetCredential("chatgpt_account_id")
		a.ParentPrivacyMode = p.GetExtraString("privacy_mode")
	}
}

// EnrichShadowParents 收集本批影子的母提供商 ID，批量查询后回填展示数据。
// 解析失败时不报错（parent_* 留空，降级）。
func (p *RuntimePresenter) EnrichShadowParents(ctx context.Context, items []ProviderWithConcurrency) {
	seen := make(map[int64]struct{})
	for i := range items {
		a := items[i].Provider
		if a == nil || a.ParentProviderID == nil {
			continue
		}
		seen[*a.ParentProviderID] = struct{}{}
	}
	if len(seen) == 0 {
		return
	}
	parentIDs := make([]int64, 0, len(seen))
	for pid := range seen {
		parentIDs = append(parentIDs, pid)
	}
	parents, err := p.parents.GetProvidersByIDs(ctx, parentIDs)
	if err != nil {
		return
	}
	pmap := make(map[int64]*providercore.Record, len(parents))
	for _, p := range parents {
		pmap[p.ID] = p
	}
	EnrichShadowParentInfo(items, pmap)
}

// Project 将已读取的运行状态转换为管理展示字段。
func (p *RuntimePresenter) Project(state providercore.RuntimeStatus) ProviderWithConcurrency {
	item := ProviderWithConcurrency{Provider: dto.ProviderFromRecord(state.Record), CurrentConcurrency: state.CurrentConcurrency, CurrentWindowCost: state.CurrentWindowCost, ActiveSessions: state.ActiveSessions, CurrentRPM: state.CurrentRPM, SchedulerScore: state.SchedulerScore, SchedulerScores: state.SchedulerScores}
	if item.Provider == nil {
		return item
	}
	if p.ollama != nil {
		p.ollama.EnrichState(item.OllamaCloudUsage)
	}
	return item
}
