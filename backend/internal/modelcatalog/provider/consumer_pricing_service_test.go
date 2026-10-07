package provider

import (
	"encoding/json"
	"testing"
	"time"

	billingpricing "github.com/TokenFlux/TokenRouter/internal/billing/pricing"

	"github.com/stretchr/testify/require"
)

func TestPricingSchedulerBlankRemoteURLDoesNotStart(t *testing.T) {
	svc := NewService(Options{RemoteURL: "  \t  "}, nil)
	defer svc.Stop()

	svc.startUpdateScheduler()
	done := make(chan struct{})
	go func() {
		svc.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("blank remote URL must not start scheduler")
	}
}

func TestPricingNonEmptyInvalidRemoteURLStillReturnsValidationError(t *testing.T) {
	svc := NewService(Options{
		RemoteURL: "://invalid",
		DataDir:   t.TempDir(),
	}, nil)

	err := svc.ForceUpdate()

	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid pricing url")
}

func TestParsePricingData_ParsesPriorityAndServiceTierFields(t *testing.T) {
	body := []byte(`{
		"gpt-5.4": {
			"input_cost_per_token": 0.0000025,
			"input_cost_per_token_priority": 0.000005,
			"output_cost_per_token": 0.000015,
			"output_cost_per_token_priority": 0.00003,
			"cache_creation_input_token_cost": 0.0000025,
			"cache_creation_input_token_cost_priority": 0.000005,
			"cache_read_input_token_cost": 0.00000025,
			"cache_read_input_token_cost_priority": 0.0000005,
			"long_context_input_token_threshold": 272000,
			"long_context_input_cost_multiplier": 2,
			"long_context_output_cost_multiplier": 1.5,
			"supports_service_tier": true,
			"supports_prompt_caching": true,
			"provider": "openai",
			"mode": "chat"
		}
	}`)

	data, err := parsePricingFixture(body)
	require.NoError(t, err)
	pricing := data["gpt-5.4"]
	require.NotNil(t, pricing)
	require.InDelta(t, 5e-6, pricing.InputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 3e-5, pricing.OutputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 5e-6, pricing.CacheCreationInputTokenCostPriority, 1e-12)
	require.InDelta(t, 5e-7, pricing.CacheReadInputTokenCostPriority, 1e-12)
	require.Equal(t, 272000, pricing.LongContextInputTokenThreshold)
	require.InDelta(t, 2.0, pricing.LongContextInputCostMultiplier, 1e-12)
	require.InDelta(t, 1.5, pricing.LongContextOutputCostMultiplier, 1e-12)
	require.True(t, pricing.SupportsServiceTier)
}

func TestParsePricingData_ParsesImageInputTokenPrice(t *testing.T) {
	pricingSvc := newModelCatalogFixture(modelCatalogFixture{})
	data, err := parsePricingFixture([]byte(`{
		"gpt-image-2": {
			"input_cost_per_token": 0.000005,
			"input_cost_per_image_token": 0.000008,
			"output_cost_per_token": 0.00001,
			"output_cost_per_image_token": 0.00003,
			"provider": "openai",
			"mode": "image_generation"
		}
	}`))
	require.NoError(t, err)
	parsed := data["gpt-image-2"]
	require.NotNil(t, parsed)
	require.InDelta(t, 8e-6, parsed.InputCostPerImageToken, 1e-12)

	setPricingFixtureData(pricingSvc, data)
	billingSvc := newBillingFixture(pricingSvc)
	pricing, err := billingSvc.GetModelPricing("gpt-image-2")
	require.NoError(t, err)
	require.InDelta(t, 8e-6, pricing.ImageInputPricePerToken, 1e-12)
}

// gpt56LadderCatalogJSON 用于验证目录驱动的 GPT-5.6 阶梯；fallback 不再隐式补阶梯。
const gpt56LadderCatalogJSON = `{
	"gpt-5.6-sol": {"provider": "openai", "mode": "chat", "cache_write_multiplier": 1.25, "flex_multiplier": 0.5,
		"input_cost_per_token": 5e-06, "input_cost_per_token_priority": 1e-05,
		"output_cost_per_token": 3e-05, "output_cost_per_token_priority": 6e-05,
		"cache_read_input_token_cost": 5e-07, "cache_read_input_token_cost_priority": 1e-06,
		"input_cost_per_token_above_272k_tokens": 1e-05,
		"output_cost_per_token_above_272k_tokens": 4.5e-05,
		"cache_read_input_token_cost_above_272k_tokens": 1e-06},
	"gpt-5.6-terra": {"provider": "openai", "mode": "chat", "cache_write_multiplier": 1.25, "flex_multiplier": 0.5,
		"input_cost_per_token": 2e-06, "input_cost_per_token_priority": 4e-06,
		"output_cost_per_token": 1.2e-05, "output_cost_per_token_priority": 2.4e-05,
		"cache_read_input_token_cost": 2e-07, "cache_read_input_token_cost_priority": 4e-07,
		"input_cost_per_token_above_272k_tokens": 4e-06,
		"output_cost_per_token_above_272k_tokens": 1.8e-05,
		"cache_read_input_token_cost_above_272k_tokens": 4e-07},
	"gpt-5.6-luna": {"provider": "openai", "mode": "chat", "cache_write_multiplier": 1.25, "flex_multiplier": 0.5,
		"input_cost_per_token": 2e-07, "input_cost_per_token_priority": 4e-07,
		"output_cost_per_token": 1.2e-06, "output_cost_per_token_priority": 2.4e-06,
		"cache_read_input_token_cost": 2e-08, "cache_read_input_token_cost_priority": 4e-08,
		"input_cost_per_token_above_272k_tokens": 4e-07,
		"output_cost_per_token_above_272k_tokens": 1.8e-06,
		"cache_read_input_token_cost_above_272k_tokens": 4e-08}
}`

