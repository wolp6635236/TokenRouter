package pricing

import (
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"slices"
	"time"
)

// BillingDefaultsKey 是本地文件的保留节点，不属于模型身份空间。
const BillingDefaultsKey = "_billing_defaults"

// CatalogRules 保存目录之外经核实的价格维度；空指针表示没有额外商业规则。
type CatalogRules struct {
	ImagePrices                  MediaPrices        `json:"image_prices,omitempty"`
	VideoPrices                  MediaPrices        `json:"video_prices,omitempty"`
	FastMultiplier               *float64           `json:"fast_multiplier,omitempty"`
	FlexMultiplier               *float64           `json:"flex_multiplier,omitempty"`
	MaxReasoningEffortMultiplier *float64           `json:"max_reasoning_effort_multiplier,omitempty"`
	CacheWriteMultiplier         *float64           `json:"cache_write_multiplier,omitempty"`
	CacheWrite1hMultiplier       *float64           `json:"cache_write_1h_multiplier,omitempty"`
	TimePricing                  *TimePricingConfig `json:"time_pricing,omitempty"`
	SourceURL                    string             `json:"source_url,omitempty"`
	VerifiedAt                   string             `json:"verified_at,omitempty"`
}

// OperationPrices 保存按操作计量的单价；缺失值不能解释为免费。
type OperationPrices struct {
	WebSearchPricePerCall        *float64          `json:"web_search_price_per_call,omitempty"`
	SearchPricePer1k             *float64          `json:"search_price_per_1k,omitempty"`
	AudioRealtimePricePerMin     *float64          `json:"audio_realtime_price_per_min,omitempty"`
	AudioTTSPricePerMillionChars *float64          `json:"audio_tts_price_per_million_chars,omitempty"`
	AudioSTTPricePerHour         *float64          `json:"audio_stt_price_per_hour,omitempty"`
	Sources                      map[string]string `json:"sources,omitempty"`
	VerifiedAt                   string            `json:"verified_at,omitempty"`
}

// MediaPrices 区分明确的零单价与无效的空尺寸价格。
type MediaPrices map[string]float64

