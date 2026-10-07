package pricing

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/TokenFlux/TokenRouter/internal/protocol"
)

func NormalizeBillingServiceTier(serviceTier string) string {
	return strings.ToLower(strings.TrimSpace(serviceTier))
}

func UsePriorityServiceTierPricing(serviceTier string, pricing *ModelPricing) bool {
	if pricing == nil {
		return false
	}
	tier := NormalizeBillingServiceTier(serviceTier)
	if tier != "priority" && tier != "fast" {
		return false
	}
	if pricing.FastModeMultiplier != nil || pricing.FastMultiplier != nil {
		return false
	}
	return pricing.PriorityInputPresent || pricing.PriorityOutputPresent || pricing.PriorityCacheReadPresent || pricing.PriorityCacheWritePresent || pricing.InputPricePerTokenPriority > 0 || pricing.OutputPricePerTokenPriority > 0 ||
		pricing.CacheCreationPricePerTokenPriority > 0 || pricing.CacheReadPricePerTokenPriority > 0
}

// NormalizedFastModeMultiplier 返回价卡 Fast 倍率；负值按 0 防御处理。
func NormalizedFastModeMultiplier(pricing *ModelPricing) (float64, bool) {
	if pricing == nil {
		return 1, false
	}
	configured := pricing.FastModeMultiplier
	if configured == nil {
		configured = pricing.FastMultiplier
	}
	if configured == nil {
		return 1, false
	}
	if *configured < 0 {
		return 0, true
	}
	return *configured, true
}

// ConfiguredServiceTierMultiplier 返回价卡显式层级倍率；未配置时沿用官方默认倍率。
func ConfiguredServiceTierMultiplier(serviceTier string, pricing *ModelPricing) float64 {
	if pricing != nil {
		switch NormalizeBillingServiceTier(serviceTier) {
		case "priority", "fast":
			if multiplier, configured := NormalizedFastModeMultiplier(pricing); configured {
				return multiplier
			}
		case "flex":
			if pricing.FlexMultiplier != nil {
				return *pricing.FlexMultiplier
			}
		}
	}
	return 1
}

// ApplyConfigFastModeMultiplier 将价卡 Fast 倍率写入最终定价元数据。
func ApplyConfigFastModeMultiplier(pricing *ModelPricing, ConfigPricing *ModelPricingEntry) {
	if pricing == nil || ConfigPricing == nil {
		return
	}
	multiplierPtr := ConfigPricing.FastMultiplier
	if multiplierPtr == nil {
		multiplierPtr = ConfigPricing.FastModeMultiplier
	}
	if multiplierPtr == nil {
		return
	}
	multiplier := *multiplierPtr
	if multiplier < 0 {
		multiplier = 0
	}
	pricing.FastModeMultiplier = &multiplier
	pricing.FastMultiplier = &multiplier
}

func ApplyConfigFlexMultiplier(pricing *ModelPricing, ConfigPricing *ModelPricingEntry) {
	if pricing == nil || ConfigPricing == nil || ConfigPricing.FlexMultiplier == nil {
		return
	}
	multiplier := *ConfigPricing.FlexMultiplier
	if multiplier < 0 {
		multiplier = 0
	}
	pricing.FlexMultiplier = &multiplier
}

// ApplyCostBreakdownMultiplier 将价卡分时倍率应用到所有 token 费用桶。
func ApplyCostBreakdownMultiplier(cost *CostBreakdown, multiplier float64) {
	if cost == nil || multiplier == 1 {
		return
	}
	cost.InputCost *= multiplier
	cost.ImageInputCost *= multiplier
	cost.OutputCost *= multiplier
	cost.ImageOutputCost *= multiplier
	cost.CacheCreationCost *= multiplier
	cost.CacheReadCost *= multiplier
	cost.TotalCost *= multiplier
	cost.ActualCost *= multiplier
}

// MaxReasoningEffortBillingMultiplier 返回 max 档位的模型/价卡倍率。
func MaxReasoningEffortBillingMultiplier(model, effort string, pricing *ModelPricing) float64 {
	if protocol.NormalizeClaudeOutputEffort(effort) == nil || !strings.EqualFold(strings.TrimSpace(effort), "max") {
		return 1
	}
	if pricing != nil && pricing.MaxReasoningEffortMultiplier != nil && *pricing.MaxReasoningEffortMultiplier > 0 {
		return *pricing.MaxReasoningEffortMultiplier
	}
	return 1
}

// ErrModelPricingUnavailable 表示当前所有定价来源都无法为请求模型提供价格。
var ErrModelPricingUnavailable = errors.New("pricing not found")

// ConfigTierOverridePrice 根据模型目录中的层级比例推导价卡层级价格。
// 价卡覆盖普通价时，priority/Fast 价格仍按对应服务层级计算。
func ConfigTierOverridePrice(baseStandard, baseTier, configStandard float64) float64 {
	if baseStandard > 0 && baseTier > 0 {
		return configStandard * (baseTier / baseStandard)
	}
	return 0
}