func TestBillingService_GPT56UsesLongContextPricingAcrossModelsAndTiers(t *testing.T) {
	models := []struct {
		name               string
		input, cached      float64
		cacheWrite, output float64
	}{
		{name: "gpt-5.6-sol", input: 5e-6, cached: 0.5e-6, cacheWrite: 6.25e-6, output: 30e-6},
		{name: "gpt-5.6-terra", input: 2e-6, cached: 0.2e-6, cacheWrite: 2.5e-6, output: 12e-6},
		{name: "gpt-5.6-luna", input: 0.2e-6, cached: 0.02e-6, cacheWrite: 0.25e-6, output: 1.2e-6},
	}
	tiers := []struct {
		name       string
		priceScale float64
	}{
		{name: "standard", priceScale: 1},
		{name: "priority", priceScale: 2},
		{name: "flex", priceScale: 0.5},
	}
	tokens := billingpricing.UsageTokens{
		InputTokens:         100000,
		CacheCreationTokens: 100000,
		CacheReadTokens:     73000,
		OutputTokens:        10,
	}

	for _, model := range models {
		for _, tier := range tiers {
			t.Run(model.name+"/"+tier.name, func(t *testing.T) {
				svc := newBillingFixture(newStubCatalogFromJSON(t, gpt56LadderCatalogJSON))
				serviceTier := ""
				if tier.name != "standard" {
					serviceTier = tier.name
				}
				cost, err := svc.CalculateCostWithServiceTier(model.name, tokens, 1, serviceTier)
				require.NoError(t, err)
				require.InDelta(t, float64(tokens.InputTokens)*model.input*tier.priceScale*2, cost.InputCost, 1e-12)
				require.InDelta(t, float64(tokens.CacheCreationTokens)*model.cacheWrite*tier.priceScale*2, cost.CacheCreationCost, 1e-12)
				require.InDelta(t, float64(tokens.CacheReadTokens)*model.cached*tier.priceScale*2, cost.CacheReadCost, 1e-12)
				require.InDelta(t, float64(tokens.OutputTokens)*model.output*tier.priceScale*1.5, cost.OutputCost, 1e-12)
			})
		}
	}
}

func TestBillingService_GPT56LongContextBoundaryIsExclusive(t *testing.T) {
	svc := newBillingFixture(newStubCatalogFromJSON(t, gpt56LadderCatalogJSON))
	tokens := billingpricing.UsageTokens{InputTokens: 100000, CacheCreationTokens: 100000, CacheReadTokens: 72000, OutputTokens: 10}

	cost, err := svc.CalculateCost("gpt-5.6-sol", tokens, 1)
	require.NoError(t, err)
	require.InDelta(t, 100000*5e-6, cost.InputCost, 1e-12)
	require.InDelta(t, 100000*6.25e-6, cost.CacheCreationCost, 1e-12)
	require.InDelta(t, 72000*0.5e-6, cost.CacheReadCost, 1e-12)
	require.InDelta(t, 10*30e-6, cost.OutputCost, 1e-12)
}

// TestCatalogService_ExplicitCatalogEntryDoesNotRedirectToSol 验证外部目录中的显式自定义条目不会再被代码重定向，也不会自动附加内置产品规则。
func TestCatalogService_ExplicitCatalogEntryDoesNotRedirectToSol(t *testing.T) {
	pricingSvc := newModelCatalogFixture(modelCatalogFixture{pricingData: map[string]*billingpricing.CatalogModelPricing{
		"gpt-5.6":       {InputCostPerToken: 4e-6},
		"gpt-5.6-sol":   {InputCostPerToken: 5e-6},
		"gpt-5.6-terra": {InputCostPerToken: 2e-6},
		"gpt-5.6-luna":  {InputCostPerToken: 0.2e-6},
		"gpt-5.4":       {InputCostPerToken: 2.5e-6},
	}})

	for i := 0; i < 100; i++ {
		for _, alias := range []string{"gpt-5.6"} {
			pricing := pricingSvc.GetModelPricing(alias)
			require.NotNil(t, pricing)
			require.InDelta(t, 4e-6, pricing.InputCostPerToken, 1e-12, "iteration=%d alias=%s", i, alias)
		}
	}

	billingSvc := newBillingFixture(pricingSvc)
	for _, alias := range []string{"gpt-5.6"} {
		pricing, err := billingSvc.GetModelPricing(alias)
		require.NoError(t, err)
		require.InDelta(t, 4e-6, pricing.InputPricePerToken, 1e-12)
		require.Zero(t, pricing.CacheCreationPricePerToken)
	}
}