func copyOptional[T any](value *T) *T {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

// Clone 隔离目录规则中的集合与可空字段，供只读快照和单次计算使用。
func (r CatalogRules) Clone() CatalogRules {
	r.ImagePrices = maps.Clone(r.ImagePrices)
	r.VideoPrices = maps.Clone(r.VideoPrices)
	r.FastMultiplier = copyOptional(r.FastMultiplier)
	r.FlexMultiplier = copyOptional(r.FlexMultiplier)
	r.MaxReasoningEffortMultiplier = copyOptional(r.MaxReasoningEffortMultiplier)
	r.CacheWriteMultiplier = copyOptional(r.CacheWriteMultiplier)
	r.CacheWrite1hMultiplier = copyOptional(r.CacheWrite1hMultiplier)
	r.TimePricing = copyOptional(r.TimePricing)
	if r.TimePricing != nil {
		r.TimePricing.Periods = slices.Clone(r.TimePricing.Periods)
	}
	return r
}

// Validate 拒绝非法规则，避免部分字段发布后才出现计费失败。
func (r CatalogRules) Validate() error {
	for name, values := range map[string]map[string]float64{"image_prices": r.ImagePrices, "video_prices": r.VideoPrices} {
		for key, price := range values {
			valid := key == "1K" || key == "2K" || key == "4K"
			if name == "video_prices" {
				valid = key == "480p" || key == "720p" || key == "1080p"
			}
			if !valid || !validAmount(price) {
				return fmt.Errorf("invalid %s.%s", name, key)
			}
		}
	}
	for name, value := range map[string]*float64{"fast_multiplier": r.FastMultiplier, "flex_multiplier": r.FlexMultiplier, "max_reasoning_effort_multiplier": r.MaxReasoningEffortMultiplier, "cache_write_multiplier": r.CacheWriteMultiplier, "cache_write_1h_multiplier": r.CacheWrite1hMultiplier} {
		if value != nil && (!validAmount(*value) || *value == 0) {
			return fmt.Errorf("invalid %s", name)
		}
	}
	if r.TimePricing != nil {
		if err := ValidateTimezoneName(r.TimePricing.Timezone); err != nil {
			return err
		}
		if _, err := time.LoadLocation(r.TimePricing.Timezone); err != nil {
			return fmt.Errorf("invalid time_pricing.timezone: %w", err)
		}
		return ValidateTimePricingConfig(r.TimePricing)
	}
	return nil
}

func validAmount(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

// Clone 返回操作价的独立副本。
func (p OperationPrices) Clone() OperationPrices {
	p.WebSearchPricePerCall = copyOptional(p.WebSearchPricePerCall)
	p.SearchPricePer1k = copyOptional(p.SearchPricePer1k)
	p.AudioRealtimePricePerMin = copyOptional(p.AudioRealtimePricePerMin)
	p.AudioTTSPricePerMillionChars = copyOptional(p.AudioTTSPricePerMillionChars)
	p.AudioSTTPricePerHour = copyOptional(p.AudioSTTPricePerHour)
	p.Sources = maps.Clone(p.Sources)
	return p
}

// Validate 检查操作单价，包括仅含覆盖字段的补丁。
func (p OperationPrices) Validate() error {
	for name, value := range map[string]*float64{"web_search_price_per_call": p.WebSearchPricePerCall, "search_price_per_1k": p.SearchPricePer1k, "audio_realtime_price_per_min": p.AudioRealtimePricePerMin, "audio_tts_price_per_million_chars": p.AudioTTSPricePerMillionChars, "audio_stt_price_per_hour": p.AudioSTTPricePerHour} {
		if value != nil && !validAmount(*value) {
			return fmt.Errorf("invalid %s", name)
		}
	}
	return nil
}

// deriveCatalogCachePrices 将明确缓存规则应用于每个上下文档，保留显式价和零价。
func deriveCatalogCachePrices(price *CatalogModelPricing) error {
	if price == nil {
		return nil
	}
	if price.PriceSources == nil {
		price.PriceSources = map[string]string{}
	}
	derive := func(base float64, basePresent bool, multiplier *float64, target *float64, present *bool, label string) error {
		if !basePresent || multiplier == nil || *present {
			return nil
		}
		value := base * *multiplier
		if !validAmount(value) {
			return fmt.Errorf("invalid derived %s", label)
		}
		*target, *present = value, true
		price.PriceSources[label] = "rule_supplement"
		return nil
	}
	if err := derive(price.InputCostPerToken, price.InputPricePresent, price.CacheWriteMultiplier, &price.CacheCreationInputTokenCost, &price.CacheCreationPricePresent, "cache_write"); err != nil {
		return err
	}
	if err := derive(price.InputCostPerTokenPriority, price.PriorityInputPresent || price.InputCostPerTokenPriority > 0, price.CacheWriteMultiplier, &price.CacheCreationInputTokenCostPriority, &price.PriorityCacheWritePresent, "priority_cache_write"); err != nil {
		return err
	}
	if err := derive(price.InputCostPerToken, price.InputPricePresent, price.CacheWrite1hMultiplier, &price.CacheCreationInputTokenCostAbove1hr, &price.CacheCreation1hPricePresent, "cache_write_1h"); err != nil {
		return err
	}
	for _, tier := range price.ContextPrices {
		if tier.Pricing == nil {
			continue
		}
		if tier.Pricing.CacheWriteMultiplier == nil {
			tier.Pricing.CacheWriteMultiplier = copyOptional(price.CacheWriteMultiplier)
		}
		if tier.Pricing.CacheWrite1hMultiplier == nil {
			tier.Pricing.CacheWrite1hMultiplier = copyOptional(price.CacheWrite1hMultiplier)
		}
		if err := deriveCatalogCachePrices(tier.Pricing); err != nil {
			return err
		}
	}
	return nil
}

// UnmarshalJSON 拒绝尺寸值 null，避免 JSON 解码将未知单价变成免费。
func (p *MediaPrices) UnmarshalJSON(body []byte) error {
	var values map[string]*float64
	if err := json.Unmarshal(body, &values); err != nil {
		return err
	}
	if values == nil {
		*p = nil
		return nil
	}
	result := make(MediaPrices, len(values))
	for key, value := range values {
		if value == nil {
			return fmt.Errorf("media price %s must be a number", key)
		}
		result[key] = *value
	}
	*p = result
	return nil
}
