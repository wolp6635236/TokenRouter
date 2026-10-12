package routing

import (
	"slices"

	"github.com/TokenFlux/TokenRouter/internal/protocol"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

// ModelChain 分别保存客户端、Key 重定向后、分组映射后与提供商映射后的模型。每个阶段应用一次映射。
// 执行层完成供应商名称规范化和响应恢复，用量记录读取本次模型链。
type ModelChain struct {
	ClientModel, RequestedModel, GroupMappedModel, ProviderMappedModel string
	APIKeyRedirected, GroupMapped                                      bool
	RestrictionModelSource                                             string
	RestrictModels                                                     bool
	PricingConfigID                                                    int64
	BillingModelSource                                                 string
}

// PlanInput 包含最终分组、请求模型和客户端协议。
type PlanInput struct {
	GroupID        *int64
	RequestedModel string
	GroupMapping   GroupMappingResult
	Group          *Group
	ClientProtocol capability.ProtocolID
}

// RoutePlan 保存本次分组和协议策略，候选解析时确定提供商及其模型映射。
// 私有字段保存一次尝试使用的协议回退表。
type RoutePlan struct {
	models         ModelChain
	groupID        int64
	schedulerType  GroupSchedulerType
	clientProtocol capability.ProtocolID
	allowed        []capability.ProtocolID
	fallbacks      map[capability.ProtocolID][]capability.ProtocolID
}

// CandidatePlan 是单个候选的本次路由结果。
type CandidatePlan struct {
	Models           ModelChain
	ProviderID       int64
	GroupID          int64
	ClientProtocol   capability.ProtocolID
	UpstreamProtocol capability.ProtocolID
}

// Mapping 从路由计划返回本次模型映射、白名单阶段和计费元数据。
func (p RoutePlan) Mapping() GroupMappingResult {
	return GroupMappingResult{
		MappedModel:            p.models.GroupMappedModel,
		PricingConfigID:        p.models.PricingConfigID,
		Mapped:                 p.models.GroupMapped,
		BillingModelSource:     p.models.BillingModelSource,
		ClientModel:            p.models.ClientModel,
		APIKeyRedirected:       p.models.APIKeyRedirected,
		RestrictModels:         p.models.RestrictModels,
		RestrictionModelSource: p.models.RestrictionModelSource,
	}
}

func (p RoutePlan) Models() ModelChain { return p.models }

// ResolveModel 使用本次提供商快照解析模型，返回候选的模型映射结果。
func (p CandidatePlan) ResolveModel(snapshot provider.ProviderSnapshot, requested string) (CandidatePlan, bool) {
	mapped, matched := snapshot.ModelPolicy.Resolve(requested)
	p.Models.ProviderMappedModel = mapped
	return p, matched
}

// Plan 复制已通过入口准入的分组数据，构造请求路由计划。
func Plan(input PlanInput) RoutePlan {
	plan := RoutePlan{
		clientProtocol: input.ClientProtocol,
		models: ModelChain{
			RequestedModel:         input.RequestedModel,
			ClientModel:            input.GroupMapping.ClientModel,
			APIKeyRedirected:       input.GroupMapping.APIKeyRedirected,
			GroupMappedModel:       input.GroupMapping.MappedModel,
			GroupMapped:            input.GroupMapping.Mapped,
			RestrictModels:         input.GroupMapping.RestrictModels,
			RestrictionModelSource: input.GroupMapping.RestrictionModelSource,
			PricingConfigID:        input.GroupMapping.PricingConfigID,
			BillingModelSource:     input.GroupMapping.BillingModelSource,
		},
	}
	if input.Group != nil {
		plan.groupID = input.Group.ID
		plan.schedulerType = input.Group.SchedulerType
		plan.allowed = slices.Clone(input.Group.AllowedProtocols)
		plan.fallbacks = protocol.CloneFallbacks(input.Group.ProtocolFallbacks)
	}
	if input.GroupID != nil {
		plan.groupID = *input.GroupID
	}
	return plan
}

// ResolveCandidate 优先选择提供商直接支持的协议，其次尝试单步转换，每次候选刷新或数据库复核时重新调用。
func (p RoutePlan) ResolveCandidate(candidate provider.ProviderSnapshot) (CandidatePlan, bool) {
	target, ok := capability.ResolveRoute(candidate.Protocols(), p.clientProtocol, p.fallbacks)
	if !ok {
		return CandidatePlan{}, false
	}
	return CandidatePlan{Models: p.models, ProviderID: candidate.ID, GroupID: p.groupID, ClientProtocol: p.clientProtocol, UpstreamProtocol: target}, true
}

// GroupID 返回计划中的最终分组 ID。
func (p RoutePlan) GroupID() int64 { return p.groupID }

func (p RoutePlan) SchedulerType() GroupSchedulerType { return p.schedulerType }

func (p RoutePlan) AllowedProtocols() []capability.ProtocolID { return slices.Clone(p.allowed) }

// WithClientProtocol 使用执行层已确定的业务入口，保留计划其它不可变值。
func (p RoutePlan) WithClientProtocol(source capability.ProtocolID) RoutePlan {
	p.clientProtocol = source
	return p
}
