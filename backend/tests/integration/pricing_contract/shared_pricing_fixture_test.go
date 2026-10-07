package pricingcontract

import (
	"context"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/billing/pricing"
	"github.com/TokenFlux/TokenRouter/internal/gateway/testkit"
	"github.com/TokenFlux/TokenRouter/internal/routing"
)

// configureBillingGroup 将请求分组与共享价卡分开注入，原收费断言继续经过生产解析器。
func configureBillingGroup(svc *testkit.Recording, group *routing.Group, settings pricing.BillingSettings, cards []routing.ModelPricingEntry) *routing.Group {
	source := configuredPrices{base: svc.Dependencies.Prices, groupID: group.ID, settings: settings, cards: cards}
	svc.Dependencies.Prices = billing.NewPriceResolver(source, svc.Dependencies.Calculator, nil, nil)
	return group
}

type configuredPrices struct {
	base     *billing.PriceResolver
	groupID  int64
	settings pricing.BillingSettings
	cards    []routing.ModelPricingEntry
}

func (s configuredPrices) GetEffectiveBillingSettings(ctx context.Context, id int64) pricing.BillingSettings {
	if id == s.groupID {
		return s.settings.Clone()
	}
	return s.base.BillingSettings(ctx, &id)
}

func (s configuredPrices) GetEffectiveConfigModelPricing(ctx context.Context, id int64, model string) *pricing.ModelPricingEntry {
	if id == s.groupID {
		if card := pricing.MatchPriceCard(s.cards, model); card != nil {
			return card
		}
	}
	if s.base != nil {
		return s.base.LookupConfigPricingNormalized(ctx, id, model)
	}
	return nil
}