func TestDefaultPricingIncludesModelsDevGPT56Rates(t *testing.T) {
	pricingSvc := newOfflinePricingFixture(t)
	billingSvc := newBillingFixture(pricingSvc)

	tests := []struct {
		model                                                             string
		input, cached, cacheWrite, output                                 float64
		inputPriority, cachedPriority, cacheWritePriority, outputPriority float64
	}{
		{model: "gpt-5.6-sol", input: 4e-6, cached: 0.4e-6, cacheWrite: 5e-6, output: 20e-6, inputPriority: 8e-6, cachedPriority: 0.8e-6, cacheWritePriority: 10e-6, outputPriority: 40e-6},
		{model: "gpt-5.6-terra", input: 2e-6, cached: 0.2e-6, cacheWrite: 2.5e-6, output: 12e-6, inputPriority: 4e-6, cachedPriority: 0.4e-6, cacheWritePriority: 5e-6, outputPriority: 24e-6},
		{model: "gpt-5.6-luna", input: 0.2e-6, cached: 0.02e-6, cacheWrite: 0.25e-6, output: 1.2e-6, inputPriority: 0.4e-6, cachedPriority: 0.04e-6, cacheWritePriority: 0.5e-6, outputPriority: 2.4e-6},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			pricing, err := billingSvc.GetModelPricing(tt.model)
			require.NoError(t, err)
			require.InDelta(t, tt.input, pricing.InputPricePerToken, 1e-12)
			require.InDelta(t, tt.cached, pricing.CacheReadPricePerToken, 1e-12)
			require.InDelta(t, tt.cacheWrite, pricing.CacheCreationPricePerToken, 1e-12)
			require.InDelta(t, tt.output, pricing.OutputPricePerToken, 1e-12)
			require.InDelta(t, tt.inputPriority, pricing.InputPricePerTokenPriority, 1e-12)
			require.InDelta(t, tt.cachedPriority, pricing.CacheReadPricePerTokenPriority, 1e-12)
			require.InDelta(t, tt.cacheWritePriority, pricing.CacheCreationPricePerTokenPriority, 1e-12)
			require.InDelta(t, tt.outputPriority, pricing.OutputPricePerTokenPriority, 1e-12)
			require.Len(t, pricing.ContextPrices, 1)
			require.Equal(t, 272000, pricing.ContextPrices[0].Threshold)
			require.InDelta(t, tt.input*2, pricing.ContextPrices[0].Pricing.InputPricePerToken, 1e-12)
			require.InDelta(t, tt.output*1.5, pricing.ContextPrices[0].Pricing.OutputPricePerToken, 1e-12)
		})
	}
}

func TestDefaultPricingIncludesModelsDevGPT6AstraRates(t *testing.T) {
	pricingSvc := newOfflinePricingFixture(t)
	billingSvc := newBillingFixture(pricingSvc)

	pricing, err := billingSvc.GetModelPricing("gpt-6-astra")
	require.NoError(t, err)
	require.InDelta(t, 10e-6, pricing.InputPricePerToken, 1e-12)
	require.InDelta(t, 1e-6, pricing.CacheReadPricePerToken, 1e-12)
	require.InDelta(t, 12.5e-6, pricing.CacheCreationPricePerToken, 1e-12)
	require.InDelta(t, 50e-6, pricing.OutputPricePerToken, 1e-12)
	require.Len(t, pricing.ContextPrices, 1)
	require.Equal(t, 272000, pricing.ContextPrices[0].Threshold)
	require.InDelta(t, 20e-6, pricing.ContextPrices[0].Pricing.InputPricePerToken, 1e-12)
	require.InDelta(t, 75e-6, pricing.ContextPrices[0].Pricing.OutputPricePerToken, 1e-12)
	inputModalities, outputModalities := pricingSvc.GetModelModalities("gpt-6-astra")
	require.Equal(t, []string{"text", "image"}, inputModalities)
	require.Equal(t, []string{"text"}, outputModalities)
}

