package pricing

import (
	"encoding/json"
	"maps"
	"slices"
)

// CatalogModelPricing 保存每 token 单价及目录元数据，存在性标记区分缺价和显式零价。
type CatalogModelPricing struct {
	CatalogRules
	InputPricePresent           bool                  `json:"-"`
	CacheCreation1hPricePresent bool                  `json:"-"`
	PriorityInputPresent        bool                  `json:"-"`
	PriorityOutputPresent       bool                  `json:"-"`
	PriorityCacheReadPresent    bool                  `json:"-"`
	PriorityCacheWritePresent   bool                  `json:"-"`
	Source                      string                `json:"source,omitempty"`
	PriceSources                map[string]string     `json:"price_sources,omitempty"`
	ContextPrices               []CatalogContextPrice `json:"context_prices,omitempty"`
	// 保留价格桶是否显式存在，供默认价格页区分免费与不适用。
	CacheCreationPricePresent           bool    `json:"-"`
	CacheReadPricePresent               bool    `json:"-"`
	ImagePricePresent                   bool    `json:"-"`
	ImageInputPricePresent              bool    `json:"-"`
	ImageOutputPricePresent             bool    `json:"-"`
	InputCostPerToken                   float64 `json:"input_cost_per_token"`
	InputCostPerTokenPriority           float64 `json:"input_cost_per_token_priority"`
	OutputCostPerToken                  float64 `json:"output_cost_per_token"`
	OutputCostPerTokenPriority          float64 `json:"output_cost_per_token_priority"`
	CacheCreationInputTokenCost         float64 `json:"cache_creation_input_token_cost"`
	CacheCreationInputTokenCostPriority float64 `json:"cache_creation_input_token_cost_priority"`
	CacheCreationInputTokenCostAbove1hr float64 `json:"cache_creation_input_token_cost_above_1hr"`
	CacheReadInputTokenCost             float64 `json:"cache_read_input_token_cost"`
	CacheReadInputTokenCostPriority     float64 `json:"cache_read_input_token_cost_priority"`
	LongContextInputTokenThreshold      int     `json:"long_context_input_token_threshold,omitempty"`
	LongContextInputCostMultiplier      float64 `json:"long_context_input_cost_multiplier,omitempty"`
	LongContextOutputCostMultiplier     float64 `json:"long_context_output_cost_multiplier,omitempty"`
	SupportsServiceTier                 bool    `json:"supports_service_tier"`
	Provider                            string  `json:"provider"`
	Mode                                string  `json:"mode"`
	SupportsPromptCaching               bool    `json:"supports_prompt_caching"`
	OutputCostPerImage                  float64 `json:"output_cost_per_image"`       // 图片生成模型每张图片价格
	OutputCostPerImageToken             float64 `json:"output_cost_per_image_token"` // 图片输出 token 价格
	InputCostPerImageToken              float64 `json:"input_cost_per_image_token"`  // 图片输入 token 价格（如 gpt-image-2 图片编辑）

	// 模型能力元数据：由模型广场下发给前端展示输入/输出模态，不参与计费。
	SupportedModalities       []string `json:"supported_modalities"`
	SupportedOutputModalities []string `json:"supported_output_modalities"`
	SupportsVision            bool     `json:"supports_vision"`
	SupportsAudioInput        bool     `json:"supports_audio_input"`
	SupportsAudioOutput       bool     `json:"supports_audio_output"`
	SupportsVideoInput        bool     `json:"supports_video_input"`

	// TokenPricingAbsent 表示源数据缺少 token 定价；models.dev 要求输入和输出桶都存在。
	// models.dev 缺价时直接返回未定价；旧文件继续兼容单侧 token 单价。
	// 独立图片计费可继续读取此类条目的媒体价格。
	TokenPricingAbsent bool `json:"-"`
}