// ApplyConfigTokenPriceOverrides 应用普通与图片 token 价格，同时保留模型内置层级比例。
func ApplyConfigTokenPriceOverrides(pricing *ModelPricing, ConfigPricing *ModelPricingEntry) {
	if pricing == nil || ConfigPricing == nil {
		return
	}
	// 显式价卡覆盖同时应用到每个目录阶梯，保留未覆盖的独立单价。
	if len(pricing.ContextPrices) > 0 {
		tiers := make([]ContextModelPrice, len(pricing.ContextPrices))
		for i, tier := range pricing.ContextPrices {
			value := *tier.Pricing
			ApplyConfigTokenPriceOverrides(&value, ConfigPricing)
			tiers[i] = ContextModelPrice{Threshold: tier.Threshold, Pricing: &value}
		}
		pricing.ContextPrices = tiers
	}
	if ConfigPricing.InputPrice != nil {
		priority := ConfigTierOverridePrice(pricing.InputPricePerToken, pricing.InputPricePerTokenPriority, *ConfigPricing.InputPrice)
		pricing.InputPricePerToken = *ConfigPricing.InputPrice
		pricing.InputPricePerTokenPriority = priority
	}
	if ConfigPricing.OutputPrice != nil {
		priority := ConfigTierOverridePrice(pricing.OutputPricePerToken, pricing.OutputPricePerTokenPriority, *ConfigPricing.OutputPrice)
		pricing.OutputPricePerToken = *ConfigPricing.OutputPrice
		pricing.OutputPricePerTokenPriority = priority
	}
	if ConfigPricing.CacheWritePrice != nil {
		basePriority := pricing.CacheCreationPricePerTokenPriority
		priority := ConfigTierOverridePrice(pricing.CacheCreationPricePerToken, basePriority, *ConfigPricing.CacheWritePrice)
		pricing.CacheCreationPricePerToken = *ConfigPricing.CacheWritePrice
		pricing.CacheCreationPricePerTokenPriority = priority
		pricing.CacheCreationPriceExplicit = true
		pricing.CacheCreation5mPrice = *ConfigPricing.CacheWritePrice
		if ConfigPricing.CacheWrite1hPrice == nil {
			// 兼容旧配置：未拆分时继续让 cache_write_price 覆盖两个 TTL 档位。
			pricing.CacheCreation1hPrice = *ConfigPricing.CacheWritePrice
		}
	}
	if ConfigPricing.CacheWrite1hPrice != nil {
		pricing.CacheCreation1hPrice = *ConfigPricing.CacheWrite1hPrice
		pricing.SupportsCacheBreakdown = true
	}
	if ConfigPricing.CacheReadPrice != nil {
		priority := ConfigTierOverridePrice(pricing.CacheReadPricePerToken, pricing.CacheReadPricePerTokenPriority, *ConfigPricing.CacheReadPrice)
		pricing.CacheReadPricePerToken = *ConfigPricing.CacheReadPrice
		pricing.CacheReadPricePerTokenPriority = priority
	}
	applyConfigImagePriceOverrides(pricing, ConfigPricing)
}

// CalculateTokenCost 按 token 区间计费
func CalculateTokenCost(resolved *ResolvedPricing, input CostInput) (*CostBreakdown, error) {
	totalContext := input.Tokens.InputTokens + input.Tokens.CacheCreationTokens + input.Tokens.CacheReadTokens

	pricing := GetIntervalPricing(resolved, totalContext)
	if pricing == nil {
		return nil, fmt.Errorf("no pricing available for model: %s: %w", input.Model, ErrModelPricingUnavailable)
	}

	// 长上下文定价仅在无区间定价且分组允许时应用（区间定价已包含上下文分层）。
	applyLongCtx := len(resolved.Intervals) == 0 && resolved.LongContextPricingEnabled

	breakdown := ComputeTokenBreakdown(pricing, input.Tokens, input.RateMultiplier, input.ServiceTier, applyLongCtx)
	if resolved.Source == PricingSourceCatalog && pricing.TimePricing != nil {
		ApplyCostBreakdownMultiplier(breakdown, pricing.TimePricing.MultiplierAt(input.ModelPricingAt, input.ModelTimeLocation))
	}
	ApplyCostBreakdownMultiplier(breakdown, ResolvedTimeMultiplier(resolved, input.PricingAt, input.TimePricingLocation))
	ApplyCostBreakdownMultiplier(breakdown, MaxReasoningEffortBillingMultiplier(input.Model, input.ReasoningEffort, pricing))
	return breakdown, nil
}

