package provider

import (
	"context"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/routing"
)

// ProviderStatsSource 提供共享价格配置，billing 据此计算提供商成本。
type ProviderStatsSource struct{ Service *routing.PricingConfigService }

func (s ProviderStatsSource) ProviderStatsGroup(ctx context.Context, id int64) (*billing.ProviderStatsPricingConfig, error) {
	pricingConfig, err := s.Service.GetPricingConfigForGroup(ctx, id)
	if err != nil || pricingConfig == nil {
		return nil, err
	}
	return &billing.ProviderStatsPricingConfig{Rules: pricingConfig.ProviderStatsPricingRules}, nil
}
