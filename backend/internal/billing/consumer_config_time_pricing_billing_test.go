package billing_test

import (
	"context"
	"testing"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	billingpricing "github.com/TokenFlux/TokenRouter/internal/billing/pricing"
	billingtestkit "github.com/TokenFlux/TokenRouter/internal/billing/testkit"
	"github.com/TokenFlux/TokenRouter/internal/gateway/provider/modelidentity"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/stretchr/testify/require"
)

func TestCalculateCostUnifiedAppliesPricingTimeMultiplierToTokenBuckets(t *testing.T) {
	groupID := int64(71)
	pricing := &routing.ModelPricingEntry{
		BillingMode: routing.BillingModeToken,
		InputPrice:  floatPtr(5e-6),
		TimePricing: &routing.TimePricingConfig{
			Timezone: "Asia/Shanghai",
			Periods:  []routing.TimePricingPeriod{{StartTime: "09:00", EndTime: "12:00", Multiplier: 2}},
		},
	}
	resolved := &billingpricing.ResolvedPricing{
		Mode:          routing.BillingModeToken,
		Source:        billingpricing.PricingSourceConfig,
		BasePricing:   &billingpricing.ModelPricing{InputPricePerToken: 5e-6, OutputPricePerToken: 15e-6},
		ConfigPricing: pricing,
	}
	service := billingtestkit.Calculator(nil, nil)
	resolver := billing.NewPriceResolver(nil, service, modelidentity.Identity, nil)

	cost, err := service.CalculateCostUnified(billing.CostInput{
		Ctx:            context.Background(),
		Model:          "claude-sonnet-4",
		GroupID:        &groupID,
		Tokens:         billingpricing.UsageTokens{InputTokens: 100, OutputTokens: 10},
		RateMultiplier: 3,
		PricingAt:      time.Date(2026, 6, 29, 1, 0, 0, 0, time.UTC),
		Resolver:       resolver,
		Resolved:       resolved,
	})
	require.NoError(t, err)
	want := (100*5e-6 + 10*15e-6) * 2
	require.InDelta(t, want, cost.TotalCost, 1e-12)
	require.InDelta(t, want*3, cost.ActualCost, 1e-12)
}

func TestCalculateCostUnifiedDoesNotApplyConfigTimeMultiplierToPerRequest(t *testing.T) {
	groupID := int64(72)
	pricing := &routing.ModelPricingEntry{
		BillingMode:     routing.BillingModePerRequest,
		PerRequestPrice: floatPtr(0.05),
		TimePricing: &routing.TimePricingConfig{
			Timezone: "Asia/Shanghai",
			Periods:  []routing.TimePricingPeriod{{StartTime: "09:00", EndTime: "12:00", Multiplier: 2}},
		},
	}
	resolved := &billingpricing.ResolvedPricing{Mode: routing.BillingModePerRequest, Source: billingpricing.PricingSourceConfig, ConfigPricing: pricing, DefaultPerRequestPrice: 0.05}
	service := billingtestkit.Calculator(nil, nil)
	resolver := billing.NewPriceResolver(nil, service, modelidentity.Identity, nil)

	cost, err := service.CalculateCostUnified(billing.CostInput{
		Ctx:            context.Background(),
		Model:          "image-model",
		GroupID:        &groupID,
		RequestCount:   3,
		RateMultiplier: 2,
		PricingAt:      time.Date(2026, 6, 29, 1, 0, 0, 0, time.UTC),
		Resolver:       resolver,
		Resolved:       resolved,
	})
	require.NoError(t, err)
	require.InDelta(t, 0.15, cost.TotalCost, 1e-12)
	require.InDelta(t, 0.30, cost.ActualCost, 1e-12)
}

func TestDisplayPricingCarriesConfigTimePricingAndMaxReasoningMultiplier(t *testing.T) {
	maxMultiplier := 1.5
	pricing := &routing.ModelPricingEntry{
		BillingMode: routing.BillingModeToken,
		InputPrice:  floatPtr(5e-6),
		TimePricing: &routing.TimePricingConfig{
			Timezone:     "Asia/Shanghai",
			WeekdaysOnly: true,
			Periods:      []routing.TimePricingPeriod{{StartTime: "09:00", EndTime: "12:00", Multiplier: 2}},
		},
	}
	resolved := &billingpricing.ResolvedPricing{
		Mode:   routing.BillingModeToken,
		Source: billingpricing.PricingSourceConfig,
		BasePricing: &billingpricing.ModelPricing{
			InputPricePerToken:           5e-6,
			OutputPricePerToken:          15e-6,
			MaxReasoningEffortMultiplier: &maxMultiplier,
		},
		ConfigPricing: pricing,
	}
	service := billingtestkit.Calculator(nil, nil)

	display := service.DisplayPricingWithResolvedMultipliers("claude-sonnet-4", 2, resolved)

	// 单价按 1x 时段展示，分时规则和 Max 倍率交给前端单独说明。
	require.InDelta(t, 10e-6, display.InputPricePerToken, 1e-15)
	require.NotNil(t, display.MaxReasoningEffortMultiplier)
	require.Equal(t, 1.5, *display.MaxReasoningEffortMultiplier)
	require.Equal(t, pricing.TimePricing, display.TimePricing)
	require.NotSame(t, pricing.TimePricing, display.TimePricing)
}

func TestDisplayPricingOmitsNeutralModifiers(t *testing.T) {
	neutral := 1.0
	resolved := &billingpricing.ResolvedPricing{
		Mode:   routing.BillingModeToken,
		Source: billingpricing.PricingSourceConfig,
		BasePricing: &billingpricing.ModelPricing{
			InputPricePerToken:           5e-6,
			MaxReasoningEffortMultiplier: &neutral,
		},
		ConfigPricing: &routing.ModelPricingEntry{
			BillingMode: routing.BillingModeToken,
			InputPrice:  floatPtr(5e-6),
			// 重叠时段在结算时按 1x 处理，展示同样省略。
			TimePricing: &routing.TimePricingConfig{
				Timezone: "UTC",
				Periods: []routing.TimePricingPeriod{
					{StartTime: "01:00", EndTime: "04:00", Multiplier: 2},
					{StartTime: "03:00", EndTime: "05:00", Multiplier: 3},
				},
			},
		},
	}
	service := billingtestkit.Calculator(nil, nil)

	display := service.DisplayPricingWithResolvedMultipliers("claude-sonnet-4", 1, resolved)

	require.Nil(t, display.MaxReasoningEffortMultiplier)
	require.Nil(t, display.TimePricing)
}
