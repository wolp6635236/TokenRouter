package pricingcontract

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/billing/pricing"
	"github.com/TokenFlux/TokenRouter/internal/billing/testkit"
	"github.com/TokenFlux/TokenRouter/internal/gateway/admission"
	"github.com/TokenFlux/TokenRouter/internal/gateway/completion"
	"github.com/TokenFlux/TokenRouter/internal/gateway/provider/modelidentity"
)

// model_references_scenario_test.go exercises routing reference selection through
// billing quotes, admission, calculation, media reservations, and gateway completion.
// Source catalog amounts deliberately differ from target amounts so accidental
// source fallback is visible in both displayed prices and final charges.

// TestModelReferencesQuoteAndChargeSameTarget compares token and media consumers.
// User price references retain the target's catalog identity while provider
// statistics continue reading the original upstream identity independently.
func TestModelReferencesQuoteAndChargeSameTarget(t *testing.T) {
	catalog := newCatalogFixture(catalogFixture{pricingData: map[string]*pricing.CatalogModelPricing{
		"source": {InputCostPerToken: 0.9, OutputCostPerToken: 0.8, OutputCostPerImage: 9},
		"target": {InputCostPerToken: 0.000002, OutputCostPerToken: 0.000006, OutputCostPerImage: 0.2},
		"video":  {CatalogRules: pricing.CatalogRules{VideoPrices: pricing.MediaPrices{"720p": 0.12}}, TokenPricingAbsent: true},
	}})
	calculator := newCalculator(catalog)
	gid := int64(100)
	resolver := testkit.ResolverWithCards(t, calculator, []pricing.ModelPricingEntry{{Models: []string{"source"}, ReferenceModel: "target"}})
	input := billing.PricingInput{Model: "source", GroupID: &gid}
	resolved := resolver.Resolve(t.Context(), input)
	require.Equal(t, "target", *resolved.LookupModel)
	require.InDelta(t, 0.000002, resolved.BasePricing.InputPricePerToken, 1e-12)
	cost, err := calculator.CalculateCostUnified(billing.CostInput{Ctx: t.Context(), Model: "source", GroupID: &gid, Tokens: pricing.UsageTokens{InputTokens: 1000, OutputTokens: 1000}, RateMultiplier: 2, Resolver: resolver})
	require.NoError(t, err)
	require.InDelta(t, 0.016, cost.ActualCost, 1e-12)
	unit, err := resolver.ResolveImageUnitPrice(t.Context(), input, "1K")
	require.NoError(t, err)
	require.Equal(t, 0.2, unit)
	quote := resolver.PublicQuote(t.Context(), billing.PublicQuoteInput{PricingInput: input, RateMultiplier: 2})
	require.Equal(t, "priced", quote.PriceStatus)
	recorder := completion.NewRecorder(completion.Dependencies{Calculator: calculator, Prices: resolver}, completion.RecorderOptions{DefaultMultiplier: 1})
	key := &completion.KeySnapshot{GroupID: &gid, Group: &completion.GroupSnapshot{ID: gid}}
	image, err := recorder.CalculateOpenAIImageCost(t.Context(), "source", key, &completion.Result{ImageCount: 3, ImageSize: "1K"}, 2)
	require.NoError(t, err)
	require.InDelta(t, 1.2, image.ActualCost, 1e-12)
	stats := resolver.ResolveProviderStats(t.Context(), billing.ProviderStatsCostInput{GroupID: gid, UpstreamModel: "source", Tokens: pricing.UsageTokens{InputTokens: 1}})
	require.NotNil(t, stats)
	require.InDelta(t, 0.9, *stats, 1e-12)
	videoResolver := testkit.ResolverWithCards(t, calculator, []pricing.ModelPricingEntry{{Models: []string{"source"}, ReferenceModel: "video"}})
	videoRecorder := completion.NewRecorder(completion.Dependencies{Calculator: calculator, Prices: videoResolver}, completion.RecorderOptions{DefaultMultiplier: 1})
	video, err := videoRecorder.CalculateOpenAIVideoCost(t.Context(), "source", key, &completion.Result{VideoCount: 2, VideoDurationSeconds: 10, VideoResolution: "720p"}, 2)
	require.NoError(t, err)
	require.InDelta(t, 4.8, video.ActualCost, 1e-12)
}

