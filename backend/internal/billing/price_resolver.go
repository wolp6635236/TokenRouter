package billing

import (
	"context"
	"strings"

	purepricing "github.com/TokenFlux/TokenRouter/internal/billing/pricing"
)

const (
	PricingSourceCatalog  = purepricing.PricingSourceCatalog
	PricingSourceUnpriced = purepricing.PricingSourceUnpriced
)

// ResolvedPricing 保留旧解析结果入口，由纯定价包唯一拥有。
type ResolvedPricing = purepricing.ResolvedPricing

// PricingInput 定价解析输入。
type PricingInput struct {
	Model   string
	GroupID *int64 // nil 表示不检查共享价格配置
}

type ConfigPrices interface {
	GetEffectiveConfigModelPricing(context.Context, int64, string) *ModelPricingEntry
}
type ModelIdentity struct {
	Candidates []string
}
type ModelCandidates func(string) ModelIdentity

// PriceResolver 按分组关联读取共享价格配置，并按需查询模型目录。
type PriceResolver struct {
	pricingConfigs ConfigPrices
	calculator     *Calculator
	lookup         ModelCandidates
	observe        func(string, error)
	providerStats  ProviderStatsSource
}

func NewPriceResolver(pricingConfigs ConfigPrices, calculator *Calculator, lookup ModelCandidates, observe func(string, error), stats ...ProviderStatsSource) *PriceResolver {
	if observe == nil {
		observe = func(string, error) {}
	}
	var providerStats ProviderStatsSource
	if len(stats) > 0 {
		providerStats = stats[0]
	}
	return &PriceResolver{pricingConfigs: pricingConfigs, calculator: calculator, lookup: lookup, observe: observe, providerStats: providerStats}
}

// Resolve 按关联价格配置与内置价格解析价卡及计费设置。
// @project-doc docs/domains/routing_and_billing.md#group_model_pricing
func (r *PriceResolver) Resolve(ctx context.Context, input PricingInput) *ResolvedPricing {
	var configPricing *ModelPricingEntry
	if input.GroupID != nil && r.pricingConfigs != nil {
		configPricing = r.LookupConfigPricingNormalized(ctx, *input.GroupID, input.Model)
	}
	var base *ModelPricing
	source := PricingSourceUnpriced
	if purepricing.PriceCardNeedsBase(configPricing) {
		base, source = r.ResolveBasePricing(input.Model)
	}
	settings := r.BillingSettings(ctx, input.GroupID)
	return purepricing.ResolvePriceCards(configPricing, base, source, settings.LongContextPricingEnabled)
}

// ResolveBasePricing 从完整型号的目录与静态价项获取基础定价。
func (r *PriceResolver) ResolveBasePricing(model string) (*ModelPricing, string) {
	pricing, err := r.calculator.GetModelPricing(model)
	if err != nil {
		r.observe(model, err)
		return nil, PricingSourceUnpriced
	}
	return pricing, PricingSourceCatalog
}

// LookupConfigPricingNormalized 优先匹配原始请求，再复用目录的明确身份候选。
// 候选解析同一型号的协议资源路径，目录尚未收录的型号也可查询手动配置。
// @project-doc docs/interfaces/model_catalog_and_marketplace.md#model_catalog_metadata_lookup
func (r *PriceResolver) LookupConfigPricingNormalized(ctx context.Context, groupID int64, model string) *ModelPricingEntry {
	if r == nil || r.pricingConfigs == nil {
		return nil
	}
	return LookupPricingForModel(model, func(candidate string) *ModelPricingEntry {
		return r.pricingConfigs.GetEffectiveConfigModelPricing(ctx, groupID, candidate)
	}, r.lookup)
}

// LookupPricingForModel 统一共享价格配置的候选顺序，完整请求名的精确/通配价卡优先。
func LookupPricingForModel(model string, lookup func(string) *ModelPricingEntry, identities ModelCandidates) *ModelPricingEntry {
	if pricing := lookup(model); pricing != nil {
		return pricing
	}
	projection := ModelIdentity{}
	if identities != nil {
		projection = identities(model)
	}
	candidates := projection.Candidates
	for _, candidate := range candidates {
		if strings.EqualFold(candidate, strings.TrimSpace(model)) {
			continue
		}
		if pricing := lookup(candidate); pricing != nil {
			return pricing
		}
	}
	return nil
}

// BillingSettings 读取分组关联的有效配置，无关联时使用统一默认值。
func (r *PriceResolver) BillingSettings(ctx context.Context, groupID *int64) purepricing.BillingSettings {
	if r != nil && groupID != nil {
		if source, ok := r.pricingConfigs.(interface {
			GetEffectiveBillingSettings(context.Context, int64) purepricing.BillingSettings
		}); ok {
			return source.GetEffectiveBillingSettings(ctx, *groupID).Clone()
		}
	}
	return purepricing.DefaultBillingSettings()
}