func TestParsePricingData_KeepsImageOnlyPricing(t *testing.T) {
	body := []byte(`{
		"image-only-model": {
			"output_cost_per_image": 0.034,
			"provider": "vertex_ai-language-models",
			"mode": "image_generation"
		}
	}`)

	data, err := parsePricingFixture(body)
	require.NoError(t, err)
	pricing := data["image-only-model"]
	require.NotNil(t, pricing)
	require.InDelta(t, 0.034, pricing.OutputCostPerImage, 1e-12)
	require.Equal(t, "image_generation", pricing.Mode)
	// 仅有图片价的条目必须标记 token 价缺失，供 token 计费路径 fail-closed。
	require.True(t, pricing.TokenPricingAbsent)
}

func TestBillingService_GetModelPricing_FailsClosedForImageOnlyEntries(t *testing.T) {
	pricingSvc := newModelCatalogFixture(modelCatalogFixture{})
	data, err := parsePricingFixture([]byte(`{
		"imagen-9.0-generate": {
			"output_cost_per_image": 0.04,
			"provider": "vertex_ai-image-models",
			"mode": "image_generation"
		},
		"gemini-image-with-token-price": {
			"input_cost_per_token": 0.0,
			"output_cost_per_token": 0.0,
			"output_cost_per_image": 0.034,
			"provider": "vertex_ai-language-models",
			"mode": "image_generation"
		}
	}`))
	require.NoError(t, err)
	setPricingFixtureData(pricingSvc, data)
	billingSvc := newBillingFixture(pricingSvc)

	// image-only 条目不得进入 token 计费（否则 token 流量按 $0 计费），
	// 必须落到 fallback / ErrModelPricingUnavailable 的 fail-closed 路径。
	_, err = billingSvc.GetModelPricing("imagen-9.0-generate")
	require.ErrorIs(t, err, billingpricing.ErrModelPricingUnavailable)

	// 显式 0 token 价的免费条目保持历史行为：正常返回。
	pricing, err := billingSvc.GetModelPricing("gemini-image-with-token-price")
	require.NoError(t, err)
	require.Zero(t, pricing.InputPricePerToken)

	// 图片计费路径不受影响：仍能读到 image-only 条目的图片单价。
	raw := pricingSvc.GetModelPricing("imagen-9.0-generate")
	require.NotNil(t, raw)
	require.InDelta(t, 0.04, raw.OutputCostPerImage, 1e-12)
}

// TestBillingService_GetDisplayPricing_ChatImageMetadataKeepsTokenMode 验证聊天模型携带按图元数据时仍展示 token 价格。
func TestBillingService_GetDisplayPricing_ChatImageMetadataKeepsTokenMode(t *testing.T) {
	pricingSvc := newModelCatalogFixture(modelCatalogFixture{pricingData: map[string]*billingpricing.CatalogModelPricing{
		"gemini-3.1-pro-high": {
			InputCostPerToken:  2e-6,
			OutputCostPerToken: 12e-6,
			OutputCostPerImage: 0.00012,
			Mode:               "chat",
		},
		"gemini-3.1-flash-image": {
			OutputCostPerImage: 0.0672,
			Mode:               "image_generation",
		},
	}})
	billingSvc := newBillingFixture(pricingSvc)

	// 聊天模型必须优先展示 token 价格，不能被辅助的按图字段覆盖。
	chatPricing := billingSvc.DisplayPricing("gemini-3.1-pro-high", 8)
	require.Equal(t, "token", chatPricing.PricingMode)
	require.Equal(t, "priced", chatPricing.PriceStatus)
	require.InDelta(t, 16e-6, chatPricing.InputPricePerToken, 1e-12)
	require.InDelta(t, 96e-6, chatPricing.OutputPricePerToken, 1e-12)

	// 明确标记为图片生成的模型仍须沿用按图展示路径。
	imagePricing := billingSvc.DisplayPricing("gemini-3.1-flash-image", 2)
	require.Equal(t, "image", imagePricing.PricingMode)
	require.Equal(t, "priced", imagePricing.PriceStatus)
	require.InDelta(t, 0.1344, imagePricing.ImagePrice1K, 1e-12)
}

// TestCatalogService_MergesFallbackOnlyModels 验证补充文件新增模型但不替换远程 token 报价。
func TestCatalogService_MergesFallbackOnlyModels(t *testing.T) {
	svc := newHotReloadCatalog(t, `{"remote-model":{"input_cost_per_token":9,"output_cost_per_token":9},"local-model":{"input_cost_per_token":0.000004,"output_cost_per_token":0.000008}}`)
	require.InDelta(t, 1e-6, svc.GetModelPricing("remote-model").InputCostPerToken, 1e-12)
	require.InDelta(t, 4e-6, svc.GetModelPricing("local-model").InputCostPerToken, 1e-12)
}

