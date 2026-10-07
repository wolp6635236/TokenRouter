package requeststate

import (
	"fmt"

	"github.com/TokenFlux/TokenRouter/internal/protocol"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing"
)

// AttemptRoute 保存当前候选的协议和模型计划。
// 值复制保持已选计划独立；模型映射在原调用时点由调用方传入。
type AttemptRoute struct {
	protocol  protocol.ProtocolID
	candidate routing.CandidatePlan
	planned   bool
}

func (a AttemptRoute) Protocol() protocol.ProtocolID { return a.protocol }

// Candidate 返回已捕获的值；缺少原计划时不会补造或重新查询。
func (a AttemptRoute) Candidate() (routing.CandidatePlan, bool) {
	return a.candidate, a.planned
}

// ResolveAttempt 在刷新提供商后复核能力，分组回退后此前的计划失效。
func (s RoutingState) ResolveAttempt(snapshot provider.ProviderSnapshot, previous AttemptRoute) (AttemptRoute, bool, error) {
	plan, planned := s.plan, s.planSet
	if planned && s.group != nil && plan.GroupID() != s.group.ID {
		planned = false
	}
	if previous.protocol != "" && !planned {
		return previous, false, nil
	}
	if s.clientProtocol == "" {
		return previous, false, nil
	}
	if !planned {
		plan = routing.Plan(routing.PlanInput{Group: s.group, ClientProtocol: s.clientProtocol})
	} else {
		plan = plan.WithClientProtocol(s.clientProtocol)
	}
	candidate, ok := plan.ResolveCandidate(snapshot)
	if !ok {
		return AttemptRoute{}, false, fmt.Errorf("provider %d has no enabled route for %s", snapshot.ID, s.clientProtocol)
	}
	result := AttemptRoute{protocol: candidate.UpstreamProtocol, planned: planned}
	if planned {
		result.candidate = candidate
	}
	return result, true, nil
}

// ResolveModel 复用原生一跳规则，不在选择候选时提前读取模型配置。
func (a AttemptRoute) ResolveModel(id int64, platform string, mapping map[string]string, requested string) (string, bool) {
	if a.planned {
		snapshot := provider.ProviderSnapshot{ID: id, Platform: platform, ModelPolicy: provider.NewModelRoutingSnapshot(mapping)}
		candidate, matched := a.candidate.ResolveModel(snapshot, requested)
		return candidate.Models.ProviderMappedModel, matched
	}
	return provider.ResolveMappedModel(mapping, requested)
}