// ComputeTokenBreakdown 是 token 计费的核心逻辑，由 CalculateTokenCost 和 calculateCostInternal 共用。
// applyLongCtx 控制是否检查长上下文定价（区间定价已自含上下文分层，不需要额外应用）。
func ComputeTokenBreakdown(
	pricing *ModelPricing, tokens UsageTokens,
	rateMultiplier float64, serviceTier string,
	applyLongCtx bool,
) *CostBreakdown {
	if applyLongCtx && len(pricing.ContextPrices) > 0 {
		input := tokens.InputTokens + tokens.CacheReadTokens + tokens.CacheCreationTokens
		selected := pricing
		for _, tier := range pricing.ContextPrices {
			if input > tier.Threshold {
				selected = tier.Pricing
			}
		}
		if selected != pricing {
			value := *selected
			value.FastModeMultiplier = pricing.FastModeMultiplier
			value.FastMultiplier = pricing.FastMultiplier
			value.FlexMultiplier = pricing.FlexMultiplier
			value.MaxReasoningEffortMultiplier = pricing.MaxReasoningEffortMultiplier
			cost := ComputeTokenBreakdown(&value, tokens, rateMultiplier, serviceTier, false)
			cost.LongContextBillingApplied = cost.ActualCost > ComputeTokenBreakdown(pricing, tokens, rateMultiplier, serviceTier, false).ActualCost
			return cost
		}
	}
	// 保存时强制 > 0；若仍有负数泄漏，按 0 处理避免按 1x 误扣。
	if rateMultiplier < 0 {
		rateMultiplier = 0
	}

	inputPrice := pricing.InputPricePerToken
	outputPrice := pricing.OutputPricePerToken
	cacheReadPrice := pricing.CacheReadPricePerToken
	cacheCreationPrice := pricing.CacheCreationPricePerToken
	cacheCreationMultiplier := 1.0
	tierMultiplier := 1.0

	tier := NormalizeBillingServiceTier(serviceTier)
	if tier == "priority" || tier == "fast" {
		if fastMultiplier, configured := NormalizedFastModeMultiplier(pricing); configured {
			// 价卡显式倍率以普通模式最终价为基准，避免和模型内置 priority 价重复叠乘。
			tierMultiplier = fastMultiplier
		} else if UsePriorityServiceTierPricing(serviceTier, pricing) {
			if pricing.InputPricePerTokenPriority > 0 || pricing.PriorityInputPresent {
				inputPrice = pricing.InputPricePerTokenPriority
			}
			if pricing.OutputPricePerTokenPriority > 0 || pricing.PriorityOutputPresent {
				outputPrice = pricing.OutputPricePerTokenPriority
			}
			if pricing.CacheReadPricePerTokenPriority > 0 || pricing.PriorityCacheReadPresent {
				cacheReadPrice = pricing.CacheReadPricePerTokenPriority
			}
			if pricing.CacheCreationPricePerTokenPriority > 0 || pricing.PriorityCacheWritePresent {
				cacheCreationPrice = pricing.CacheCreationPricePerTokenPriority
			}
		} else {
			tierMultiplier = ConfiguredServiceTierMultiplier(serviceTier, pricing)
		}
	} else {
		tierMultiplier = ConfiguredServiceTierMultiplier(serviceTier, pricing)
	}

	longContextPricingEligible := applyLongCtx && ShouldApplySessionLongContextPricing(tokens, pricing)
	var baselineCost *CostBreakdown
	if longContextPricingEligible {
		baselineCost = ComputeTokenBreakdown(pricing, tokens, rateMultiplier, serviceTier, false)
		// 未配置的一侧倍率按 1 计，避免部分覆盖条目把对应分项算成免费。
		longContextInputMultiplier := LongContextMultiplierOrOne(pricing.LongContextInputMultiplier)
		inputPrice *= longContextInputMultiplier
		outputPrice *= LongContextMultiplierOrOne(pricing.LongContextOutputMultiplier)
		// 缓存读取本质上是输入侧的复用，应与 input 一同应用长上下文倍率；
		// 否则 cache hit 越多，少计的费用越多（见 #2293）。
		cacheReadPrice *= longContextInputMultiplier
		// 缓存创建（cache_write）也是输入侧操作，三档价格（标准 / 5m / 1h）
		// 都通过 ComputeCacheCreationCost 直接读取 pricing.*，不会经过这里
		// 的倍率修改，因此显式向下传一个倍率，避免长上下文场景下被漏乘。
		cacheCreationMultiplier = longContextInputMultiplier
	}

	bd := &CostBreakdown{}
	// 分离图片输入 token 与文本输入 token（多模态 embedding、图片编辑等图文不同价场景）。
	// InputCost 仅计文本输入，图片输入费用单独记入 ImageInputCost，便于对账；总额不变。
	// ImageInputTokens 为 0 时（绝大多数 chat/vision 流量）走原始单价路径，行为不变。
	if tokens.ImageInputTokens > 0 {
		imageInputTokens := tokens.ImageInputTokens
		textInputTokens := tokens.InputTokens - imageInputTokens
		if textInputTokens < 0 {
			textInputTokens = 0
			imageInputTokens = tokens.InputTokens
		}
		imageInputPrice := pricing.ImageInputPricePerToken
		if imageInputPrice == 0 {
			imageInputPrice = inputPrice
		}
		bd.InputCost = float64(textInputTokens) * inputPrice
		bd.ImageInputCost = float64(imageInputTokens) * imageInputPrice
	} else {
		bd.InputCost = float64(tokens.InputTokens) * inputPrice
	}

	// 分离图片输出 token 与文本输出 token
	textOutputTokens := tokens.OutputTokens - tokens.ImageOutputTokens
	if textOutputTokens < 0 {
		textOutputTokens = 0
	}
	bd.OutputCost = float64(textOutputTokens) * outputPrice

	// 图片输出 token 费用（独立费率）
	if tokens.ImageOutputTokens > 0 {
		imgPrice := pricing.ImageOutputPricePerToken
		if imgPrice == 0 && !pricing.ImageOutputPriceExplicit {
			imgPrice = outputPrice
		}
		bd.ImageOutputCost = float64(tokens.ImageOutputTokens) * imgPrice
	}

	// 明确的 Fast 缓存写入价同样缩放 TTL 单价，来源标签不参与收费判断。
	if UsePriorityServiceTierPricing(serviceTier, pricing) && pricing.SupportsCacheBreakdown && pricing.CacheCreationPricePerToken > 0 && pricing.PriorityCacheWritePresent {
		cacheCreationMultiplier *= cacheCreationPrice / pricing.CacheCreationPricePerToken
	}
	// 缓存创建费用
	bd.CacheCreationCost = ComputeCacheCreationCost(pricing, tokens, cacheCreationPrice, cacheCreationMultiplier)

	bd.CacheReadCost = float64(tokens.CacheReadTokens) * cacheReadPrice

	if tierMultiplier != 1.0 {
		bd.InputCost *= tierMultiplier
		bd.ImageInputCost *= tierMultiplier
		bd.OutputCost *= tierMultiplier
		bd.ImageOutputCost *= tierMultiplier
		bd.CacheCreationCost *= tierMultiplier
		bd.CacheReadCost *= tierMultiplier
	}

	bd.TotalCost = bd.InputCost + bd.ImageInputCost + bd.OutputCost + bd.ImageOutputCost +
		bd.CacheCreationCost + bd.CacheReadCost
	bd.ActualCost = bd.TotalCost * rateMultiplier
	bd.LongContextBillingApplied = baselineCost != nil && bd.ActualCost > baselineCost.ActualCost

	return bd
}