func TestGetModelPricing_Gpt53CodexSparkUsesGpt51CodexPricing(t *testing.T) {
	sparkPricing := &billingpricing.CatalogModelPricing{InputCostPerToken: 1}
	gpt53Pricing := &billingpricing.CatalogModelPricing{InputCostPerToken: 9}

	svc := newModelCatalogFixture(modelCatalogFixture{
		pricingData: map[string]*billingpricing.CatalogModelPricing{
			"gpt-5.1-codex": sparkPricing,
			"gpt-5.3":       gpt53Pricing,
		},
	})

	got := svc.GetModelPricing("gpt-5.3-codex-spark")
	require.Nil(t, got)
}

func TestGetModelPricing_Gpt53CodexFallbackStillUsesGpt52Codex(t *testing.T) {
	gpt52CodexPricing := &billingpricing.CatalogModelPricing{InputCostPerToken: 2}

	svc := newModelCatalogFixture(modelCatalogFixture{
		pricingData: map[string]*billingpricing.CatalogModelPricing{
			"gpt-5.2-codex": gpt52CodexPricing,
		},
	})

	got := svc.GetModelPricing("gpt-5.3-codex")
	require.Nil(t, got)
}

func TestGetModelPricing_OpenAIFallbackMatchedLoggedAsInfo(t *testing.T) {
	logSink, restore := captureStructuredLog(t)
	defer restore()

	gpt52CodexPricing := &billingpricing.CatalogModelPricing{InputCostPerToken: 2}
	svc := newModelCatalogFixture(modelCatalogFixture{
		pricingData: map[string]*billingpricing.CatalogModelPricing{
			"gpt-5.2-codex": gpt52CodexPricing,
		},
	})

	got := svc.GetModelPricing("gpt-5.3-codex")
	require.Nil(t, got)

	require.False(t, logSink.ContainsMessageAtLevel("[Pricing] OpenAI fallback matched gpt-5.3-codex -> gpt-5.2-codex", "info"))
	require.False(t, logSink.ContainsMessageAtLevel("[Pricing] OpenAI fallback matched gpt-5.3-codex -> gpt-5.2-codex", "warn"))
}

func TestGetModelPricing_UnknownCompactAliasIsUnpriced(t *testing.T) {
	svc := newModelCatalogFixture(modelCatalogFixture{pricingData: map[string]*billingpricing.CatalogModelPricing{}})
	for _, model := range []string{"openai/gpt5.5", "gpt-5.5-openai-compact", "gpt-5.5-preview"} {
		require.Nil(t, svc.GetModelPricing(model))
	}
}

func TestCatalogService_Gemini36FlashThinkingTiersUseBasePricing(t *testing.T) {
	basePricing := &billingpricing.CatalogModelPricing{
		InputCostPerToken:       1.5e-6,
		OutputCostPerToken:      7.5e-6,
		CacheReadInputTokenCost: 0.15e-6,
	}
	svc := newModelCatalogFixture(modelCatalogFixture{pricingData: map[string]*billingpricing.CatalogModelPricing{
		"gemini-3.6-flash": basePricing,
	}})

	for _, model := range []string{
		"gemini-3.6-flash",
		"gemini-3.6-flash-high",
		"gemini-3.6-flash-low",
		"gemini-3.6-flash-medium",
		"gemini-3.6-flash-tiered",
	} {
		t.Run(model, func(t *testing.T) {
			if model == "gemini-3.6-flash" || model == "gemini-3.5-flash" {
				require.Same(t, basePricing, svc.GetModelPricing(model))
			} else {
				require.Nil(t, svc.GetModelPricing(model))
			}
		})
	}
}

// TestCatalogService_Gemini35FlashThinkingTiersUseBasePricing 验证后缀型号不会复用基础模型价格。
func TestCatalogService_Gemini35FlashThinkingTiersUseBasePricing(t *testing.T) {
	basePricing := &billingpricing.CatalogModelPricing{
		InputCostPerToken:       1.5e-6,
		OutputCostPerToken:      9e-6,
		CacheReadInputTokenCost: 0.15e-6,
	}
	svc := newModelCatalogFixture(modelCatalogFixture{pricingData: map[string]*billingpricing.CatalogModelPricing{
		"gemini-3.5-flash": basePricing,
	}})

	for _, model := range []string{
		"gemini-3.5-flash",
		"gemini-3.5-flash-high",
		"gemini-3.5-flash-low",
		"gemini-3.5-flash-medium",
		"gemini-3.5-flash-tiered",
	} {
		t.Run(model, func(t *testing.T) {
			if model == "gemini-3.6-flash" || model == "gemini-3.5-flash" {
				require.Same(t, basePricing, svc.GetModelPricing(model))
			} else {
				require.Nil(t, svc.GetModelPricing(model))
			}
		})
	}
}

