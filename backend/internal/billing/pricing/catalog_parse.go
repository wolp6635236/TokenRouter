package pricing

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// ErrNoPricingEntries 表示没有可用报价；调用方仍须先检查逐条解码诊断。
var ErrNoPricingEntries = errors.New("no valid pricing entries found")

// CatalogDiagnostics 描述纯解析发现的目录问题，日志输出由 provider 负责。
type CatalogDiagnostics struct {
	Skipped                           int
	InvalidEntries                    []string
	OrphanCacheTiers, LopsidedLadders []string
}

// ValidationError 把逐条解码错误交给要求完整发布的目录加载器处理。
func (d CatalogDiagnostics) ValidationError() error {
	if len(d.InvalidEntries) == 0 {
		return nil
	}
	return fmt.Errorf("invalid pricing entries: %s", strings.Join(d.InvalidEntries, "; "))
}

// DecodeCatalogEntries 读取本地价格对象，并在合并前统一旧字段名称。
func DecodeCatalogEntries(body []byte) (map[string]json.RawMessage, error) {
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, err
	}
	if entries == nil {
		return nil, fmt.Errorf("expected a pricing JSON object")
	}
	for name, entry := range entries {
		if name == "sample_spec" {
			delete(entries, name)
			continue
		}
		normalized, err := normalizeCatalogEntry(entry)
		if err != nil {
			return nil, fmt.Errorf("invalid pricing entry %s: %w", name, err)
		}
		entries[name] = normalized
	}
	return entries, nil
}

// normalizeCatalogEntry 只在输入边界兼容 litellm_provider；新字段的空值和 null 同样优先。
func normalizeCatalogEntry(raw json.RawMessage) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	if fields == nil {
		return nil, fmt.Errorf("expected an object")
	}
	legacy, exists := fields["litellm_provider"]
	if !exists {
		return raw, nil
	}
	if _, present := fields["provider"]; !present {
		fields["provider"] = legacy
	}
	delete(fields, "litellm_provider")
	return json.Marshal(fields)
}