// TestModelReferencesInheritTargetCardsAndKeepMissingUnpriced covers configured targets.
// Free amounts, per-request units, and scheduled multipliers follow the target;
// absent targets cannot use the deliberately priced source catalog entry.
func TestModelReferencesInheritTargetCardsAndKeepMissingUnpriced(t *testing.T) {
	calculator := newCalculator(newCatalogFixture(catalogFixture{pricingData: map[string]*pricing.CatalogModelPricing{
		"source": {InputCostPerToken: 9, OutputCostPerImage: 9}, "target": {InputCostPerToken: 0.001, OutputCostPerToken: 0.002},
	}}))
	gid := int64(100)
	zero, unit := 0.0, 0.3
	cases := []struct {
		name   string                    // configured pricing scenario
		card   pricing.ModelPricingEntry // target model price card
		tokens pricing.UsageTokens       // reported token usage buckets
		want   float64                   // expected user charge in USD
	}{
		{"free", pricing.ModelPricingEntry{Models: []string{"target"}, InputPrice: &zero, OutputPrice: &zero}, pricing.UsageTokens{InputTokens: 100}, 0},
		{"request", pricing.ModelPricingEntry{Models: []string{"target"}, BillingMode: pricing.BillingModePerRequest, PerRequestPrice: &unit}, pricing.UsageTokens{}, 0.6},
		{"schedule", pricing.ModelPricingEntry{Models: []string{"target"}, TimePricing: &pricing.TimePricingConfig{Timezone: "UTC", Periods: []pricing.TimePricingPeriod{{StartTime: "09:00", EndTime: "12:00", Multiplier: 2}}}}, pricing.UsageTokens{InputTokens: 100}, 0.2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resolver := testkit.ResolverWithCards(t, calculator, []pricing.ModelPricingEntry{{Models: []string{"source"}, ReferenceModel: "target"}, tc.card})
			cost, err := calculator.CalculateCostUnified(billing.CostInput{Ctx: t.Context(), Model: "source", GroupID: &gid, Tokens: tc.tokens, RequestCount: 2, RateMultiplier: 1, PricingAt: time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC), Resolver: resolver})
			require.NoError(t, err)
			require.InDelta(t, tc.want, cost.ActualCost, 1e-12)
		})
	}
	missing := testkit.ResolverWithCards(t, calculator, []pricing.ModelPricingEntry{{Models: []string{"source"}, ReferenceModel: "missing"}})
	input := billing.PricingInput{Model: "source", GroupID: &gid}
	require.True(t, missing.Resolve(t.Context(), input).IsUnpriced())
	_, err := missing.ResolveImageUnitPrice(t.Context(), input, "1K")
	require.ErrorIs(t, err, pricing.ErrModelPricingUnavailable)
	require.Equal(t, "unpriced", missing.PublicQuote(t.Context(), billing.PublicQuoteInput{PricingInput: input, RateMultiplier: 1}).PriceStatus)
}

// TestCompatibleClaudeReferencePricingAdmissionAndCompletion compares reference preflight and settlement prices.
// Catalog targets, free targets, and target modifiers survive compatible Claude alias filtering;
// missing targets and ordinary Claude catalog candidates retain rejection behavior.
func TestCompatibleClaudeReferencePricingAdmissionAndCompletion(t *testing.T) {
	const alias = "claude-sonnet-4-5"
	target, free, missing := "kimi-k2.5", "free-target", "missing-target"
	factor := 2.0
	calculator := newCalculator(newCatalogFixture(catalogFixture{pricingData: map[string]*pricing.CatalogModelPricing{
		alias:  {InputCostPerToken: 9},
		target: {InputCostPerToken: 0.001, OutputCostPerToken: 0.002},
		free:   {Source: "models.dev", InputPricePresent: true},
	}}))
	cases := []struct {
		name      string                      // reference pricing scenario
		cards     []pricing.ModelPricingEntry // configured source and target cards
		admission bool                        // expected preflight price availability
		kept      bool                        // expected priced alias retention
		cost      float64                     // expected user charge in USD
	}{
		{"catalog target", []pricing.ModelPricingEntry{{Models: []string{alias}, ReferenceModel: target}}, true, true, 0.1},
		{"free target", []pricing.ModelPricingEntry{{Models: []string{alias}, ReferenceModel: free}}, true, true, 0},
		{"target modifier", []pricing.ModelPricingEntry{{Models: []string{alias}, ReferenceModel: target}, {Models: []string{target}, MaxReasoningEffortMultiplier: &factor}}, true, true, 0.2},
		{"missing target", []pricing.ModelPricingEntry{{Models: []string{alias}, ReferenceModel: missing}}, false, false, 0},
		{"corrupt chain", []pricing.ModelPricingEntry{{Models: []string{alias}, ReferenceModel: target}, {Models: []string{target}, ReferenceModel: free}}, false, false, 0},
		{"source modifier", []pricing.ModelPricingEntry{{Models: []string{alias}, MaxReasoningEffortMultiplier: &factor}}, true, false, 0},
		{"ordinary catalog", nil, true, false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resolver := testkit.ResolverWithCards(t, calculator, tc.cards)
			gid := int64(100)
			preflight := &admission.ModelPricing{Resolver: resolver}
			if tc.admission {
				require.NoError(t, preflight.Check(t.Context(), &gid, alias))
			} else {
				require.ErrorIs(t, preflight.Check(t.Context(), &gid, alias), admission.ErrModelPricingRejected)
			}
			recorder := completion.NewRecorder(completion.Dependencies{Calculator: calculator, Prices: resolver}, completion.RecorderOptions{DefaultMultiplier: 1})
			key := &completion.KeySnapshot{GroupID: &gid, Group: &completion.GroupSnapshot{ID: gid}}
			candidates := modelidentity.UsageCandidates(alias, target)
			filtered := recorder.FilterCompatibleClaudeBillingCandidates(t.Context(), &completion.ProviderSnapshot{RequiresExplicitClaudePricing: true}, key, candidates)
			effort := "max"
			result := &completion.Result{Model: alias, UpstreamModel: target, ReasoningEffort: &effort}
			if !tc.kept {
				require.Empty(t, filtered)
				_, err := recorder.CalculateOpenAIRecordUsageCostAt(t.Context(), result, key, filtered, 1, 1, 1, 1, pricing.UsageTokens{InputTokens: 100}, "", time.Time{})
				require.ErrorIs(t, err, pricing.ErrModelPricingUnavailable)
				return
			}
			require.Equal(t, []string{alias}, filtered)
			quote := resolver.PublicQuote(t.Context(), billing.PublicQuoteInput{PricingInput: billing.PricingInput{Model: alias, GroupID: &gid}, RateMultiplier: 1})
			require.Equal(t, "priced", quote.PriceStatus)
			cost, err := recorder.CalculateOpenAIRecordUsageCostAt(t.Context(), result, key, filtered, 1, 1, 1, 1, pricing.UsageTokens{InputTokens: 100}, "", time.Time{})
			require.NoError(t, err)
			require.InDelta(t, tc.cost, cost.ActualCost, 1e-12)
			require.Equal(t, alias, result.Model)
		})
	}
}