// ComputeCacheCreationCost 计算缓存创建费用（支持 5m/1h 分类或标准计费）。
// multiplier 用于长上下文等场景下的整体价格缩放（普通调用传 1.0 即可）。
func ComputeCacheCreationCost(pricing *ModelPricing, tokens UsageTokens, price, multiplier float64) float64 {
	if pricing.SupportsCacheBreakdown && (pricing.CacheCreation5mPrice > 0 || pricing.CacheCreation1hPrice > 0) {
		cacheCreation5mTokens, cacheCreation1hTokens := NormalizeCacheCreationBreakdown(tokens)
		if cacheCreation5mTokens == 0 && cacheCreation1hTokens == 0 && tokens.CacheCreationTokens > 0 {
			// API 未返回 ephemeral 明细，回退到全部按 5m 单价计费
			return float64(tokens.CacheCreationTokens) * pricing.CacheCreation5mPrice * multiplier
		}
		return float64(cacheCreation5mTokens)*pricing.CacheCreation5mPrice*multiplier +
			float64(cacheCreation1hTokens)*pricing.CacheCreation1hPrice*multiplier
	}
	return float64(tokens.CacheCreationTokens) * price * multiplier
}

// NormalizeCacheCreationBreakdown 在聚合值为正且 TTL 明细相互矛盾时封顶明细，
// 并在整数 token 约束下尽量保留上游报告的比例。
func NormalizeCacheCreationBreakdown(tokens UsageTokens) (int, int) {
	cacheCreation5mTokens := tokens.CacheCreation5mTokens
	cacheCreation1hTokens := tokens.CacheCreation1hTokens
	aggregate := tokens.CacheCreationTokens
	if cacheCreation5mTokens < 0 {
		cacheCreation5mTokens = 0
	}
	if cacheCreation1hTokens < 0 {
		cacheCreation1hTokens = 0
	}
	if aggregate <= 0 || (cacheCreation5mTokens <= aggregate && cacheCreation1hTokens <= aggregate-cacheCreation5mTokens) {
		return cacheCreation5mTokens, cacheCreation1hTokens
	}

	detailTotal := float64(cacheCreation5mTokens) + float64(cacheCreation1hTokens)
	normalized5mTokens := math.Round(float64(aggregate) * float64(cacheCreation5mTokens) / detailTotal)
	if normalized5mTokens >= float64(aggregate) {
		cacheCreation5mTokens = aggregate
	} else {
		cacheCreation5mTokens = int(normalized5mTokens)
	}
	return cacheCreation5mTokens, aggregate - cacheCreation5mTokens
}

// CalculatePerRequestCost 按次/图片计费
func CalculatePerRequestCost(resolved *ResolvedPricing, input CostInput) (*CostBreakdown, error) {
	units := input.UsageUnits
	if units <= 0 {
		count := input.RequestCount
		if count <= 0 {
			count = 1
		}
		units = float64(count)
	}

	totalContext := input.Tokens.InputTokens + input.Tokens.CacheCreationTokens + input.Tokens.CacheReadTokens
	unitPrice, found := ResolveRequestUnitPrice(resolved, input.SizeTier, &totalContext)
	if !found {
		return nil, fmt.Errorf("%w for request price: %s", ErrModelPricingUnavailable, input.Model)
	}

	totalCost := unitPrice * units
	actualCost := totalCost * input.RateMultiplier

	return &CostBreakdown{
		TotalCost:  totalCost,
		ActualCost: actualCost,
	}, nil
}

// LongContextMultiplierOrOne 将未配置的长上下文倍率归一为 1。
func LongContextMultiplierOrOne(multiplier float64) float64 {
	if multiplier <= 0 {
		return 1
	}
	return multiplier
}

func ShouldApplySessionLongContextPricing(tokens UsageTokens, pricing *ModelPricing) bool {
	if pricing == nil || pricing.LongContextInputThreshold <= 0 {
		return false
	}
	if pricing.LongContextInputMultiplier <= 1 && pricing.LongContextOutputMultiplier <= 1 {
		return false
	}
	totalInputTokens := tokens.InputTokens + tokens.CacheCreationTokens + tokens.CacheReadTokens
	if pricing.LongContextThresholdInclusive {
		return totalInputTokens >= pricing.LongContextInputThreshold
	}
	return totalInputTokens > pricing.LongContextInputThreshold
}

// DisplayPricingFromResolved 将已解析的计费配置转换成模型广场展示价格。
func DisplayPricingFromResolved(model string, rateMultiplier float64, resolved *ResolvedPricing) (ModelDisplayPricing, bool) {
	if resolved == nil {
		return ModelDisplayPricing{}, false
	}

	switch resolved.Mode {
	case BillingModeToken:
		pricing := ResolvedDisplayTokenPricing(resolved)
		if pricing != nil && !resolved.LongContextPricingEnabled {
			pricing = WithoutLongContextDisplayPricing(pricing)
		}
		if pricing != nil && (HasAnyDisplayTokenPricing(pricing) || resolved.HasEffectiveOverridePricing()) {
			return withTokenDisplayModifiers(BuildTokenDisplayPricing(pricing, rateMultiplier), resolved), true
		}
		intervals := ResolvedDisplayPricingIntervals(resolved, rateMultiplier)
		if len(intervals) > 0 {
			return withTokenDisplayModifiers(BuildTokenIntervalDisplayPricing(intervals), resolved), true
		}
		return ModelDisplayPricing{}, false
	case BillingModeImage, BillingModePerRequest:
		if resolved.Source != PricingSourceConfig {
			return ModelDisplayPricing{}, false
		}
		if resolved.Mode == BillingModePerRequest && !LooksLikeImageModel(model) {
			return ModelDisplayPricing{}, false
		}
		result := ModelDisplayPricing{PricingMode: "image", PriceStatus: "priced"}
		prices := []*float64{&result.ImagePrice1K, &result.ImagePrice2K, &result.ImagePrice4K}
		for i, size := range []string{"1K", "2K", "4K"} {
			value, found := ConfiguredImageUnitPrice(resolved, size)
			if !found {
				continue
			}
			*prices[i] = value * rateMultiplier
			result.ImagePriceSizes = append(result.ImagePriceSizes, size)
		}
		return result, len(result.ImagePriceSizes) > 0
	default:
		return ModelDisplayPricing{}, false
	}
}