func TestCatalogService_Gemini36FlashTierSpecificPricingTakesPrecedence(t *testing.T) {
	basePricing := &billingpricing.CatalogModelPricing{InputCostPerToken: 1.5e-6}
	tierPricing := &billingpricing.CatalogModelPricing{InputCostPerToken: 2e-6}
	svc := newModelCatalogFixture(modelCatalogFixture{pricingData: map[string]*billingpricing.CatalogModelPricing{
		"gemini-3.6-flash":     basePricing,
		"gemini-3.6-flash-low": tierPricing,
	}})

	require.Same(t, tierPricing, svc.GetModelPricing("models/gemini-3.6-flash-low"))
}

// TestCatalogService_Gemini35FlashTierSpecificPricingTakesPrecedence 验证未来出现档位专属价格时优先精确匹配。
func TestCatalogService_Gemini35FlashTierSpecificPricingTakesPrecedence(t *testing.T) {
	basePricing := &billingpricing.CatalogModelPricing{InputCostPerToken: 1.5e-6}
	tierPricing := &billingpricing.CatalogModelPricing{InputCostPerToken: 2e-6}
	svc := newModelCatalogFixture(modelCatalogFixture{pricingData: map[string]*billingpricing.CatalogModelPricing{
		"gemini-3.5-flash":     basePricing,
		"gemini-3.5-flash-low": tierPricing,
	}})

	require.Same(t, tierPricing, svc.GetModelPricing("models/gemini-3.5-flash-low"))
}

// TestBillingService_Gemini35FlashThinkingTierFallbacksAreBillable 验证远程价格不可用时各档位仍能安全计费。
func TestBillingService_Gemini35FlashTiersRequireOwnPricing(t *testing.T) {
	svc := newBillingFixture(nil)
	for _, model := range []string{"gemini-3.5-flash-high", "gemini-3.5-flash-low", "gemini-3.5-flash-medium", "gemini-3.5-flash-tiered"} {
		price, err := svc.GetModelPricing(model)
		require.ErrorIs(t, err, billingpricing.ErrModelPricingUnavailable)
		require.Nil(t, price)
	}
}

func TestBillingService_Gemini36FlashTiersRequireOwnPricing(t *testing.T) {
	svc := newBillingFixture(nil)
	for _, model := range []string{"gemini-3.6-flash-high", "gemini-3.6-flash-low", "gemini-3.6-flash-medium", "gemini-3.6-flash-tiered"} {
		price, err := svc.GetModelPricing(model)
		require.ErrorIs(t, err, billingpricing.ErrModelPricingUnavailable)
		require.Nil(t, price)
	}
}

func TestDefaultPricingIncludesGemini36FlashRates(t *testing.T) {
	svc := NewService(Options{DataDir: t.TempDir()}, nil)
	require.NoError(t, svc.Initialize())
	require.NotNil(t, svc.GetModelPricing("gemini-3.6-flash"))
	// 后缀是否有价取决于目录是否存在完整 ID，不从基名生成。
	for _, suffix := range []string{"high", "low", "medium", "tiered"} {
		model := "gemini-3.6-flash-" + suffix
		if _, exists := svc.pricingData[model]; !exists {
			require.Nil(t, svc.GetModelPricing(model))
		}
	}
}

// TestDefaultPricingIncludesGemini35FlashRates 验证内置快照只为已登记的完整型号提供价格。
func TestDefaultPricingIncludesGemini35FlashRates(t *testing.T) {
	svc := NewService(Options{DataDir: t.TempDir()}, nil)
	require.NoError(t, svc.Initialize())
	require.NotNil(t, svc.GetModelPricing("gemini-3.5-flash"))
	// 后缀是否有价取决于目录是否存在完整 ID，不从基名生成。
	for _, suffix := range []string{"high", "low", "medium", "tiered"} {
		model := "gemini-3.5-flash-" + suffix
		if _, exists := svc.pricingData[model]; !exists {
			require.Nil(t, svc.GetModelPricing(model))
		}
	}
}

// TestDefaultCatalogRequiresConfiguredAutoReviewPricing 要求内部型号通过手动价卡或模型映射取得价格。
func TestDefaultCatalogRequiresConfiguredAutoReviewPricing(t *testing.T) {
	service := newOfflinePricingFixture(t)
	require.NotContains(t, service.Snapshot().Data, "codex-auto-review")
	require.Nil(t, service.GetModelPricing("codex-auto-review"))
}

func TestGetModelPricing_ImageModelDoesNotFallbackToTextModel(t *testing.T) {
	imagePricing := &billingpricing.CatalogModelPricing{InputCostPerToken: 3}
	textPricing := &billingpricing.CatalogModelPricing{InputCostPerToken: 9}

	svc := newModelCatalogFixture(modelCatalogFixture{
		pricingData: map[string]*billingpricing.CatalogModelPricing{
			"gpt-image-2": imagePricing,
			"gpt-5.4":     textPricing,
		},
	})

	got := svc.GetModelPricing("gpt-image-3")
	require.Nil(t, got)
}

