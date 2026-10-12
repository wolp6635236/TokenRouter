package routing

import (
	"strings"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/billing/pricing"
)

const (
	BillingModeToken      = pricing.BillingModeToken
	BillingModePerRequest = pricing.BillingModePerRequest
	BillingModeImage      = pricing.BillingModeImage
	BillingModeVideo      = pricing.BillingModeVideo

	BillingModelSourceRequested   = "requested"
	BillingModelSourceUpstream    = "upstream"
	BillingModelSourceGroupMapped = "group_mapped"
)

// BillingMode 使用统一价卡的计费模式。
type BillingMode = pricing.BillingMode

// PricingConfig 价格配置实体。
type PricingConfig struct {
	pricing.BillingSettings
	ID                 int64
	Name               string
	Description        string
	Status             string
	BillingModelSource string // "requested", "upstream", or "group_mapped"

	CreatedAt time.Time
	UpdatedAt time.Time

	// 关联的分组 ID 列表
	GroupIDs []int64
	// 共享模型定价列表
	ModelPricing []ModelPricingEntry

	// 提供商统计定价
	ProviderStatsPricingRules []ProviderStatsPricingRule // 自定义提供商统计定价规则（按 SortOrder 排序，先命中为准）
}

// ProviderStatsPricingRule 定义提供商成本统计的定价规则。
type ProviderStatsPricingRule = pricing.ProviderStatsPricingRule

// ModelPricingEntry 定义共享价格配置的模型价卡。
type ModelPricingEntry = pricing.ModelPricingEntry

// TimePricingConfig 定义每日分时倍率。
type TimePricingConfig = pricing.TimePricingConfig

// TimePricingPeriod 定义单个分时时段。
type TimePricingPeriod = pricing.TimePricingPeriod

// PricingInterval 定义上下文或媒体规格的价格区间。
type PricingInterval = pricing.PricingInterval

// PricingUsageFields 价格配置相关的使用记录字段（嵌入到各平台的 RecordUsageInput 中）。
type PricingUsageFields struct {
	PricingConfigID    int64  // 价格配置 ID（0 = 无价格配置）
	OriginalModel      string // Key 重定向后的请求模型（分组映射前）
	GroupMappedModel   string // 分组映射后的模型名（无映射时等于 OriginalModel）
	BillingModelSource string // 计费模型来源："requested" / "upstream" / "group_mapped"
	ModelMappingChain  string // 映射链描述，如 "a→b→c"
}

// IsActive 判断价格配置是否启用。
func (c *PricingConfig) IsActive() bool {
	return c.Status == StatusActive
}

// GetModelPricing 根据模型名查找价格配置定价，未找到返回 nil。
// 精确匹配，大小写不敏感。返回值拷贝，不污染缓存。
func (c *PricingConfig) GetModelPricing(model string) *ModelPricingEntry {
	modelLower := strings.ToLower(model)

	for i := range c.ModelPricing {
		for _, m := range c.ModelPricing[i].Models {
			if strings.EqualFold(m, modelLower) {
				cp := c.ModelPricing[i].Clone()
				return &cp
			}
		}
	}

	return nil
}

// Clone 返回 PricingConfig 的深拷贝。
func (c *PricingConfig) Clone() *PricingConfig {
	if c == nil {
		return nil
	}
	cp := *c
	cp.BillingSettings = c.BillingSettings.Clone()
	if c.GroupIDs != nil {
		cp.GroupIDs = make([]int64, len(c.GroupIDs))
		copy(cp.GroupIDs, c.GroupIDs)
	}
	if c.ModelPricing != nil {
		cp.ModelPricing = make([]ModelPricingEntry, len(c.ModelPricing))
		for i := range c.ModelPricing {
			cp.ModelPricing[i] = c.ModelPricing[i].Clone()
		}
	}

	if c.ProviderStatsPricingRules != nil {
		cp.ProviderStatsPricingRules = make([]ProviderStatsPricingRule, len(c.ProviderStatsPricingRules))
		for i, rule := range c.ProviderStatsPricingRules {
			cp.ProviderStatsPricingRules[i] = rule
			if rule.GroupIDs != nil {
				cp.ProviderStatsPricingRules[i].GroupIDs = make([]int64, len(rule.GroupIDs))
				copy(cp.ProviderStatsPricingRules[i].GroupIDs, rule.GroupIDs)
			}
			if rule.ProviderIDs != nil {
				cp.ProviderStatsPricingRules[i].ProviderIDs = make([]int64, len(rule.ProviderIDs))
				copy(cp.ProviderStatsPricingRules[i].ProviderIDs, rule.ProviderIDs)
			}
			if rule.Pricing != nil {
				cp.ProviderStatsPricingRules[i].Pricing = make([]ModelPricingEntry, len(rule.Pricing))
				for j := range rule.Pricing {
					cp.ProviderStatsPricingRules[i].Pricing[j] = rule.Pricing[j].Clone()
				}
			}
		}
	}
	return &cp
}

// ValidateIntervals 委托唯一纯定价实现，保留旧调用签名。
func ValidateIntervals(intervals []PricingInterval, mode BillingMode) error {
	return pricing.ValidateIntervals(intervals, mode)
}
