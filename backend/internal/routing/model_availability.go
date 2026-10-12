package routing

import (
	"context"
	"strings"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

// ModelAvailabilityDiagnosis 描述请求模型是否被分组内任一持久可用提供商支持。
// 持久可用指提供商为 active 且启用 schedulable；诊断忽略限流、过载、临时不可调度和
// 运行时阻断等瞬时状态，供 handler 区分 404 model_not_found 与 503 service_unavailable。
type ModelAvailabilityDiagnosis struct {
	// HasProvidersInPool 表示分组内、满足专用入口平台过滤的持久可用提供商存在。
	HasProvidersInPool bool
	// HasModelSupport 表示至少有一个提供商的模型映射允许请求模型。
	HasModelSupport bool
}

// ModelAvailabilityDiagnoser 提供模型静态可用性的窄读取能力，供入口复用同一分类器。
type ModelAvailabilityDiagnoser interface {
	DiagnoseModelAvailabilityForPlatform(
		ctx context.Context,
		groupID *int64,
		requestedModel string,
		platform string,
	) ModelAvailabilityDiagnosis
}

// AvailabilityProvider 包含提供商平台和模型支持判断函数。
type AvailabilityProvider struct {
	Platform string
	Supports func(context.Context, string) bool
}
type AvailabilityReader func(context.Context, *int64, []string, bool) ([]AvailabilityProvider, error)

// ModelAvailability 用已有查询边界区分永久模型缺失和暂时容量不足；自身无缓存。
type ModelAvailability struct {
	Read     AvailabilityReader
	MapModel func(context.Context, *int64, string) string
}

// ModelAvailabilityDiagnoserFunc 将已绑定诊断意图交给消费者，不新增查询或缓存。
type ModelAvailabilityDiagnoserFunc func(context.Context, *int64, string, string) ModelAvailabilityDiagnosis

// DiagnoseGeneral 通过专用持久配置查询检查指定平台的提供商，
// 判断请求模型是否被配置支持。该查询绕过调度快照，忽略限流、过载、临时不可调度、
// 到期窗口、额度和运行时阻断等瞬时状态。
//
// 该方法用于错误路径：内部失败或输入无法诊断时返回 {true,true}，
// 让调用方保守地继续走 503 分支，避免误报 404。
func (s *ModelAvailability) DiagnoseGeneral(
	ctx context.Context,
	groupID *int64,
	requestedModel string,
	platform string,
) ModelAvailabilityDiagnosis {
	if s == nil {
		return ModelAvailabilityDiagnosis{HasProvidersInPool: true, HasModelSupport: true}
	}
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" {
		// 空模型无法判断 model_not_found，交给调用方回落到 503。
		return ModelAvailabilityDiagnosis{HasProvidersInPool: true, HasModelSupport: true}
	}
	if groupID == nil || *groupID <= 0 {
		return ModelAvailabilityDiagnosis{}
	}
	if s.Read == nil {
		return ModelAvailabilityDiagnosis{HasProvidersInPool: true, HasModelSupport: true}
	}
	providers, err := s.Read(ctx, groupID, availabilityPlatforms(platform), false)
	if err != nil {
		return ModelAvailabilityDiagnosis{HasProvidersInPool: true, HasModelSupport: true}
	}

	diag := ModelAvailabilityDiagnosis{}
	routingModel := requestedModel
	if s.MapModel != nil {
		routingModel = s.MapModel(ctx, groupID, requestedModel)
	}
	for i := range providers {
		if platform != "" && providers[i].Platform != strings.TrimSpace(platform) {
			continue
		}
		diag.HasProvidersInPool = true
		if providers[i].Supports(ctx, routingModel) {
			diag.HasModelSupport = true
			return diag
		}
	}
	return diag
}

// DiagnoseCompatible 判断请求模型是否被分组内指定 OpenAI 兼容平台提供商配置支持。
// platform 非空时附加强制平台过滤；为空时检查同组全部平台的静态能力。
// 诊断使用持久配置查询，绕过调度快照并忽略瞬时运行状态。
//
// 该方法用于错误路径：内部失败、空模型或 nil service 时返回 {true,true}，
// 让调用方保守地继续走 503 分支。
func (s *ModelAvailability) DiagnoseCompatible(
	ctx context.Context,
	groupID *int64,
	requestedModel string,
	platform string,
) ModelAvailabilityDiagnosis {
	if s == nil {
		return ModelAvailabilityDiagnosis{HasProvidersInPool: true, HasModelSupport: true}
	}
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" {
		return ModelAvailabilityDiagnosis{HasProvidersInPool: true, HasModelSupport: true}
	}
	if groupID == nil || *groupID <= 0 {
		return ModelAvailabilityDiagnosis{}
	}
	routingModel := requestedModel
	if s.MapModel != nil {
		routingModel = s.MapModel(ctx, groupID, requestedModel)
	}
	return s.DiagnoseCompatibleRouting(ctx, groupID, routingModel, platform)
}

// DiagnoseCompatibleRouting 直接诊断已经完成分组映射及协议专用映射的提供商层模型。
// Messages 错误路径使用该入口，避免把 D 再次当作客户端模型执行分组映射。
func (s *ModelAvailability) DiagnoseCompatibleRouting(
	ctx context.Context,
	groupID *int64,
	routingModel string,
	platform string,
) ModelAvailabilityDiagnosis {
	if s == nil {
		return ModelAvailabilityDiagnosis{HasProvidersInPool: true, HasModelSupport: true}
	}
	routingModel = strings.TrimSpace(routingModel)
	if routingModel == "" {
		return ModelAvailabilityDiagnosis{HasProvidersInPool: true, HasModelSupport: true}
	}
	if groupID == nil || *groupID <= 0 {
		return ModelAvailabilityDiagnosis{}
	}
	if s.Read == nil {
		return ModelAvailabilityDiagnosis{HasProvidersInPool: true, HasModelSupport: true}
	}

	providers, err := s.Read(ctx, groupID, availabilityPlatforms(platform), false)
	if err != nil {
		// 查询失败时保守返回 503 分支，避免临时查询错误误判为 404 model_not_found。
		return ModelAvailabilityDiagnosis{HasProvidersInPool: true, HasModelSupport: true}
	}

	diag := ModelAvailabilityDiagnosis{}
	for i := range providers {
		if platform != "" && providers[i].Platform != strings.TrimSpace(platform) {
			continue
		}
		diag.HasProvidersInPool = true
		// 与提供商选择共用默认目录、显式模型范围和协议资格。
		if providers[i].Supports(ctx, routingModel) {
			diag.HasModelSupport = true
			return diag
		}
	}
	return diag
}

func (f ModelAvailabilityDiagnoserFunc) DiagnoseModelAvailabilityForPlatform(ctx context.Context, group *int64, model, platform string) ModelAvailabilityDiagnosis {
	return f(ctx, group, model, platform)
}

// availabilityPlatforms 与调度使用同一提供商平台目录，候选始终限定在分组成员内。
func availabilityPlatforms(platform string) []string {
	if platform = strings.TrimSpace(platform); platform != "" {
		return []string{platform}
	}
	return capability.ProviderPlatforms()
}