// withTokenDisplayModifiers 附上结算时按推理档位和请求时刻生效的 token 倍率。
// 倍率为 1、分时规则为空或校验失败时，对应字段留空，和结算按 1x 处理的结果一致。
func withTokenDisplayModifiers(display ModelDisplayPricing, resolved *ResolvedPricing) ModelDisplayPricing {
	if resolved.BasePricing != nil {
		multiplier := resolved.BasePricing.MaxReasoningEffortMultiplier
		if multiplier != nil && *multiplier > 0 && *multiplier != 1 {
			value := *multiplier
			display.MaxReasoningEffortMultiplier = &value
		}
	}
	if resolved.ConfigPricing != nil {
		config := resolved.ConfigPricing.TimePricing
		if config != nil && len(config.Periods) > 0 && ValidateTimePricingConfig(config) == nil {
			display.TimePricing = resolved.ConfigPricing.Clone().TimePricing
		}
	}
	return display
}

// WithoutLongContextDisplayPricing 移除内置长上下文展示元数据，保留基础单价。
func WithoutLongContextDisplayPricing(pricing *ModelPricing) *ModelPricing {
	if pricing == nil {
		return nil
	}
	cloned := *pricing
	cloned.LongContextInputThreshold = 0
	cloned.ContextPrices = nil
	cloned.LongContextInputMultiplier = 0
	cloned.LongContextOutputMultiplier = 0
	return &cloned
}

// ResolvedTokenPriceRanges 补齐显式区间前后与中间的默认价范围，不修改原价卡。
func ResolvedTokenPriceRanges(resolved *ResolvedPricing) []ResolvedTokenPriceRange {
	if resolved == nil || len(resolved.Intervals) == 0 {
		return nil
	}
	intervals := append([]PricingInterval(nil), resolved.Intervals...)
	sort.Slice(intervals, func(i, j int) bool { return intervals[i].MinTokens < intervals[j].MinTokens })
	ranges := make([]ResolvedTokenPriceRange, 0, len(intervals)*2+1)
	cursor := 0
	for i := range intervals {
		interval := &intervals[i]
		if interval.MinTokens > cursor {
			maxTokens := interval.MinTokens
			ranges = append(ranges, ResolvedTokenPriceRange{cursor, &maxTokens, WithoutLongContextDisplayPricing(resolved.tokenPricingForInterval(nil))})
		}
		ranges = append(ranges, ResolvedTokenPriceRange{interval.MinTokens, interval.MaxTokens, resolved.tokenPricingForInterval(interval)})
		if interval.MaxTokens == nil {
			return ranges
		}
		cursor = *interval.MaxTokens
	}
	return append(ranges, ResolvedTokenPriceRange{cursor, nil, WithoutLongContextDisplayPricing(resolved.tokenPricingForInterval(nil))})
}

func ResolvedDisplayTokenPricing(resolved *ResolvedPricing) *ModelPricing {
	if resolved == nil {
		return nil
	}
	if len(resolved.Intervals) == 0 {
		return resolved.tokenPricingForInterval(nil)
	}
	ranges := ResolvedTokenPriceRanges(resolved)
	pricing := ranges[0].pricing
	// 缺价范围或不同价格不能压平成一个覆盖所有上下文的单价。
	for _, priceRange := range ranges {
		if priceRange.pricing == nil || !SameDisplayTokenPricing(pricing, priceRange.pricing) {
			return nil
		}
	}
	return pricing
}

// ResolvedDisplayPricingIntervals 展示全部有定价的范围，缺价范围不伪装成免费。
func ResolvedDisplayPricingIntervals(resolved *ResolvedPricing, rateMultiplier float64) []ModelDisplayPricingInterval {
	var intervals []ModelDisplayPricingInterval
	for _, priceRange := range ResolvedTokenPriceRanges(resolved) {
		if priceRange.pricing != nil {
			intervals = append(intervals, ModelPricingDisplayInterval(priceRange.minTokens, priceRange.maxTokens, priceRange.pricing, rateMultiplier))
		}
	}
	return intervals
}

func PricingIntervalHasEffectiveTokenPricing(interval PricingInterval) bool {
	return interval.InputPrice != nil ||
		interval.OutputPrice != nil ||
		interval.CacheWritePrice != nil ||
		interval.CacheWrite1hPrice != nil ||
		interval.CacheReadPrice != nil
}

func SameDisplayTokenPricing(a *ModelPricing, b *ModelPricing) bool {
	if a == nil || b == nil {
		return a == b
	}
	// 普通价和 Fast 价都相同才能合并，避免默认段与显式段的服务层级差异被隐藏。
	return ModelPricingDisplayInterval(0, nil, a, 1) == ModelPricingDisplayInterval(0, nil, b, 1)
}