// TestCompatibleClaudeReferencesKeepCatalogMediaPrices exercises unit prices without token rates.
// Reference selection preserves the Claude alias through filtering, then image
// and video completion charge the target units instead of rejecting the candidate.
func TestCompatibleClaudeReferencesKeepCatalogMediaPrices(t *testing.T) {
	const alias = "claude-media-alias"
	calculator := newCalculator(newCatalogFixture(catalogFixture{pricingData: map[string]*pricing.CatalogModelPricing{
		"image-target": {OutputCostPerImage: 0.2, TokenPricingAbsent: true},
		"video-target": {CatalogRules: pricing.CatalogRules{VideoPrices: pricing.MediaPrices{"720p": 0.12}}, TokenPricingAbsent: true},
	}}))
	for _, tc := range []struct {
		target string            // referenced catalog model ID
		result completion.Result // completed media operation quantities
		want   float64           // expected user charge in USD
	}{
		{"image-target", completion.Result{Model: alias, ImageCount: 3, ImageSize: "1K"}, 0.6},
		{"video-target", completion.Result{Model: alias, VideoCount: 2, VideoDurationSeconds: 10, VideoResolution: "720p"}, 2.4},
	} {
		t.Run(tc.target, func(t *testing.T) {
			resolver := testkit.ResolverWithCards(t, calculator, []pricing.ModelPricingEntry{{Models: []string{alias}, ReferenceModel: tc.target}})
			gid := int64(100)
			key := &completion.KeySnapshot{GroupID: &gid, Group: &completion.GroupSnapshot{ID: gid}}
			recorder := completion.NewRecorder(completion.Dependencies{Calculator: calculator, Prices: resolver}, completion.RecorderOptions{DefaultMultiplier: 1})
			resolved := resolver.Resolve(t.Context(), billing.PricingInput{Model: alias, GroupID: &gid})
			require.False(t, resolved.HasConfiguredPricing())
			require.True(t, resolved.IsUnpriced())
			filtered := recorder.FilterCompatibleClaudeBillingCandidates(t.Context(), &completion.ProviderSnapshot{RequiresExplicitClaudePricing: true}, key, []string{alias})
			require.Equal(t, []string{alias}, filtered)
			cost, err := recorder.CalculateOpenAIRecordUsageCostAt(t.Context(), &tc.result, key, filtered, 1, 1, 1, 1, pricing.UsageTokens{}, "", time.Time{})
			require.NoError(t, err)
			require.InDelta(t, tc.want, cost.ActualCost, 1e-12)
			require.Equal(t, alias, tc.result.Model)
		})
	}
}
