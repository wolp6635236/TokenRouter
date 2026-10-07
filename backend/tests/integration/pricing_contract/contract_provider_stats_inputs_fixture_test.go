package pricingcontract

import (
	"context"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/billing/pricing"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/usage"
)

// applyContractProviderStatsCost resolves the provider stats cost for a usage log entry.
// It resolves the upstream model (falling back to the requested model) and calls
// the 4-level priority chain via contractProviderStatsCost.
func applyContractProviderStatsCost(
	ctx context.Context,
	usageLog *usage.UsageLog,
	cs *routing.PricingConfigService, bs *billing.Calculator,
	providerID int64, groupID int64,
	upstreamModel, requestedModel, groupMappedModel string,
	tokens pricing.UsageTokens,
	totalCost float64,
	resolvers ...*billing.PriceResolver,
) {
	model := upstreamModel
	if model == "" {
		model = requestedModel
	}
	requestCount := 1
	if usageLog != nil && usageLog.ImageCount > 0 {
		requestCount = usageLog.ImageCount
	}
	serviceTier := ""
	reasoningEffort := ""
	if usageLog != nil && usageLog.ServiceTier != nil {
		serviceTier = *usageLog.ServiceTier
	}
	if usageLog != nil && usageLog.ReasoningEffort != nil {
		reasoningEffort = *usageLog.ReasoningEffort
	}
	if len(resolvers) > 0 && resolvers[0] != nil {
		usageLog.ProviderStatsCost = resolvers[0].ResolveProviderStats(ctx, billing.ProviderStatsCostInput{PreferRequestedModel: usageLog.Platform == "qoder", ProviderID: providerID, GroupID: groupID, UpstreamModel: model, RequestedModel: requestedModel, MappedModel: groupMappedModel, Tokens: tokens, RequestCount: requestCount, ServiceTier: serviceTier, ReasoningEffort: reasoningEffort})
		return
	}
	usageLog.ProviderStatsCost = contractProviderStatsWithMapping(
		ctx, cs, bs, usageLog.Platform, providerID, groupID, model, requestedModel, groupMappedModel, tokens, requestCount, totalCost, serviceTier,
		reasoningEffort,
	)
}

// contractProviderStatsCost 计算独立提供商成本，先匹配自定义规则，再查询模型默认价。
// 无可用成本价时返回 nil，保留日志层的历史回退公式。
// Qoder 自定义规则依次按请求模型、分组映射模型和最终上游模型匹配。
// totalCost 仅作为测试输入，生产成本解析器不接收用户售价。
func contractProviderStatsCost(
	ctx context.Context,
	pricingConfigService *routing.PricingConfigService,
	billingService *billing.Calculator,
	actualPlatform string,
	providerID int64,
	groupID int64,
	upstreamModel string,
	requestedModel string,
	tokens pricing.UsageTokens,
	requestCount int,
	totalCost float64,
	serviceTier string,
	reasoningEfforts ...string,
) *float64 {
	return contractProviderStatsWithMapping(ctx, pricingConfigService, billingService, actualPlatform, providerID, groupID, upstreamModel, requestedModel, "", tokens, requestCount, totalCost, serviceTier, reasoningEfforts...)
}

// contractProviderStatsWithMapping 委托 billing 的唯一提供商统计规则。
func contractProviderStatsWithMapping(
	ctx context.Context,
	pricingConfigService *routing.PricingConfigService,
	billingService *billing.Calculator,
	actualPlatform string,
	providerID int64,
	groupID int64,
	upstreamModel string,
	requestedModel string,
	groupMappedModel string,
	tokens pricing.UsageTokens,
	requestCount int,
	totalCost float64,
	serviceTier string,
	reasoningEfforts ...string,
) *float64 {
	effort := ""
	if len(reasoningEfforts) > 0 {
		effort = reasoningEfforts[0]
	}
	var source billing.ProviderStatsSource
	if pricingConfigService != nil {
		source = gatewayprovider.ProviderStatsSource{Service: pricingConfigService}
	}
	var calculator *billing.Calculator
	if billingService != nil {
		calculator = billingService
	}
	resolver := billing.NewPriceResolver(nil, calculator, nil, nil, source)
	return resolver.ResolveProviderStats(ctx, billing.ProviderStatsCostInput{PreferRequestedModel: actualPlatform == "qoder", ProviderID: providerID, GroupID: groupID, UpstreamModel: upstreamModel, RequestedModel: requestedModel, MappedModel: groupMappedModel, Tokens: tokens, RequestCount: requestCount, ServiceTier: serviceTier, ReasoningEffort: effort})
}