// ParsePricingEntries 在已读取的原始条目上执行唯一价格解析，不读取配置或文件。
func ParsePricingEntries(rawData map[string]json.RawMessage) (map[string]*CatalogModelPricing, CatalogDiagnostics, error) {
	result := make(map[string]*CatalogModelPricing)
	skipped := 0
	var invalidEntries []string
	var orphanCacheTiers, lopsidedLadders []string

	for modelName, rawEntry := range rawData {
		// 跳过 sample_spec 等文档条目
		if modelName == "sample_spec" {
			continue
		}

		// 尝试解析每个条目
		normalized, decodeErr := normalizeCatalogEntry(rawEntry)
		var entry CatalogRawEntry
		if decodeErr == nil {
			decodeErr = json.Unmarshal(normalized, &entry)
		}
		if decodeErr != nil {
			skipped++
			invalidEntries = append(invalidEntries, fmt.Sprintf("%s: %v", modelName, decodeErr))
			continue
		}

		// 可空单价保留零值，负数和非有限值拒绝整次发布。
		invalidAmount := false
		value := reflect.ValueOf(entry)
		fields := value.Type()
		for i := range value.NumField() {
			field := value.Field(i)
			if field.Kind() == reflect.Pointer && field.Type().Elem().Kind() == reflect.Float64 && !field.IsNil() && !validAmount(field.Elem().Float()) {
				invalidEntries = append(invalidEntries, fmt.Sprintf("%s: invalid %s", modelName, fields.Field(i).Tag.Get("json")))
				invalidAmount = true
			}
		}
		if invalidAmount {
			continue
		}
		// 规则和补丁同样需要校验，不能因缺少基础价绕过更新验证。
		if err := entry.Validate(); err != nil {
			invalidEntries = append(invalidEntries, fmt.Sprintf("%s: %v", modelName, err))
			continue
		}
		if modelName == BillingDefaultsKey {
			var defaults OperationPrices
			if err := json.Unmarshal(rawEntry, &defaults); err != nil {
				invalidEntries = append(invalidEntries, fmt.Sprintf("%s: %v", modelName, err))
			} else if err := defaults.Validate(); err != nil {
				invalidEntries = append(invalidEntries, fmt.Sprintf("%s: %v", modelName, err))
			}
			continue
		}
		// 只保留有有效价格的条目
		if entry.InputCostPerToken == nil && entry.OutputCostPerToken == nil && entry.OutputCostPerImage == nil && entry.OutputCostPerImageToken == nil && entry.InputCostPerImageToken == nil && len(entry.ImagePrices) == 0 && len(entry.VideoPrices) == 0 {
			continue
		}

		pricing := &CatalogModelPricing{
			InputPricePresent:           entry.InputCostPerToken != nil,
			CatalogRules:                entry.Clone(),
			CacheCreation1hPricePresent: entry.CacheCreationInputTokenCostAbove1hr != nil,
			PriorityInputPresent:        entry.Source != "" && entry.InputCostPerTokenPriority != nil,
			PriorityOutputPresent:       entry.Source != "" && entry.OutputCostPerTokenPriority != nil,
			PriorityCacheReadPresent:    entry.Source != "" && entry.CacheReadInputTokenCostPriority != nil,
			PriorityCacheWritePresent:   entry.Source != "" && entry.CacheCreationInputTokenCostPriority != nil,
			Source:                      entry.Source,
			PriceSources:                entry.PriceSources,
			ContextPrices:               entry.ContextPrices,
			CacheCreationPricePresent:   entry.CacheCreationInputTokenCost != nil,
			CacheReadPricePresent:       entry.CacheReadInputTokenCost != nil,
			ImagePricePresent:           entry.OutputCostPerImage != nil,
			ImageInputPricePresent:      entry.InputCostPerImageToken != nil,
			ImageOutputPricePresent:     entry.OutputCostPerImageToken != nil,
			Provider:                    entry.Provider,
			Mode:                        entry.Mode,
			SupportsPromptCaching:       entry.SupportsPromptCaching,
			SupportsServiceTier:         entry.SupportsServiceTier,
			SupportedModalities:         entry.SupportedModalities,
			SupportedOutputModalities:   entry.SupportedOutputModalities,
			SupportsVision:              entry.SupportsVision,
			SupportsAudioInput:          entry.SupportsAudioInput,
			SupportsAudioOutput:         entry.SupportsAudioOutput,
			SupportsVideoInput:          entry.SupportsVideoInput,
			TokenPricingAbsent:          entry.InputCostPerToken == nil && entry.OutputCostPerToken == nil,
		}
		// models.dev 缺失任一文本价格桶时保持未定价，图片报价不能补成免费文本输出。
		if entry.Source == "models.dev" && (entry.InputCostPerToken == nil || entry.OutputCostPerToken == nil) {
			pricing.TokenPricingAbsent = true
		}
		// 保持原字段优先，兼容部分厂商使用的输入模态字段名。
		if len(pricing.SupportedModalities) == 0 {
			pricing.SupportedModalities = entry.SupportedInputModalities
		}

		if entry.InputCostPerToken != nil {
			pricing.InputCostPerToken = *entry.InputCostPerToken
		}
		if entry.InputCostPerTokenPriority != nil {
			pricing.InputCostPerTokenPriority = *entry.InputCostPerTokenPriority
		}
		if entry.OutputCostPerToken != nil {
			pricing.OutputCostPerToken = *entry.OutputCostPerToken
		}
		if entry.OutputCostPerTokenPriority != nil {
			pricing.OutputCostPerTokenPriority = *entry.OutputCostPerTokenPriority
		}
		if entry.CacheCreationInputTokenCost != nil {
			pricing.CacheCreationInputTokenCost = *entry.CacheCreationInputTokenCost
		}
		if entry.CacheCreationInputTokenCostPriority != nil {
			pricing.CacheCreationInputTokenCostPriority = *entry.CacheCreationInputTokenCostPriority
		}
		if entry.CacheCreationInputTokenCostAbove1hr != nil {
			pricing.CacheCreationInputTokenCostAbove1hr = *entry.CacheCreationInputTokenCostAbove1hr
		}
		if entry.CacheReadInputTokenCost != nil {
			pricing.CacheReadInputTokenCost = *entry.CacheReadInputTokenCost
		}
		if entry.CacheReadInputTokenCostPriority != nil {
			pricing.CacheReadInputTokenCostPriority = *entry.CacheReadInputTokenCostPriority
		}
		if entry.LongContextInputTokenThreshold != nil {
			pricing.LongContextInputTokenThreshold = *entry.LongContextInputTokenThreshold
		}
		if entry.LongContextInputCostMultiplier != nil {
			pricing.LongContextInputCostMultiplier = *entry.LongContextInputCostMultiplier
		}
		if entry.LongContextOutputCostMultiplier != nil {
			pricing.LongContextOutputCostMultiplier = *entry.LongContextOutputCostMultiplier
		}
		if entry.OutputCostPerImage != nil {
			pricing.OutputCostPerImage = *entry.OutputCostPerImage
		}
		if entry.OutputCostPerImageToken != nil {
			pricing.OutputCostPerImageToken = *entry.OutputCostPerImageToken
		}
		if entry.InputCostPerImageToken != nil {
			pricing.InputCostPerImageToken = *entry.InputCostPerImageToken
		}

		if err := deriveCatalogCachePrices(pricing); err != nil {
			invalidEntries = append(invalidEntries, fmt.Sprintf("%s: %v", modelName, err))
			continue
		}
		// 显式 long_context 字段（包括显式 0）优先于目录阶梯和 above 绝对价字段。
		hasExplicitLongContext := entry.LongContextInputTokenThreshold != nil ||
			entry.LongContextInputCostMultiplier != nil ||
			entry.LongContextOutputCostMultiplier != nil
		if hasExplicitLongContext {
			// 只保留兼容阈值与倍率，避免新的绝对阶梯绕过管理员的关闭或改价。
			pricing.ContextPrices = nil
		} else {
			DeriveLongContextFromAboveTierFields(rawEntry, pricing)
			if IsLopsidedLongContextLadder(pricing) {
				lopsidedLadders = append(lopsidedLadders, fmt.Sprintf("%s(input x%.2f, output x%.2f)", modelName,
					pricing.LongContextInputCostMultiplier, pricing.LongContextOutputCostMultiplier))
			}
		}
		if orphans := OrphanCacheTierFields(rawEntry); len(orphans) > 0 {
			orphanCacheTiers = append(orphanCacheTiers, modelName+"("+strings.Join(orphans, ",")+")")
		}

		result[modelName] = pricing
	}

	sort.Strings(invalidEntries)
	diagnostics := CatalogDiagnostics{Skipped: skipped, InvalidEntries: invalidEntries, OrphanCacheTiers: orphanCacheTiers, LopsidedLadders: lopsidedLadders}

	if len(result) == 0 {
		return nil, diagnostics, ErrNoPricingEntries
	}

	return result, diagnostics, nil
}