// CatalogRawEntry 使用指针保留原始价格字段的缺失与显式零值。
type CatalogRawEntry struct {
	CatalogRules
	Source                              string                `json:"source,omitempty"`
	PriceSources                        map[string]string     `json:"price_sources,omitempty"`
	ContextPrices                       []CatalogContextPrice `json:"context_prices,omitempty"`
	InputCostPerToken                   *float64              `json:"input_cost_per_token"`
	InputCostPerTokenPriority           *float64              `json:"input_cost_per_token_priority"`
	OutputCostPerToken                  *float64              `json:"output_cost_per_token"`
	OutputCostPerTokenPriority          *float64              `json:"output_cost_per_token_priority"`
	CacheCreationInputTokenCost         *float64              `json:"cache_creation_input_token_cost"`
	CacheCreationInputTokenCostPriority *float64              `json:"cache_creation_input_token_cost_priority"`
	CacheCreationInputTokenCostAbove1hr *float64              `json:"cache_creation_input_token_cost_above_1hr"`
	CacheReadInputTokenCost             *float64              `json:"cache_read_input_token_cost"`
	CacheReadInputTokenCostPriority     *float64              `json:"cache_read_input_token_cost_priority"`
	LongContextInputTokenThreshold      *int                  `json:"long_context_input_token_threshold"`
	LongContextInputCostMultiplier      *float64              `json:"long_context_input_cost_multiplier"`
	LongContextOutputCostMultiplier     *float64              `json:"long_context_output_cost_multiplier"`
	SupportsServiceTier                 bool                  `json:"supports_service_tier"`
	Provider                            string                `json:"provider"`
	Mode                                string                `json:"mode"`
	SupportsPromptCaching               bool                  `json:"supports_prompt_caching"`
	OutputCostPerImage                  *float64              `json:"output_cost_per_image"`
	OutputCostPerImageToken             *float64              `json:"output_cost_per_image_token"`
	InputCostPerImageToken              *float64              `json:"input_cost_per_image_token"`
	SupportedModalities                 []string              `json:"supported_modalities"`
	SupportedInputModalities            []string              `json:"supported_input_modalities"`
	SupportedOutputModalities           []string              `json:"supported_output_modalities"`
	SupportsVision                      bool                  `json:"supports_vision"`
	SupportsAudioInput                  bool                  `json:"supports_audio_input"`
	SupportsAudioOutput                 bool                  `json:"supports_audio_output"`
	SupportsVideoInput                  bool                  `json:"supports_video_input"`
}

// CatalogContextPrice 保存目录给出的绝对阶梯价，适用于超过 Threshold 的整次用量。
type CatalogContextPrice struct {
	Threshold int                  `json:"threshold"`
	Pricing   *CatalogModelPricing `json:"pricing"`
}

// CloneCatalogPrice 递归复制阶梯与来源标签，避免只读快照丢失零价存在性。
func CloneCatalogPrice(value *CatalogModelPricing) *CatalogModelPricing {
	if value == nil {
		return nil
	}
	result := *value
	result.CatalogRules = value.Clone()
	result.SupportedModalities = slices.Clone(value.SupportedModalities)
	result.SupportedOutputModalities = slices.Clone(value.SupportedOutputModalities)
	if value.PriceSources != nil {
		result.PriceSources = make(map[string]string, len(value.PriceSources))
		maps.Copy(result.PriceSources, value.PriceSources)
	}
	if value.ContextPrices != nil {
		result.ContextPrices = make([]CatalogContextPrice, len(value.ContextPrices))
	}
	for i, tier := range value.ContextPrices {
		result.ContextPrices[i] = CatalogContextPrice{Threshold: tier.Threshold, Pricing: CloneCatalogPrice(tier.Pricing)}
	}
	return &result
}

// UnmarshalJSON 用同一价格解析器读取阶梯，保留缓存桶缺失与零价的区别。
func (c *CatalogContextPrice) UnmarshalJSON(body []byte) error {
	var raw struct {
		Threshold int             `json:"threshold"`
		Pricing   json.RawMessage `json:"pricing"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return err
	}
	entries, diagnostics, err := ParsePricingEntries(map[string]json.RawMessage{"tier": raw.Pricing})
	if validationErr := diagnostics.ValidationError(); validationErr != nil {
		return validationErr
	}
	if err != nil {
		return err
	}
	c.Threshold, c.Pricing = raw.Threshold, entries["tier"]
	return nil
}
