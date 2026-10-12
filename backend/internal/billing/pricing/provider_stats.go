package pricing

import (
	"slices"
	"strings"
)

// ProviderStatsInput 包含查询到的价卡，nil 成本和配置的零价分别处理。
type ProviderStatsInput struct {
	Rules               []ProviderStatsPricingRule
	ProviderID, GroupID int64
	Models              []string
	Tokens              UsageTokens
	RequestCount        int
}

func UniqueNonEmptyProviderStatsModels(models []string) []string {
	out := make([]string, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		key := strings.ToLower(model)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, model)
	}
	return out
}

// TryCustomRules 遍历自定义规则，按数组顺序先命中为准。
func TryCustomRules(
	rules []ProviderStatsPricingRule, providerID, groupID int64,
	model string, tokens UsageTokens, requestCount int,
) *float64 {
	modelLower := strings.ToLower(model)
	for _, rule := range rules {
		if !MatchProviderStatsRule(&rule, providerID, groupID) {
			continue
		}
		pricing := FindEffectivePricingForModel(rule.Pricing, modelLower)
		if pricing == nil {
			continue // 规则匹配但模型不在规则定价中，继续下一条
		}
		// 自定义统计价是独立的最终成本基数，不继承用户侧的模型/推理倍率。
		if cost := CalculateStatsCost(pricing, tokens, requestCount); cost != nil {
			return cost
		}
	}
	return nil
}

// MatchProviderStatsRule 检查规则是否匹配指定的 providerID 和 groupID。
// 匹配条件：providerID ∈ rule.ProviderIDs 或 groupID ∈ rule.GroupIDs。
// 如果规则的 ProviderIDs 和 GroupIDs 都为空，视为不匹配。
func MatchProviderStatsRule(rule *ProviderStatsPricingRule, providerID, groupID int64) bool {
	if len(rule.ProviderIDs) == 0 && len(rule.GroupIDs) == 0 {
		return false
	}
	if slices.Contains(rule.ProviderIDs, providerID) {
		return true
	}
	return slices.Contains(rule.GroupIDs, groupID)
}

// FindEffectivePricingForModel 用于提供商统计成本规则。
// 空定价行是配置占位，零值指针表示有效的 0 成本覆盖。
func FindEffectivePricingForModel(pricingList []ModelPricingEntry, modelLower string) *ModelPricingEntry {
	return FindPricingForModelByPredicate(pricingList, modelLower, func(p *ModelPricingEntry) bool {
		return p != nil && p.HasEffectivePricing()
	})
}

// FindPricingForModelByPredicate 在通过筛选的定价行中先精确匹配，再按配置顺序匹配通配符。
func FindPricingForModelByPredicate(pricingList []ModelPricingEntry, modelLower string, include func(*ModelPricingEntry) bool) *ModelPricingEntry {
	if include == nil {
		include = func(*ModelPricingEntry) bool { return true }
	}
	// 精确匹配优先
	for i := range pricingList {
		p := &pricingList[i]
		if !include(p) {
			continue
		}
		for _, m := range p.Models {
			if strings.EqualFold(m, modelLower) {
				return p
			}
		}
	}
	// 通配符匹配：按配置顺序，先匹配先使用
	for i := range pricingList {
		p := &pricingList[i]
		if !include(p) {
			continue
		}
		for _, m := range p.Models {
			ml := strings.ToLower(m)
			if !strings.HasSuffix(ml, "*") {
				continue
			}
			prefix := strings.TrimSuffix(ml, "*")
			if strings.HasPrefix(modelLower, prefix) {
				return p
			}
		}
	}
	return nil
}

// CalculateStatsCost 使用给定的定价计算费用，并在最后应用可选的定价倍率。
func CalculateStatsCost(pricing *ModelPricingEntry, tokens UsageTokens, requestCount int) *float64 {
	if pricing == nil {
		return nil
	}
	var cost *float64
	switch pricing.BillingMode {
	case BillingModePerRequest, BillingModeImage:
		cost = CalculatePerRequestStatsCost(pricing, requestCount)
	default:
		cost = CalculateTokenStatsCost(pricing, tokens)
	}
	if cost == nil {
		return nil
	}
	// 提供商统计规则与实际价卡计费共用同一倍率语义。
	if multiplier, configured := NormalizedPriceMultiplier(pricing); configured {
		scaled := *cost * multiplier
		cost = &scaled
	}
	return cost
}

