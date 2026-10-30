package usage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/billing/pricing"
)

// system_usage_test.go checks system pricing and persistence retry semantics
// independently of upstream transport. Fixed catalog amounts distinguish
// missing evidence from explicit zero usage, and cancellation fixtures verify
// that already observed cost is persisted without charging customer funds.

// systemPriceCatalog provides stable token prices for accounting fixtures.
type systemPriceCatalog struct{}

// systemStatsPrices supplies one independent provider card to recorder tests.
type systemStatsPrices struct {
	mode pricing.BillingMode // configured provider pricing unit
}

// systemMappedPrices supplies a card only for the intermediate group alias.
type systemMappedPrices struct{}

// GetModelPricing returns nil only for missing and an explicit zero-price card
// for free. Every other fixture name receives the same nonzero input and output
// rates so tests can distinguish model policy from arithmetic.
func (systemPriceCatalog) GetModelPricing(model string) *billing.CatalogModelPricing {
	if model == "missing" {
		return nil
	}
	price := &billing.CatalogModelPricing{Source: "fixture", InputPricePresent: true}
	if model != "free" {
		price.InputCostPerToken = 0.01
		price.OutputCostPerToken = 0.02
	}
	return price
}

// ForceUpdate leaves the deterministic fixture catalog unchanged for every call.
func (systemPriceCatalog) ForceUpdate() error { return nil }

// TestSystemRecorderPricesObservedWork supplies complete, missing, partial, and
// explicitly free observations. It verifies that only supported cost evidence
// becomes a numeric provider cost and every customer funding amount stays zero.
func TestSystemRecorderPricesObservedWork(t *testing.T) {
	for _, test := range []struct {
		model string // fixture pricing identity
		state string // observed usage completeness
		want  string // expected cost completeness
	}{
		{"paid", "complete", "calculated"},
		{"paid", "partial", "partial"},
		{"paid", "missing", "unknown"},
		{"missing", "complete", "unknown"},
		{"free", "complete", "calculated"},
	} {
		t.Run(test.model+test.state, func(t *testing.T) {
			row := &UsageLog{RequestID: "attempt", RequestSource: SourceProviderTest, Model: test.model, ProviderID: 1, InputTokens: 10, OutputTokens: 2, ActualCost: 9, UserID: 3, APIKeyID: 4, CreatedAt: time.Now(), SystemUsage: &SystemUsage{Outcome: "success", UsageState: test.state}}
			calls := 0
			recorder := &SystemRecorder{Calculator: billing.NewCalculator(systemPriceCatalog{}, billing.CalculatorOptions{}), Save: func(ctx context.Context, got *UsageLog, _ string) error {
				calls++
				require.NoError(t, ctx.Err())
				require.Zero(t, got.ActualCost)
				require.Zero(t, got.UserID)
				require.Zero(t, got.APIKeyID)
				if calls == 1 {
					return errors.New("transient persistence failure")
				}
				return nil
			}}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			require.NoError(t, recorder.Record(ctx, row, "apikey"))
			require.Equal(t, 2, calls)
			require.Equal(t, test.want, row.SystemUsage.CostState)
			if test.want == "unknown" {
				require.Nil(t, row.ProviderStatsCost)
			} else {
				require.NotNil(t, row.ProviderStatsCost)
				if test.model == "free" {
					require.Zero(t, *row.ProviderStatsCost)
				} else {
					require.InDelta(t, 0.14, *row.ProviderStatsCost, 1e-9)
				}
			}
		})
	}
}

// ProviderStatsGroup ignores group selection and returns one provider-one card
// for paid. Each call creates independent configuration storage, so recorder
// tests cannot share mutable pricing state across their cases.
func (s systemStatsPrices) ProviderStatsGroup(context.Context, int64) (*billing.ProviderStatsPricingConfig, error) {
	price := 0.01
	return &billing.ProviderStatsPricingConfig{Rules: []pricing.ProviderStatsPricingRule{{ProviderIDs: []int64{1}, Pricing: []pricing.ModelPricingEntry{{Models: []string{"paid"}, BillingMode: s.mode, InputPrice: &price, PerRequestPrice: &price}}}}}, nil
}

// TestSystemRecorderIncompleteOperationPricing compares provider token and flat
// operation cards after an interrupted request. Only measured token cost can
// enter provider spending without evidence that the operation completed.
func TestSystemRecorderIncompleteOperationPricing(t *testing.T) {
	for _, mode := range []pricing.BillingMode{pricing.BillingModeToken, pricing.BillingModePerRequest} {
		calculator := billing.NewCalculator(systemPriceCatalog{}, billing.CalculatorOptions{})
		resolver := billing.NewPriceResolver(nil, calculator, nil, nil, systemStatsPrices{mode})
		recorder := &SystemRecorder{Calculator: calculator, Resolver: resolver, Save: func(context.Context, *UsageLog, string) error { return nil }}
		row := &UsageLog{RequestID: "partial", RequestSource: SourceProviderTest, ProviderID: 1, Model: "paid", InputTokens: 10, CreatedAt: time.Now(), SystemUsage: &SystemUsage{Outcome: "failed", UsageState: "partial"}}
		require.NoError(t, recorder.Record(t.Context(), row, "apikey"))
		if mode == pricing.BillingModeToken {
			require.Equal(t, "partial", row.SystemUsage.CostState)
			require.InDelta(t, 0.1, *row.ProviderStatsCost, 1e-9)
		} else {
			require.Equal(t, "unknown", row.SystemUsage.CostState)
			require.Nil(t, row.ProviderStatsCost)
		}
	}
}