func BuildTokenDisplayPricing(pricing *ModelPricing, rateMultiplier float64) ModelDisplayPricing {
	if intervals := LongContextDisplayPricingIntervals(pricing, rateMultiplier); len(intervals) > 0 {
		return BuildTokenIntervalDisplayPricing(intervals)
	}

	cacheWritePrice, cacheWrite1hPrice := CacheCreationDisplayPrices(pricing)
	displayPricing := ModelDisplayPricing{
		PricingMode:               "token",
		PriceStatus:               "priced",
		InputPricePerToken:        pricing.InputPricePerToken * rateMultiplier,
		ImageInputPricePerToken:   pricing.ImageInputPricePerToken * rateMultiplier,
		OutputPricePerToken:       pricing.OutputPricePerToken * rateMultiplier,
		CacheWritePricePerToken:   cacheWritePrice * rateMultiplier,
		CacheWrite1hPricePerToken: cacheWrite1hPrice * rateMultiplier,
		CacheReadPricePerToken:    pricing.CacheReadPricePerToken * rateMultiplier,
		ImageOutputPricePerToken:  pricing.ImageOutputPricePerToken * rateMultiplier,
	}
	if fastPricing, ok := FastModeDisplayPricing(pricing); ok {
		displayPricing.FastInputPricePerToken = fastPricing.InputPricePerToken * rateMultiplier
		displayPricing.FastImageInputPricePerToken = fastPricing.ImageInputPricePerToken * rateMultiplier
		displayPricing.FastOutputPricePerToken = fastPricing.OutputPricePerToken * rateMultiplier
		fastCacheWritePrice, fastCacheWrite1hPrice := CacheCreationDisplayPrices(fastPricing)
		displayPricing.FastCacheWritePricePerToken = fastCacheWritePrice * rateMultiplier
		displayPricing.FastCacheWrite1hPricePerToken = fastCacheWrite1hPrice * rateMultiplier
		displayPricing.FastCacheReadPricePerToken = fastPricing.CacheReadPricePerToken * rateMultiplier
		displayPricing.FastImageOutputPricePerToken = fastPricing.ImageOutputPricePerToken * rateMultiplier
	}
	return displayPricing
}

// CacheCreationDisplayPrices 返回展示用的 5m/1h 缓存写入单价。
// 未启用 TTL 明细时只返回兼容旧配置的聚合单价。
func CacheCreationDisplayPrices(pricing *ModelPricing) (float64, float64) {
	if pricing == nil {
		return 0, 0
	}
	short := pricing.CacheCreationPricePerToken
	if pricing.SupportsCacheBreakdown && pricing.CacheCreation5mPrice > 0 {
		short = pricing.CacheCreation5mPrice
	}
	if !pricing.SupportsCacheBreakdown {
		return short, 0
	}
	return short, pricing.CacheCreation1hPrice
}

// LongContextDisplayPricingIntervals 将内置长上下文倍率转换成模型广场可展示的两段价格。
func LongContextDisplayPricingIntervals(pricing *ModelPricing, rateMultiplier float64) []ModelDisplayPricingInterval {
	if pricing != nil && len(pricing.ContextPrices) > 0 {
		var intervals []ModelDisplayPricingInterval
		start := 0
		current := pricing
		for _, tier := range pricing.ContextPrices {
			end := tier.Threshold
			intervals = append(intervals, ModelPricingDisplayInterval(start, &end, current, rateMultiplier))
			next := *tier.Pricing
			next.FastModeMultiplier = pricing.FastModeMultiplier
			next.FastMultiplier = pricing.FastMultiplier
			next.FlexMultiplier = pricing.FlexMultiplier
			next.MaxReasoningEffortMultiplier = pricing.MaxReasoningEffortMultiplier
			start, current = end, &next
		}
		return append(intervals, ModelPricingDisplayInterval(start, nil, current, rateMultiplier))
	}
	if !HasLongContextDisplayPricing(pricing) {
		return nil
	}

	maxTokens := pricing.LongContextInputThreshold
	baseInterval := ModelPricingDisplayInterval(0, &maxTokens, pricing, rateMultiplier)

	longContextPricing := ApplyLongContextDisplayMultipliers(pricing)
	longContextInterval := ModelPricingDisplayInterval(pricing.LongContextInputThreshold, nil, longContextPricing, rateMultiplier)

	return []ModelDisplayPricingInterval{baseInterval, longContextInterval}
}

// ApplyLongContextDisplayMultipliers 按结算规则生成长上下文展示价格，不修改原始模型定价。
// 缓存创建与读取都属于输入侧，普通价、priority 价及缓存时长明细必须使用同一输入倍率。
func ApplyLongContextDisplayMultipliers(pricing *ModelPricing) *ModelPricing {
	if pricing == nil {
		return nil
	}
	adjusted := *pricing
	// 与结算计算一致，覆盖文件省略的单侧倍率按 1x 展示。
	inputMultiplier := LongContextMultiplierOrOne(pricing.LongContextInputMultiplier)
	outputMultiplier := LongContextMultiplierOrOne(pricing.LongContextOutputMultiplier)
	adjusted.InputPricePerToken *= inputMultiplier
	adjusted.InputPricePerTokenPriority *= inputMultiplier
	adjusted.OutputPricePerToken *= outputMultiplier
	adjusted.OutputPricePerTokenPriority *= outputMultiplier
	adjusted.CacheCreationPricePerToken *= inputMultiplier
	adjusted.CacheCreationPricePerTokenPriority *= inputMultiplier
	adjusted.CacheCreation5mPrice *= inputMultiplier
	adjusted.CacheCreation1hPrice *= inputMultiplier
	adjusted.CacheReadPricePerToken *= inputMultiplier
	adjusted.CacheReadPricePerTokenPriority *= inputMultiplier
	return &adjusted
}

func HasLongContextDisplayPricing(pricing *ModelPricing) bool {
	return HasAnyDisplayTokenPricing(pricing) &&
		pricing.LongContextInputThreshold > 0 &&
		(pricing.LongContextInputMultiplier > 1 || pricing.LongContextOutputMultiplier > 1)
}