func TestParsePricingData_PreservesPriorityAndServiceTierFields(t *testing.T) {
	raw := map[string]any{
		"gpt-5.4": map[string]any{
			"input_cost_per_token":                 2.5e-6,
			"input_cost_per_token_priority":        5e-6,
			"output_cost_per_token":                15e-6,
			"output_cost_per_token_priority":       30e-6,
			"cache_read_input_token_cost":          0.25e-6,
			"cache_read_input_token_cost_priority": 0.5e-6,
			"supports_service_tier":                true,
			"supports_prompt_caching":              true,
			"provider":                             "openai",
			"mode":                                 "chat",
		},
	}
	body, err := json.Marshal(raw)
	require.NoError(t, err)

	pricingMap, err := parsePricingFixture(body)
	require.NoError(t, err)

	pricing := pricingMap["gpt-5.4"]
	require.NotNil(t, pricing)
	require.InDelta(t, 2.5e-6, pricing.InputCostPerToken, 1e-12)
	require.InDelta(t, 5e-6, pricing.InputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 15e-6, pricing.OutputCostPerToken, 1e-12)
	require.InDelta(t, 30e-6, pricing.OutputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 0.25e-6, pricing.CacheReadInputTokenCost, 1e-12)
	require.InDelta(t, 0.5e-6, pricing.CacheReadInputTokenCostPriority, 1e-12)
	require.True(t, pricing.SupportsServiceTier)
}

func TestParsePricingData_PreservesServiceTierPriorityFields(t *testing.T) {
	pricingData, err := parsePricingFixture([]byte(`{
		"gpt-5.4": {
			"input_cost_per_token": 0.0000025,
			"input_cost_per_token_priority": 0.000005,
			"output_cost_per_token": 0.000015,
			"output_cost_per_token_priority": 0.00003,
			"cache_read_input_token_cost": 0.00000025,
			"cache_read_input_token_cost_priority": 0.0000005,
			"supports_service_tier": true,
			"provider": "openai",
			"mode": "chat"
		}
	}`))
	require.NoError(t, err)

	pricing := pricingData["gpt-5.4"]
	require.NotNil(t, pricing)
	require.InDelta(t, 0.0000025, pricing.InputCostPerToken, 1e-12)
	require.InDelta(t, 0.000005, pricing.InputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 0.000015, pricing.OutputCostPerToken, 1e-12)
	require.InDelta(t, 0.00003, pricing.OutputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 0.00000025, pricing.CacheReadInputTokenCost, 1e-12)
	require.InDelta(t, 0.0000005, pricing.CacheReadInputTokenCostPriority, 1e-12)
	require.True(t, pricing.SupportsServiceTier)
}

// ---------------------------------------------------------------------------
// ListModelNamesByProvider 模型列表测试
// ---------------------------------------------------------------------------

func TestListModelNamesByProvider_ReturnsMatchingModels(t *testing.T) {
	svc := newModelCatalogFixture(modelCatalogFixture{
		pricingData: map[string]*billingpricing.CatalogModelPricing{
			"claude-opus-4-5-20251101": {Provider: "anthropic", InputCostPerToken: 1.5e-5},
			"claude-sonnet-4-5":        {Provider: "anthropic", InputCostPerToken: 3e-6},
			"gpt-4o":                   {Provider: "openai", InputCostPerToken: 5e-6},
			"gemini-2.5-pro":           {Provider: "google", InputCostPerToken: 1.25e-6},
		},
	})

	got := svc.ListModelNamesByProvider("anthropic")
	require.ElementsMatch(t, []string{"claude-opus-4-5-20251101", "claude-sonnet-4-5"}, got)
	// 必须按字母序排序
	require.Equal(t, "claude-opus-4-5-20251101", got[0])
	require.Equal(t, "claude-sonnet-4-5", got[1])
}

func TestListModelNamesByProvider_CaseInsensitive(t *testing.T) {
	svc := newModelCatalogFixture(modelCatalogFixture{
		pricingData: map[string]*billingpricing.CatalogModelPricing{
			"gpt-4o": {Provider: "OpenAI", InputCostPerToken: 5e-6},
		},
	})

	got := svc.ListModelNamesByProvider("openai")
	require.Equal(t, []string{"gpt-4o"}, got)

	got2 := svc.ListModelNamesByProvider("OPENAI")
	require.Equal(t, []string{"gpt-4o"}, got2)
}

func TestListModelNamesByProvider_NoMatch(t *testing.T) {
	svc := newModelCatalogFixture(modelCatalogFixture{
		pricingData: map[string]*billingpricing.CatalogModelPricing{
			"gpt-4o": {Provider: "openai", InputCostPerToken: 5e-6},
		},
	})

	got := svc.ListModelNamesByProvider("anthropic")
	require.NotNil(t, got)
	require.Empty(t, got)
}

