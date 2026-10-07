package provider

import (
	"context"
	"strings"

	"github.com/TokenFlux/TokenRouter/internal/batchimage"
	"github.com/TokenFlux/TokenRouter/internal/protocol"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing"
)

// CandidateProviders 保留批量任务的三种提供商查询，不提前扩大查询范围。
type CandidateProviders interface {
	ResultProviders
	ListSchedulableByPlatform(context.Context, string) ([]provider.Record, error)
	ListSchedulableByGroupIDAndPlatform(context.Context, int64, string) ([]provider.Record, error)
}

// Candidates 将查询到的提供商转换为批量任务候选，使用注入的注册表和模型观察函数。
type Candidates struct {
	Source       CandidateProviders
	Registry     *batchimage.Registry[BatchImageProvider]
	ObserveModel func(context.Context, string)
}

func (r *Candidates) GetByID(ctx context.Context, id int64) (*batchimage.Candidate, error) {
	value, err := r.Source.GetByID(ctx, id)
	return r.Project(value), err
}

func (r *Candidates) ListSchedulableByPlatform(ctx context.Context, platform string) ([]batchimage.Candidate, error) {
	values, err := r.Source.ListSchedulableByPlatform(ctx, platform)
	return r.project(values), err
}

func (r *Candidates) ListSchedulableByGroupIDAndPlatform(ctx context.Context, id int64, platform string) ([]batchimage.Candidate, error) {
	values, err := r.Source.ListSchedulableByGroupIDAndPlatform(ctx, id, platform)
	return r.project(values), err
}

func (r *Candidates) project(values []provider.Record) []batchimage.Candidate {
	out := make([]batchimage.Candidate, len(values))
	for i := range values {
		out[i] = *r.Project(&values[i])
	}
	return out
}

// Project 按原时机执行资格及一跳模型映射，不把候选写回共享提供商数据。
func (r *Candidates) Project(value *provider.Record) *batchimage.Candidate {
	if value == nil {
		return nil
	}
	result := &batchimage.Candidate{ID: value.ID, Priority: value.Priority, CandidateRules: candidateRules{value}}
	result.ProtocolEnabled = func() bool {
		_, ok := routing.Plan(routing.PlanInput{ClientProtocol: protocol.ProtocolImageBatches}).ResolveCandidate(value.RoutingSnapshot())
		return ok
	}
	result.SupportsProvider = func(name string) bool {
		selected, ok := r.Registry.Get(name)
		return ok && selected != nil && selected.SupportsProvider(provider.CloneRecord(value))
	}
	result.Bind = func(name string) batchimage.ExecutionProvider {
		selected, _ := r.Registry.Get(name)
		return BindProvider(selected, value)
	}
	// 批量提供商按 Gemini/Vertex 规则解析模型。
	result.ResolveUpstream = func(ctx context.Context, model string) string {
		resolved := strings.TrimSpace(provider.ResolveForwardMappedModel(value, model, provideradapter.ModelDefaults()))
		if r.ObserveModel != nil {
			r.ObserveModel(ctx, resolved)
		}
		return resolved
	}
	return result
}

type candidateRules struct{ *provider.Record }

func (r candidateRules) GetModelMapping() map[string]string {
	return provider.ResolveModelMapping(r.Record, provideradapter.ModelDefaults())
}

func (r candidateRules) IsModelSupported(model string) bool {
	return r.Record.IsModelSupported(model, provideradapter.ModelDefaults(), provideradapter.ModelRules(r.Record))
}

func (r candidateRules) ResolveMappedModel(model string) (string, bool) {
	return provider.ResolveMappedModel(r.GetModelMapping(), model)
}
