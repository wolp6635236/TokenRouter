package provider

import (
	"context"
	"slices"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	"github.com/TokenFlux/TokenRouter/internal/protocol"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing"
)

// CandidateSnapshot 返回候选快照，并在快照中设置本次启用的协议。
func (p ModelPolicy) CandidateSnapshot() provider.ProviderSnapshot {
	if p.Record == nil {
		return provider.ProviderSnapshot{}
	}
	snapshot := p.Record.RoutingSnapshot()
	snapshot.EnabledProtocols = slices.Clone(p.Record.UpstreamProtocolsForLegacy(p.protocolTarget().GetAPIProtocol()))
	return snapshot
}

// ProtocolRoute 根据分组协议配置复核候选，返回上游协议。
func (p ModelPolicy) ProtocolRoute(group *routing.Group, source protocol.ProtocolID) (protocol.ProtocolID, bool) {
	if p.Record == nil {
		return "", false
	}
	var projected *routing.Group
	if group != nil {
		projected = &routing.Group{ID: group.ID, SchedulerType: group.SchedulerType, AllowedProtocols: group.AllowedProtocols, ProtocolFallbacks: group.ProtocolFallbacks}
	}
	plan := routing.Plan(routing.PlanInput{Group: projected, ClientProtocol: source})
	candidate, ok := plan.ResolveCandidate(p.CandidateSnapshot())
	return candidate.UpstreamProtocol, ok
}

// AllowsProtocol 根据请求分组和客户端协议检查提供商资格。
func (p ModelPolicy) AllowsProtocol(ctx context.Context) bool {
	group, _ := requeststate.GroupFromContext(ctx)
	if p.Record == nil || group != nil && (group.RequireOAuthOnly && !p.Record.IsOAuth() || group.RequirePrivacySet && !p.Record.IsPrivacySet()) {
		return false
	}
	source, _ := requeststate.ClientProtocolFromContext(ctx)
	if source == "" {
		return true
	}
	_, ok := p.ProtocolRoute(group, source)
	return ok
}

// Schedulable 依次检查协议、提供商状态和模型可用性。
func (p ModelPolicy) Schedulable(ctx context.Context, model string) bool {
	if p.Record == nil {
		return false
	}
	if !p.AllowsProtocol(ctx) || !p.Record.IsSchedulable() {
		return false
	}
	return p.AllowsModel(ctx, model)
}

// Limited 检查模型各限流窗口是否生效。
func (p ModelPolicy) Limited(ctx context.Context, model string) bool {
	return slices.ContainsFunc(p.LimitKeys(ctx, model), p.Record.ModelRateLimitActive)
}

// LimitRemaining 返回各模型限流窗口中最长的剩余时间。
func (p ModelPolicy) LimitRemaining(ctx context.Context, model string) time.Duration {
	remaining := time.Duration(0)
	for _, key := range p.LimitKeys(ctx, model) {
		if value := p.Record.ModelRateLimitRemaining(key); value > remaining {
			remaining = value
		}
	}
	return remaining
}

// FinalAntigravityModel 根据模型和本次 thinking 设置解析 Antigravity 上游模型。
func (p ModelPolicy) FinalAntigravityModel(ctx context.Context, model string) string {
	return provideradapter.FinalAntigravityModel(p.Record, model, modelThinking(ctx))
}