func TestListModelNamesByProvider_EmptyCatalog(t *testing.T) {
	svc := newModelCatalogFixture(modelCatalogFixture{
		pricingData: map[string]*billingpricing.CatalogModelPricing{},
	})

	got := svc.ListModelNamesByProvider("openai")
	require.NotNil(t, got)
	require.Empty(t, got)
}

func TestParsePricingData_ParsesModalityFields(t *testing.T) {
	data, err := parsePricingFixture([]byte(`{
		"gemini-2.5-flash": {
			"input_cost_per_token": 0.0000003,
			"output_cost_per_token": 0.0000025,
			"provider": "google",
			"mode": "chat",
			"supported_modalities": ["text", "image", "audio", "video"],
			"supported_output_modalities": ["text", "image"],
			"supports_vision": true,
			"supports_audio_input": true
		}
	}`))
	require.NoError(t, err)
	parsed := data["gemini-2.5-flash"]
	require.NotNil(t, parsed)
	require.Equal(t, []string{"text", "image", "audio", "video"}, parsed.SupportedModalities)
	require.Equal(t, []string{"text", "image"}, parsed.SupportedOutputModalities)
	require.True(t, parsed.SupportsVision)
	require.True(t, parsed.SupportsAudioInput)
	require.False(t, parsed.SupportsAudioOutput)
}

func TestGetModelModalities(t *testing.T) {
	tests := []struct {
		name    string
		model   string
		data    map[string]*billingpricing.CatalogModelPricing
		wantIn  []string
		wantOut []string
	}{
		{
			name:  "supported_modalities 优先且按固定顺序输出",
			model: "gemini-2.5-flash",
			data: map[string]*billingpricing.CatalogModelPricing{
				"gemini-2.5-flash": {
					Mode:                      "chat",
					SupportedModalities:       []string{"video", "text", "image"},
					SupportedOutputModalities: []string{"image", "text"},
				},
			},
			wantIn:  []string{"text", "image", "video"},
			wantOut: []string{"text", "image"},
		},
		{
			name:  "模态缺失时用 mode 兜底并用 supports_vision 补充图片输入",
			model: "gpt-5.5",
			data: map[string]*billingpricing.CatalogModelPricing{
				"gpt-5.5": {Mode: "chat", SupportsVision: true},
			},
			wantIn:  []string{"text", "image"},
			wantOut: []string{"text"},
		},
		{
			name:  "生图模型用图片输入价识别图生图能力",
			model: "gpt-image-2",
			data: map[string]*billingpricing.CatalogModelPricing{
				"gpt-image-2": {Mode: "image_generation", InputCostPerImageToken: 8e-6},
			},
			wantIn:  []string{"text", "image"},
			wantOut: []string{"image"},
		},
		{
			name:  "纯生图模型只有文字输入",
			model: "flux-schnell",
			data: map[string]*billingpricing.CatalogModelPricing{
				"flux-schnell": {Mode: "image_generation"},
			},
			wantIn:  []string{"text"},
			wantOut: []string{"image"},
		},
		{
			name:  "音频输入输出标记合成音频模态",
			model: "gpt-realtime",
			data: map[string]*billingpricing.CatalogModelPricing{
				"gpt-realtime": {Mode: "realtime", SupportsAudioInput: true, SupportsAudioOutput: true},
			},
			wantIn:  []string{"text", "audio"},
			wantOut: []string{"text", "audio"},
		},
		{
			name:  "非模态取值被过滤",
			model: "file-model",
			data: map[string]*billingpricing.CatalogModelPricing{
				"file-model": {
					Mode:                      "chat",
					SupportedModalities:       []string{"text", "file"},
					SupportedOutputModalities: []string{"text"},
				},
			},
			wantIn:  []string{"text"},
			wantOut: []string{"text"},
		},
		{
			name:  "版本写法不同不命中",
			model: "claude-opus-4-5-20251101",
			data: map[string]*billingpricing.CatalogModelPricing{
				"claude-opus-4.5-20251101": {Mode: "chat", SupportsVision: true},
			},
			wantIn:  nil,
			wantOut: nil,
		},
		{
			name:  "查不到时返回 nil",
			model: "unknown-model",
			data: map[string]*billingpricing.CatalogModelPricing{
				"claude-opus-4.5": {Mode: "chat"},
			},
			wantIn:  nil,
			wantOut: nil,
		},
		{
			name:  "不做系列模糊回退，避免新模型继承旧模型能力",
			model: "claude-opus-4.6",
			data: map[string]*billingpricing.CatalogModelPricing{
				"claude-opus-4.5": {Mode: "chat", SupportsVision: true},
			},
			wantIn:  nil,
			wantOut: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newModelCatalogFixture(modelCatalogFixture{pricingData: tt.data})
			in, out := svc.GetModelModalities(tt.model)
			require.Equal(t, tt.wantIn, in)
			require.Equal(t, tt.wantOut, out)
		})
	}
}