// CalculatePerRequestStatsCost 按次/图片计费。
func CalculatePerRequestStatsCost(pricing *ModelPricingEntry, requestCount int) *float64 {
	if pricing.PerRequestPrice == nil {
		return nil
	}
	if requestCount <= 0 {
		requestCount = 1
	}
	cost := *pricing.PerRequestPrice * float64(requestCount)
	if cost < 0 {
		return nil
	}
	return &cost
}

// CalculateTokenStatsCost Token 计费。
// If the pricing has intervals, find the matching interval by total token count
// and use its prices instead of the flat pricing fields.
func CalculateTokenStatsCost(pricing *ModelPricingEntry, tokens UsageTokens) *float64 {
	p := pricing
	if validIntervals := FilterValidTokenIntervals(pricing.Intervals); len(validIntervals) > 0 {
		totalTokens := tokens.InputTokens + tokens.OutputTokens + tokens.CacheCreationTokens + tokens.CacheReadTokens
		if iv := FindMatchingInterval(validIntervals, totalTokens); iv != nil {
			p = &ModelPricingEntry{
				InputPrice:        iv.InputPrice,
				OutputPrice:       iv.OutputPrice,
				CacheWritePrice:   iv.CacheWritePrice,
				CacheWrite1hPrice: iv.CacheWrite1hPrice,
				CacheReadPrice:    iv.CacheReadPrice,
				ImageOutputPrice:  pricing.ImageOutputPrice,
			}
		}
	}
	if !HasAnyTokenStatsPrice(p) || !HasAnyStatsTokenUsage(tokens) {
		return nil
	}
	deref := func(ptr *float64) float64 {
		if ptr == nil {
			return 0
		}
		return *ptr
	}
	cacheCreationCost := float64(tokens.CacheCreationTokens) * deref(p.CacheWritePrice)
	if p.CacheWrite1hPrice != nil {
		cache5m, cache1h := NormalizeCacheCreationBreakdown(tokens)
		if cache5m > 0 || cache1h > 0 {
			cacheCreationCost = float64(cache5m)*deref(p.CacheWritePrice) +
				float64(cache1h)*deref(p.CacheWrite1hPrice)
		}
	}
	cost := float64(tokens.InputTokens)*deref(p.InputPrice) +
		float64(tokens.OutputTokens)*deref(p.OutputPrice) +
		cacheCreationCost +
		float64(tokens.CacheReadTokens)*deref(p.CacheReadPrice) +
		float64(tokens.ImageOutputTokens)*deref(p.ImageOutputPrice)
	if cost < 0 {
		return nil
	}
	return &cost
}

func HasAnyTokenStatsPrice(pricing *ModelPricingEntry) bool {
	return pricing != nil && (pricing.InputPrice != nil ||
		pricing.OutputPrice != nil ||
		pricing.CacheWritePrice != nil ||
		pricing.CacheWrite1hPrice != nil ||
		pricing.CacheReadPrice != nil ||
		pricing.ImageOutputPrice != nil)
}

func HasAnyStatsTokenUsage(tokens UsageTokens) bool {
	return tokens.InputTokens > 0 ||
		tokens.OutputTokens > 0 ||
		tokens.CacheCreationTokens > 0 ||
		tokens.CacheReadTokens > 0 ||
		tokens.ImageOutputTokens > 0
}

// ResolveProviderStatsOverride 返回 handled，区分明确不覆盖与继续查询模型目录。
func ResolveProviderStatsOverride(input ProviderStatsInput) (*float64, bool) {
	for _, model := range input.Models {
		if cost := TryCustomRules(input.Rules, input.ProviderID, input.GroupID, model, input.Tokens, input.RequestCount); cost != nil {
			return cost, true
		}
	}
	return nil, false
}