func ModelPricingDisplayInterval(minTokens int, maxTokens *int, pricing *ModelPricing, rateMultiplier float64) ModelDisplayPricingInterval {
	cacheWritePrice, cacheWrite1hPrice := CacheCreationDisplayPrices(pricing)
	interval := ModelDisplayPricingInterval{
		MinTokens:                 minTokens,
		MaxTokens:                 maxTokens,
		InputPricePerToken:        pricing.InputPricePerToken * rateMultiplier,
		ImageInputPricePerToken:   pricing.ImageInputPricePerToken * rateMultiplier,
		OutputPricePerToken:       pricing.OutputPricePerToken * rateMultiplier,
		CacheWritePricePerToken:   cacheWritePrice * rateMultiplier,
		CacheWrite1hPricePerToken: cacheWrite1hPrice * rateMultiplier,
		CacheReadPricePerToken:    pricing.CacheReadPricePerToken * rateMultiplier,
		ImageOutputPricePerToken:  pricing.ImageOutputPricePerToken * rateMultiplier,
	}
	if fastPricing, ok := FastModeDisplayPricing(pricing); ok {
		interval.FastInputPricePerToken = fastPricing.InputPricePerToken * rateMultiplier
		interval.FastImageInputPricePerToken = fastPricing.ImageInputPricePerToken * rateMultiplier
		interval.FastOutputPricePerToken = fastPricing.OutputPricePerToken * rateMultiplier
		fastCacheWritePrice, fastCacheWrite1hPrice := CacheCreationDisplayPrices(fastPricing)
		interval.FastCacheWritePricePerToken = fastCacheWritePrice * rateMultiplier
		interval.FastCacheWrite1hPricePerToken = fastCacheWrite1hPrice * rateMultiplier
		interval.FastCacheReadPricePerToken = fastPricing.CacheReadPricePerToken * rateMultiplier
		interval.FastImageOutputPricePerToken = fastPricing.ImageOutputPricePerToken * rateMultiplier
	}
	return interval
}

func FastModeDisplayPricing(pricing *ModelPricing) (*ModelPricing, bool) {
	if !HasFastModeDisplayPricing(pricing) {
		return nil, false
	}
	if multiplier, configured := NormalizedFastModeMultiplier(pricing); configured {
		fastPricing := MultiplyModelPricing(pricing, multiplier)
		fastPricing.FastModeMultiplier = nil
		return fastPricing, true
	}

	fastPricing := *pricing
	if UsePriorityServiceTierPricing(OpenAIFastTierPriority, pricing) {
		if pricing.InputPricePerTokenPriority > 0 || pricing.PriorityInputPresent {
			fastPricing.InputPricePerToken = pricing.InputPricePerTokenPriority
		}
		if pricing.OutputPricePerTokenPriority > 0 || pricing.PriorityOutputPresent {
			fastPricing.OutputPricePerToken = pricing.OutputPricePerTokenPriority
		}
		if pricing.CacheCreationPricePerTokenPriority > 0 || pricing.PriorityCacheWritePresent {
			fastPricing.CacheCreationPricePerToken = pricing.CacheCreationPricePerTokenPriority
			if pricing.SupportsCacheBreakdown && pricing.CacheCreationPricePerToken > 0 {
				ratio := pricing.CacheCreationPricePerTokenPriority / pricing.CacheCreationPricePerToken
				fastPricing.CacheCreation5mPrice *= ratio
				fastPricing.CacheCreation1hPrice *= ratio
			}
		}
		if pricing.CacheReadPricePerTokenPriority > 0 || pricing.PriorityCacheReadPresent {
			fastPricing.CacheReadPricePerToken = pricing.CacheReadPricePerTokenPriority
		}
		return &fastPricing, true
	}

	multiplier := ConfiguredServiceTierMultiplier(OpenAIFastTierPriority, pricing)
	fastPricing.InputPricePerToken *= multiplier
	fastPricing.ImageInputPricePerToken *= multiplier
	fastPricing.OutputPricePerToken *= multiplier
	fastPricing.CacheCreationPricePerToken *= multiplier
	fastPricing.CacheReadPricePerToken *= multiplier
	fastPricing.ImageOutputPricePerToken *= multiplier
	return &fastPricing, true
}

// ResolvedHasFastModeDisplayPricing 同时检查默认价与有效区间，覆盖只有区间单价的自定义模型。
func ResolvedHasFastModeDisplayPricing(resolved *ResolvedPricing) bool {
	if resolved == nil || resolved.Mode != BillingModeToken {
		return false
	}
	if HasFastModeDisplayPricing(resolved.tokenPricingForInterval(nil)) {
		return true
	}
	for i := range resolved.Intervals {
		pricing := resolved.tokenPricingForInterval(&resolved.Intervals[i])
		if HasFastModeDisplayPricing(pricing) {
			return true
		}
	}
	return false
}

func HasFastModeDisplayPricing(pricing *ModelPricing) bool {
	return HasAnyDisplayTokenPricing(pricing) &&
		(pricing.FastModeMultiplier != nil ||
			pricing.FastMultiplier != nil || pricing.PriorityInputPresent || pricing.PriorityOutputPresent || pricing.PriorityCacheReadPresent || pricing.PriorityCacheWritePresent ||
			pricing.InputPricePerTokenPriority > 0 ||
			pricing.OutputPricePerTokenPriority > 0 ||
			pricing.CacheCreationPricePerTokenPriority > 0 ||
			pricing.CacheReadPricePerTokenPriority > 0)
}

