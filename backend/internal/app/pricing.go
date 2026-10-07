package app

import (
	"context"
	"log/slog"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	billingadapter "github.com/TokenFlux/TokenRouter/internal/billing/provider"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/provider/modelidentity"
	catalogprovider "github.com/TokenFlux/TokenRouter/internal/modelcatalog/provider"
	"github.com/TokenFlux/TokenRouter/internal/pkg/timezone"
	"github.com/TokenFlux/TokenRouter/internal/routing"
)

// provideBillingCalculator 构造使用模型目录和应用时钟的共享计算器。
func provideBillingCalculator(catalog *catalogprovider.Service, calendar timezone.Calendar) *billing.Calculator {
	return billing.NewCalculator(catalog, billing.CalculatorOptions{Now: calendar.Now, LoadLocation: billingadapter.LoadPricingLocation})
}

func provideBillingPriceResolver(modelConfigs *routing.PricingConfigService, calculator *billing.Calculator) *billing.PriceResolver {
	return billing.NewPriceResolver(modelConfigs, calculator, modelidentity.Identity, func(model string, err error) {
		slog.DebugContext(context.Background(), "model catalog pricing unavailable", "model", model, "error", err)
	}, gatewayprovider.ProviderStatsSource{Service: modelConfigs})
}
