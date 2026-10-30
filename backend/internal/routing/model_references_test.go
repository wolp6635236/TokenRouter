package routing

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/modelcatalog"
)

// model_references_test.go covers reference validation and snapshot selection.
// Controlled profiles distinguish source patterns, target cards, and catalog-only
// targets. Invalid chains include resource spellings and wildcard matches so
// reference lookup cannot become recursive through another name.

// TestModelReferenceValidation checks single-hop targets and mutually exclusive overrides.
// Missing target data is accepted; explicit zero and false remain configured fields.
func TestModelReferenceValidation(t *testing.T) {
	zero := 0.0
	disabled := false
	for _, target := range []string{"custom-one", "CUSTOM-TWO", "models/custom-three", "custom-*"} {
		require.Error(t, validatePriceReferences([]ModelPricingEntry{{Models: []string{"custom-*"}, ReferenceModel: target}}))
	}
	require.NoError(t, validatePriceReferences([]ModelPricingEntry{{Models: []string{"custom-*"}, ReferenceModel: " missing "}}))
	require.Error(t, validatePriceReferences([]ModelPricingEntry{{Models: []string{"a"}, ReferenceModel: "b"}, {Models: []string{"b"}, ReferenceModel: "c"}}))
	require.Error(t, validatePriceReferences([]ModelPricingEntry{{Models: []string{"a"}, ReferenceModel: "b", InputPrice: &zero}}))
	require.Error(t, validatePriceReferences([]ModelPricingEntry{{Models: []string{"a"}, ReferenceModel: "b", PerRequestPrice: &zero}}))
	require.Error(t, (PricingConfigValidation{}).ProviderStatsPricing([]ModelPricingEntry{{Models: []string{"a"}, ReferenceModel: "b"}}))
	config := &ModelAttributeConfig{Rules: []ModelAttributeRule{{Models: []string{"a"}, ReferenceModel: "b", Attributes: modelcatalog.Attributes{ToolCall: &disabled}}}}
	require.Error(t, config.ValidateReferences())
	config.Rules[0].Attributes = modelcatalog.Attributes{}
	require.NoError(t, config.ValidateReferences())
	config.Rules = append(config.Rules, ModelAttributeRule{Models: []string{"b*"}, ReferenceModel: "c", Match: &ModelAttributeMatch{Platform: "gemini"}})
	require.Error(t, config.ValidateReferences())
}

// TestResolveModelPricingReferences checks target inheritance within one cached profile.
// Returned cards are independent, and missing targets retain their lookup identity.
func TestResolveModelPricingReferences(t *testing.T) {
	amount := 2.0
	reads := 0
	rows := []PricingConfig{{ID: 1, Status: StatusActive, GroupIDs: []int64{7}, ModelPricing: []ModelPricingEntry{
		{Models: []string{"custom-*"}, ReferenceModel: "models/target"},
		{Models: []string{"target"}, InputPrice: &amount},
		{Models: []string{"absent"}, ReferenceModel: "missing"},
	}}}
	repo := &mockPricingConfigRepository{listAllFn: func(context.Context) ([]PricingConfig, error) {
		reads++
		return rows, nil
	}}
	service := NewPricingConfigService(repo, nil)
	model, card := service.ResolveModelPricing(t.Context(), 7, "custom-one")
	require.Equal(t, "models/target", model)
	require.Equal(t, 2.0, *card.InputPrice)
	*card.InputPrice = 9
	_, again := service.ResolveModelPricing(t.Context(), 7, "custom-two")
	require.Equal(t, 2.0, *again.InputPrice)
	require.Equal(t, 1, reads)
	model, card = service.ResolveModelPricing(t.Context(), 7, "absent")
	require.Equal(t, "missing", model)
	require.Nil(t, card)
	rows[0].ModelPricing[1].InputPrice = &amount
	amount = 3
	service.InvalidateCache()
	_, card = service.ResolveModelPricing(t.Context(), 7, "custom-one")
	require.Equal(t, 3.0, *card.InputPrice)
}