// BuildTokenIntervalDisplayPricing 标记此模型需要按上下文区间展示价格。
func BuildTokenIntervalDisplayPricing(intervals []ModelDisplayPricingInterval) ModelDisplayPricing {
	return ModelDisplayPricing{
		PricingMode:      "token",
		PriceStatus:      "priced",
		ContextIntervals: intervals,
	}
}

func BuildImageDisplayPricing(price1K, price2K, price4K float64) ModelDisplayPricing {
	return ModelDisplayPricing{
		PricingMode:     "image",
		PriceStatus:     "priced",
		ImagePriceSizes: []string{"1K", "2K", "4K"},
		ImagePrice1K:    price1K,
		ImagePrice2K:    price2K,
		ImagePrice4K:    price4K,
	}
}

func UnknownDisplayPricing() ModelDisplayPricing {
	return ModelDisplayPricing{
		PricingMode: "unknown",
		PriceStatus: "unpriced",
	}
}

// CalculateWebSearchCost 计算 Codex alpha/search 网页搜索按次费用。
// callCount: 搜索调用次数（每次请求为 1）
// groupPrice: 分组配置的单次价格（nil 表示缺价；0 表示免费）
// rateMultiplier: 分组费率倍数
func CalculateWebSearchCost(callCount int, groupPrice *float64, rateMultiplier float64) *CostBreakdown {
	if callCount <= 0 {
		return &CostBreakdown{}
	}
	if groupPrice == nil {
		return &CostBreakdown{}
	}
	unitPrice := *groupPrice
	if groupPrice != nil && *groupPrice >= 0 {
		unitPrice = *groupPrice
	}
	totalCost := unitPrice * float64(callCount)

	// 应用倍率（保存时强制 > 0；负数按 0 处理避免按 1x 误扣）
	if rateMultiplier < 0 {
		rateMultiplier = 0
	}
	return &CostBreakdown{
		TotalCost:   totalCost,
		ActualCost:  totalCost * rateMultiplier,
		BillingMode: string(BillingModePerRequest),
	}
}

// CalculateSearchCost 按每千次调用结算搜索工具；调用方提供有效单价，显式 0 表示免费。
func CalculateSearchCost(numCalls int, groupPricePer1k *float64, rateMultiplier float64) *CostBreakdown {
	if numCalls <= 0 {
		return &CostBreakdown{}
	}
	if groupPricePer1k == nil {
		return &CostBreakdown{}
	}
	pricePer1k := *groupPricePer1k
	if groupPricePer1k != nil {
		if *groupPricePer1k < 0 {
			return &CostBreakdown{}
		}
		pricePer1k = *groupPricePer1k
	}
	if pricePer1k == 0 {
		return &CostBreakdown{}
	}
	if rateMultiplier < 0 {
		rateMultiplier = 0
	}
	unit := pricePer1k / 1000.0
	total := unit * float64(numCalls)
	return &CostBreakdown{
		TotalCost:   total,
		ActualCost:  total * rateMultiplier,
		BillingMode: string(BillingModePerRequest),
	}
}

// CalculateAudioCost 分别按分钟、百万字符和小时结算 Realtime、TTS 与 STT；
// 调用方完成分组与目录价格解析，显式 0 表示对应模式免费。
func CalculateAudioCost(mode string, durationOrUnits float64, groupConfig *AudioPriceConfig, rateMultiplier float64) *CostBreakdown {
	if durationOrUnits <= 0 {
		return &CostBreakdown{}
	}
	var unitPrice float64
	switch strings.ToLower(mode) {
	case "realtime":
		if groupConfig != nil && groupConfig.RealtimePerMin != nil {
			unitPrice = *groupConfig.RealtimePerMin
		}
	case "tts":
		if groupConfig != nil && groupConfig.TTSPerMChars != nil {
			unitPrice = *groupConfig.TTSPerMChars
		}
	case "stt":
		if groupConfig != nil && groupConfig.STTPerHour != nil {
			unitPrice = *groupConfig.STTPerHour
		}
	default:
		return &CostBreakdown{}
	}
	if unitPrice <= 0 {
		return &CostBreakdown{}
	}
	if rateMultiplier < 0 {
		rateMultiplier = 0
	}
	total := unitPrice * durationOrUnits
	return &CostBreakdown{
		TotalCost:   total,
		ActualCost:  total * rateMultiplier,
		BillingMode: string(BillingModePerRequest),
	}
}

// HasExplicitImageGenerationPricing 仅把明确标记为图片生成的按图价格视为图片计费。
// 部分聊天模型也携带 output_cost_per_image 元数据，不能据此覆盖其 token 定价。
func HasExplicitImageGenerationPricing(pricing *CatalogModelPricing) bool {
	return HasImageUnitPrice(pricing) &&
		strings.EqualFold(strings.TrimSpace(pricing.Mode), "image_generation")
}

func HasAnyDisplayTokenPricing(pricing *ModelPricing) bool {
	if pricing == nil {
		return false
	}
	return pricing.CatalogSource != "" && pricing.CatalogSource != "unpriced" || pricing.InputPricePerToken > 0 ||
		pricing.ImageInputPricePerToken > 0 ||
		pricing.OutputPricePerToken > 0 ||
		pricing.CacheCreationPricePerToken > 0 ||
		pricing.CacheCreation1hPrice > 0 ||
		pricing.CacheReadPricePerToken > 0 ||
		pricing.ImageOutputPricePerToken > 0
}

func LooksLikeImageModel(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return false
	}

	return strings.Contains(model, "-image") ||
		strings.Contains(model, "image-") ||
		strings.Contains(model, "/image") ||
		strings.HasPrefix(model, "imagen-") ||
		strings.Contains(model, "gpt-image") ||
		strings.Contains(model, "dall-e")
}