// TestSystemRecorderRejectsUnconfiguredVendorAlias exposes a catalog price for
// a compatible provider's Claude-shaped alias. Without an explicit group price,
// neither base pricing nor provider fallback may borrow that unrelated amount.
func TestSystemRecorderRejectsUnconfiguredVendorAlias(t *testing.T) {
	calculator := billing.NewCalculator(systemPriceCatalog{}, billing.CalculatorOptions{})
	recorder := &SystemRecorder{Calculator: calculator, Resolver: billing.NewPriceResolver(nil, calculator, nil, nil, systemStatsPrices{pricing.BillingModeToken}), RequiresExplicitPricing: func(platform, model string) bool { return platform == "kimi" && model == "claude-alias" }, Save: func(context.Context, *UsageLog, string) error { return nil }}
	row := &UsageLog{RequestID: "alias", RequestSource: SourceProviderTest, ProviderID: 1, Platform: "kimi", Model: "claude-alias", InputTokens: 10, OutputTokens: 2, CreatedAt: time.Now(), SystemUsage: &SystemUsage{Outcome: "success", UsageState: "complete"}}
	require.NoError(t, recorder.Record(t.Context(), row, "apikey"))
	require.Equal(t, "unknown", row.SystemUsage.CostState)
	require.Nil(t, row.ProviderStatsCost)
	require.Zero(t, row.TotalCost)
}

// TestSystemRecorderOperationCostWithoutTokens supplies successful operation
// results without token counters. Configured request prices remain usable,
// while image pricing requires an image and token pricing requires counters.
func TestSystemRecorderOperationCostWithoutTokens(t *testing.T) {
	for _, test := range []struct {
		mode   pricing.BillingMode // configured provider price unit
		images int                 // observed generated image count
		priced bool                // expected known provider cost
	}{{pricing.BillingModeToken, 0, false}, {pricing.BillingModePerRequest, 0, true}, {pricing.BillingModeImage, 1, true}, {pricing.BillingModeImage, 0, false}} {
		calculator := billing.NewCalculator(systemPriceCatalog{}, billing.CalculatorOptions{})
		recorder := &SystemRecorder{Calculator: calculator, Resolver: billing.NewPriceResolver(nil, calculator, nil, nil, systemStatsPrices{test.mode}), Save: func(context.Context, *UsageLog, string) error { return nil }}
		row := &UsageLog{RequestID: "operation", RequestSource: SourceProviderTest, ProviderID: 1, Model: "paid", ImageCount: test.images, CreatedAt: time.Now(), SystemUsage: &SystemUsage{Outcome: "success", UsageState: "missing"}}
		require.NoError(t, recorder.Record(t.Context(), row, "apikey"))
		if test.priced {
			require.Equal(t, "calculated", row.SystemUsage.CostState)
			require.InDelta(t, 0.01, *row.ProviderStatsCost, 1e-9)
		} else {
			require.Equal(t, "unknown", row.SystemUsage.CostState)
			require.Nil(t, row.ProviderStatsCost)
		}
	}
}

// GetEffectiveConfigModelPricing returns an independent token card for mapped.
// Other model identities remain unpriced, allowing the recorder test to verify
// fallback from the actual and requested names without borrowing catalog amounts.
func (systemMappedPrices) GetEffectiveConfigModelPricing(_ context.Context, _ int64, model string) *billing.ModelPricingEntry {
	if model != "mapped" {
		return nil
	}
	input, output := 0.01, 0.02
	return &billing.ModelPricingEntry{Models: []string{"mapped"}, InputPrice: &input, OutputPrice: &output}
}

// TestSystemRecorderUsesMappedModelPrice keeps actual and requested identities
// outside the catalog while pricing the group-mapped alias. The expense must
// use that configured card and preserve the recorded execution model.
func TestSystemRecorderUsesMappedModelPrice(t *testing.T) {
	calculator := billing.NewCalculator(nil, billing.CalculatorOptions{})
	recorder := &SystemRecorder{Calculator: calculator, Resolver: billing.NewPriceResolver(systemMappedPrices{}, calculator, nil, nil), Save: func(context.Context, *UsageLog, string) error { return nil }}
	groupID := int64(1)
	upstream := "unpriced-upstream"
	row := &UsageLog{RequestID: "mapping", RequestSource: SourceGroupProbe, ProviderID: 1, GroupID: &groupID, Model: "requested", RequestedModel: "requested", UpstreamModel: &upstream, InputTokens: 10, OutputTokens: 2, CreatedAt: time.Now(), SystemUsage: &SystemUsage{MappedModel: "mapped", Outcome: "success", UsageState: "complete"}}
	require.NoError(t, recorder.Record(t.Context(), row, "apikey"))
	require.Equal(t, "calculated", row.SystemUsage.CostState)
	require.NotNil(t, row.ProviderStatsCost)
	require.InDelta(t, 0.14, *row.ProviderStatsCost, 1e-9)
	require.Equal(t, "unpriced-upstream", *row.UpstreamModel)
}
