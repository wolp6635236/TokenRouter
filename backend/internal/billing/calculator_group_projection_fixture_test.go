package billing_test

import (
	"context"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/billing/pricing"
	"github.com/TokenFlux/TokenRouter/internal/routing"
)

// settingsPrices 为计费测试提供共享配置输入，匹配和计算仍由生产实现执行。
type settingsPrices struct {
	settings pricing.BillingSettings
	cards    []routing.ModelPricingEntry
}

func (s *settingsPrices) GetEffectiveBillingSettings(context.Context, int64) pricing.BillingSettings {
	return s.settings.Clone()
}

func (s *settingsPrices) GetEffectiveConfigModelPricing(_ context.Context, _ int64, model string) *pricing.ModelPricingEntry {
	return pricing.MatchPriceCard(s.cards, model)
}

func settingsResolver(calculator *billing.Calculator, settings pricing.BillingSettings, cards []routing.ModelPricingEntry) (*billing.PriceResolver, *settingsPrices) {
	source := &settingsPrices{settings: settings, cards: cards}
	return billing.NewPriceResolver(source, calculator, nil, nil), source
}
