package testkit

import (
	_ "embed"
	"encoding/json"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/billing/pricing"
	billingadapter "github.com/TokenFlux/TokenRouter/internal/billing/provider"
	catalogprovider "github.com/TokenFlux/TokenRouter/internal/modelcatalog/provider"
	"github.com/TokenFlux/TokenRouter/internal/pkg/timezone"
)

// historicalPriceData 仅冻结清理前的测试输入，不参与应用装配或运行时查价。
//
//go:embed testdata/historical_prices.json
var historicalPriceData []byte

// mediaPriceData 是媒体计算测试的固定输入，不属于运行时补充。
//
//go:embed testdata/media_prices.json
var mediaPriceData []byte

// HistoricalPrices 给回归测试提供显式目录夹具，调用方可以独立修改。
func HistoricalPrices() map[string]*pricing.ModelPricing {
	var prices map[string]*pricing.ModelPricing
	if err := json.Unmarshal(historicalPriceData, &prices); err != nil {
		panic(err)
	}
	return prices
}

// Calculator 为跨模块计费测试准备模型目录，并构造 billing.Calculator。
func Calculator(catalog *catalogprovider.Service, prices map[string]*pricing.ModelPricing) *billing.Calculator {
	useBaseline := prices == nil && catalog == nil
	if useBaseline {
		prices = HistoricalPrices()
	}
	if prices != nil {
		data := map[string]*pricing.CatalogModelPricing{}
		if catalog != nil {
			data = catalog.Snapshot().Data
		}
		if data == nil {
			data = map[string]*pricing.CatalogModelPricing{}
		}
		for name, price := range prices {
			if price == nil {
				continue
			}
			if _, exists := data[name]; exists {
				continue
			}
			data[name] = &pricing.CatalogModelPricing{
				InputCostPerToken: price.InputPricePerToken, OutputCostPerToken: price.OutputPricePerToken,
				CacheReadInputTokenCost: price.CacheReadPricePerToken, CacheCreationInputTokenCost: price.CacheCreationPricePerToken,
				InputCostPerTokenPriority: price.InputPricePerTokenPriority, OutputCostPerTokenPriority: price.OutputPricePerTokenPriority,
				CacheReadInputTokenCostPriority: price.CacheReadPricePerTokenPriority, CacheCreationInputTokenCostPriority: price.CacheCreationPricePerTokenPriority,
				CacheCreationInputTokenCostAbove1hr: price.CacheCreation1hPrice,
				CacheCreation1hPricePresent:         price.SupportsCacheBreakdown,
				SupportsServiceTier:                 price.SupportsServiceTier,
				LongContextInputTokenThreshold:      price.LongContextInputThreshold,
				LongContextInputCostMultiplier:      price.LongContextInputMultiplier, LongContextOutputCostMultiplier: price.LongContextOutputMultiplier,
				InputCostPerImageToken: price.ImageInputPricePerToken, OutputCostPerImageToken: price.ImageOutputPricePerToken,
				CatalogRules: pricing.CatalogRules{FastMultiplier: price.FastMultiplier, FlexMultiplier: price.FlexMultiplier, MaxReasoningEffortMultiplier: price.MaxReasoningEffortMultiplier, TimePricing: price.TimePricing},
			}
			if price.SupportsCacheBreakdown {
				data[name].CacheCreationInputTokenCost = price.CacheCreation5mPrice
			}
		}
		if useBaseline {
			entries, err := pricing.DecodeCatalogEntries(mediaPriceData)
			if err != nil {
				panic(err)
			}
			media, _, err := pricing.ParsePricingEntries(entries)
			if err != nil {
				panic(err)
			}
			for name, price := range media {
				data[name] = price
			}
		}
		catalog = catalogprovider.NewServiceFromSnapshot(catalogprovider.Options{}, nil, catalogprovider.Snapshot{Data: data})
	}
	var source billing.PriceCatalog
	if catalog != nil {
		source = catalog
	}
	return billing.NewCalculator(source, billing.CalculatorOptions{
		Now:          timezone.NewCalendar(time.Local).Now,
		LoadLocation: billingadapter.LoadPricingLocation,
	})
}
