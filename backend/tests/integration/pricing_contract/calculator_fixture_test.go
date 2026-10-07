package pricingcontract

import (
	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/billing/pricing"
	"github.com/TokenFlux/TokenRouter/internal/billing/testkit"
	"github.com/TokenFlux/TokenRouter/internal/modelcatalog/provider"
)

// newCalculator 使用测试目录构造计价器。
func newCalculator(catalog *provider.Service) *billing.Calculator {
	return newCalculatorWithPrices(catalog, nil)
}

func newCalculatorWithPrices(catalog *provider.Service, prices map[string]*pricing.ModelPricing) *billing.Calculator {
	return testkit.Calculator(catalog, prices)
}
