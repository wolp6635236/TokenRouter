package pricingcontract

import (
	"context"
	"log/slog"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/creative"
	"github.com/TokenFlux/TokenRouter/internal/gateway/provider/modelidentity"
	"github.com/TokenFlux/TokenRouter/internal/routing"
)

func creativeGroupProjection(value *routing.Group) *creative.GroupView {
	if value == nil {
		return nil
	}
	return &creative.GroupView{ID: value.ID, Name: value.Name, IsExclusive: value.IsExclusive, AllowImageGeneration: value.AllowImageGeneration, Active: value.IsActive(), RateMultiplier: value.RateMultiplier, Operations: creative.OperationsForGroup(value.ResponsesImagePolicy != "" || value.ProtocolFallbacks != nil, value.AllowsClientProtocol)}
}

// creativePriceFixture 将目录和解析器接入测试，使用 billing 计算价格并处理缺价回退。
func creativePriceFixture(calculator *billing.Calculator, resolver *billing.PriceResolver) func(context.Context, *creative.GroupView, string, string) (float64, bool) {
	return func(ctx context.Context, group *creative.GroupView, model, size string) (float64, bool) {
		if group == nil {
			return 0, false
		}
		selected := resolver
		if selected == nil && calculator != nil {
			selected = billing.NewPriceResolver(nil, calculator, modelidentity.Identity, func(model string, err error) {
				slog.Debug("failed to get model pricing from model catalog, using fallback", "model", model, "error", err)
			})
		}
		value, err := selected.ResolveImageUnitPrice(ctx, billing.PricingInput{Model: model, GroupID: &group.ID}, size)
		return value, err == nil
	}
}
