package pricingcontract

import (
	"time"

	"github.com/TokenFlux/TokenRouter/internal/billing/pricing"
	billingadapter "github.com/TokenFlux/TokenRouter/internal/billing/provider"
)

// contractTimeLocation 为定价接口测试加载时区，配置校验和倍率计算由生产实现执行。
func contractTimeLocation(value *pricing.TimePricingConfig) *time.Location {
	if value == nil {
		return nil
	}
	location, _ := billingadapter.LoadPricingLocation(value.Timezone)
	return location
}
