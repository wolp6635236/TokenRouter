package testkit

import (
	"context"
	"slices"
	"sort"
	"time"

	billingadapter "github.com/TokenFlux/TokenRouter/internal/billing/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

// ModelKey 模型配置测试输入复合键（显式包含 Platform 防止跨平台同名模型冲突）。
type ModelKey struct {
	GroupID  int64
	Platform string // 平台标识
	Model    string // lowercase
}

// GroupPlatform 通配符定价输入键。
type GroupPlatform struct {
	GroupID  int64
	Platform string
}

// PricePattern 通配符定价条目。
type PricePattern struct {
	Prefix  string
	Pricing *routing.ModelPricingEntry
}

// ModelPattern 通配符映射条目。
type ModelPattern struct {
	Prefix string
	Target string
}

// ModelConfigData 包含模型配置测试的分组策略和价格配置。
type ModelConfigData struct {
	GroupPolicies  map[int64]routing.GroupRoutingPolicy
	Configurations []Configuration
	UseRows        bool
	// 测试指定的价格和模型输入
	Prices        map[ModelKey]*routing.ModelPricingEntry // (GroupID, Platform, Model) → 定价
	PricePatterns map[GroupPlatform][]*PricePattern       // (GroupID, Platform) → 通配符定价（按配置顺序，先匹配先使用）
	Models        map[ModelKey]string                     // (GroupID, Platform, Model) → 映射目标
	ModelPatterns map[GroupPlatform][]*ModelPattern       // (GroupID, Platform) → 通配符映射（按配置顺序，先匹配先使用）
	ByGroup       map[int64]*Configuration                // GroupID → 组合配置
	Platforms     map[int64]string                        // GroupID → Platform

	// 测试记录的目录元数据
	ByID     map[int64]*Configuration
	LoadedAt time.Time
}

// NewModelConfigData 创建空的组合配置输入（所有 map 已初始化）。
func NewModelConfigData() *ModelConfigData {
	return &ModelConfigData{
		Prices:        make(map[ModelKey]*routing.ModelPricingEntry),
		PricePatterns: make(map[GroupPlatform][]*PricePattern),
		Models:        make(map[ModelKey]string),
		ModelPatterns: make(map[GroupPlatform][]*ModelPattern),
		ByGroup:       make(map[int64]*Configuration),
		Platforms:     make(map[int64]string),
		ByID:          make(map[int64]*Configuration),
	}
}

// ModelConfigDataFromRows 转换测试输入，由 routing 的生产入口编译配置。
func ModelConfigDataFromRows(pricingConfigs []Configuration, platforms map[int64]string) *ModelConfigData {
	fixture := NewModelConfigData()
	fixture.Configurations = pricingConfigs
	fixture.UseRows = true
	fixture.Platforms = platforms
	return fixture
}

// ModelConfigFromData 将夹具数据装配到唯一的生产模型配置实现。
func ModelConfigFromData(fixture *ModelConfigData) *routing.PricingConfigService {
	if fixture.GroupPolicies == nil {
		fixture.GroupPolicies = make(map[int64]routing.GroupRoutingPolicy)
	}
	var pricingConfigs []Configuration
	if fixture.UseRows {
		pricingConfigs = fixture.Configurations
	} else {
		ids := make([]int64, 0, len(fixture.ByGroup))
		for gid := range fixture.ByGroup {
			ids = append(ids, gid)
		}
		slices.Sort(ids)
		for _, gid := range ids {
			ch := fixture.ByGroup[gid].Clone()
			ch.GroupIDs = []int64{gid}
			// 将展平的测试价格按分组还原为价卡数据。
			keys := make([]ModelKey, 0, len(fixture.Prices))
			for key := range fixture.Prices {
				if key.GroupID == gid {
					keys = append(keys, key)
				}
			}
			sort.Slice(keys, func(i, j int) bool {
				if keys[i].Platform != keys[j].Platform {
					return keys[i].Platform < keys[j].Platform
				}
				return keys[i].Model < keys[j].Model
			})
			for _, key := range keys {
				price := fixture.Prices[key].Clone()
				price.Models = []string{key.Model}
				ch.ModelPricing = append(ch.ModelPricing, price)
			}
			for key, entries := range fixture.PricePatterns {
				if key.GroupID == gid {
					for _, entry := range entries {
						price := entry.Pricing.Clone()
						price.Models = []string{entry.Prefix + "*"}
						ch.ModelPricing = append(ch.ModelPricing, price)
					}
				}
			}
			policy, exists := fixture.GroupPolicies[gid]
			if !exists {
				policy = ch.Policy()
			}
			if len(fixture.Models) > 0 || len(fixture.ModelPatterns) > 0 {
				policy.Enabled = true
			}
			if policy.ModelMapping == nil {
				policy.ModelMapping = make(map[string]string)
			}
			for key, target := range fixture.Models {
				if key.GroupID == gid {
					policy.ModelMapping[key.Model] = target
				}
			}
			for key, entries := range fixture.ModelPatterns {
				if key.GroupID == gid {
					for _, entry := range entries {
						policy.ModelMapping[entry.Prefix+"*"] = entry.Target
					}
				}
			}
			fixture.GroupPolicies[gid] = policy
			pricingConfigs = append(pricingConfigs, *ch)
		}
	}
	return NewPricingConfigService(ConfigRows{Values: pricingConfigs, Platforms: fixture.Platforms}, nil, routing.PricingConfigOptions{Now: time.Now, LoadLocation: billingadapter.LoadPricingLocation, ReadGroup: func(_ context.Context, id int64) (*routing.Group, error) {
		policy, exists := fixture.GroupPolicies[id]
		if !exists {
			for _, row := range pricingConfigs {
				for _, groupID := range row.GroupIDs {
					if groupID == id {
						policy = row.Policy()
					}
				}
			}
		}
		return &routing.Group{ID: id, AllowedProtocols: capability.DefaultGroupClientProtocols(""), RoutingPolicy: policy}, nil
	}})
}
