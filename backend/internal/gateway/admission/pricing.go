package admission

import (
	"context"
	"fmt"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/billing/pricing"
)

// ModelPricingUnavailableMessage 告知调用方配置价格或选择已定价的模型。
const ModelPricingUnavailableMessage = "Model pricing is unavailable. Ask the administrator to configure a price or map the request to a priced model."

// ErrModelPricingRejected 表示请求在调用上游前因缺价被拒绝。
var ErrModelPricingRejected = fmt.Errorf("model pricing admission rejected: %w", pricing.ErrModelPricingUnavailable)

// ModelPricing 检查文本请求的计费模型是否有目录价或手动价卡。
// @project-doc docs/domains/routing_and_billing.md#missing_model_pricing
type ModelPricing struct {
	Resolver *billing.PriceResolver
}

// Check 允许手动零价和区间价，缺少基础价的倍率配置仍按缺价拒绝。
func (p *ModelPricing) Check(ctx context.Context, groupID *int64, model string) error {
	resolved := p.Resolver.Resolve(ctx, billing.PricingInput{Model: model, GroupID: groupID})
	if resolved == nil || resolved.IsUnpriced() {
		return fmt.Errorf("%w: %s", ErrModelPricingRejected, model)
	}
	return nil
}
