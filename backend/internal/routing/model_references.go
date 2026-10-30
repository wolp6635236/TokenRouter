package routing

import (
	"context"
	"reflect"
	"strings"

	"github.com/TokenFlux/TokenRouter/internal/billing/pricing"
	"github.com/TokenFlux/TokenRouter/internal/modelcatalog"
	"github.com/TokenFlux/TokenRouter/internal/pkg/apperror"
)

// model_references.go validates single-hop profile references and selects their
// effective price cards. References borrow a target's complete configuration,
// so source overrides cannot be combined with a reference. Target validation
// includes every reference source, regardless of attribute route selectors.
//
// Price lookup retains one configuration snapshot while selecting the source
// and target. The returned model identifies the catalog data used to fill the
// target's missing prices, even when the target has no configured card. This
// keeps catalog inheritance and media prices attached to the same identity.
// Runtime guards also reject chains in records written outside validated APIs.

// validateReference checks a concrete target against all reference source patterns.
// Resource spellings use the same identity candidates as price lookup, allowing
// validation to reject a chain hidden behind a Google resource prefix.
func validateReference(target string, sources []string) error {
	if target == "" || strings.Contains(target, "*") {
		return apperror.BadRequest("INVALID_MODEL_REFERENCE", "reference_model must name a concrete model")
	}
	for _, candidate := range pricing.BuildModelIdentityCandidates(target) {
		for _, pattern := range sources {
			pattern = strings.ToLower(strings.TrimSpace(pattern))
			if pattern == candidate || strings.HasSuffix(pattern, "*") && strings.HasPrefix(candidate, strings.TrimSuffix(pattern, "*")) {
				return apperror.BadRequest("INVALID_MODEL_REFERENCE", "reference target must not match another reference rule")
			}
		}
	}
	return nil
}

// validatePriceReferences normalizes targets and rejects source-side price fields.
// A token billing mode is accepted as the storage default for reference rows;
// the selected target determines the effective charging mode at resolution time.
func validatePriceReferences(entries []ModelPricingEntry) error {
	var sources []string
	for i := range entries {
		entries[i].ReferenceModel = strings.TrimSpace(entries[i].ReferenceModel)
		if entries[i].ReferenceModel != "" {
			sources = append(sources, entries[i].Models...)
		}
	}
	for _, entry := range entries {
		if entry.ReferenceModel == "" {
			continue
		}
		if err := validateReference(entry.ReferenceModel, sources); err != nil {
			return err
		}
		card := entry
		card.ReferenceModel = ""
		hasOverrides := card.HasEffectivePricing() || card.PriceMultiplier != nil ||
			card.PerRequestPrice != nil || card.FastModeMultiplier != nil ||
			len(card.Intervals) > 0 || card.TimePricing != nil ||
			card.BillingMode != "" && card.BillingMode != BillingModeToken
		if hasOverrides {
			return apperror.BadRequest("INVALID_MODEL_REFERENCE", "reference rules cannot contain price overrides")
		}
	}
	return nil
}

// ValidateReferences normalizes attribute references for both saves and previews.
// Targets can be absent from the catalog, but reference rules cannot carry
// attribute overrides or point at another reference source in this profile.
func (c *ModelAttributeConfig) ValidateReferences() error {
	var sources []string
	for i := range c.Rules {
		c.Rules[i].ReferenceModel = strings.TrimSpace(c.Rules[i].ReferenceModel)
		if c.Rules[i].ReferenceModel != "" {
			sources = append(sources, c.Rules[i].Models...)
		}
	}
	for _, rule := range c.Rules {
		if rule.ReferenceModel == "" {
			continue
		}
		if err := validateReference(rule.ReferenceModel, sources); err != nil {
			return err
		}
		if !reflect.DeepEqual(rule.Attributes, modelcatalog.Attributes{}) {
			return apperror.BadRequest("INVALID_MODEL_REFERENCE", "reference rules cannot contain attribute overrides")
		}
	}
	return nil
}

// ResolveModelPricing selects a catalog identity and card from one group snapshot.
// Missing profiles retain the supplied model. A reference with no target card
// returns its target identity with nil pricing so catalog inheritance remains
// possible. Returned cards own separate mutable storage.
//
// Exact and wildcard matching run for each complete identity before resource
// alternatives, following ordinary configured pricing. A stored chain returns
// an empty lookup identity, leaving pricing unavailable instead of reading the
// source's catalog record. Repository failures retain the existing observed
// catalog-only behavior used by ordinary configured-price lookup.
func (s *PricingConfigService) ResolveModelPricing(ctx context.Context, groupID int64, model string) (string, *ModelPricingEntry) {
	lk, err := s.lookupGroupPricingConfig(ctx, groupID)
	if err != nil {
		// Failed profile reads retain the existing catalog-only policy and are
		// observed here before any source or target card has been selected.
		s.warn("failed to load pricing configuration cache", "group_id", groupID, "error", err)
		return model, nil
	}
	if lk == nil {
		return model, nil
	}
	lookup := func(name string) *ModelPricingEntry {
		for _, candidate := range pricing.BuildModelIdentityCandidates(name) {
			if entry := lookupPricing(lk.cache, groupID, candidate, true); entry != nil {
				return entry
			}
		}
		return nil
	}
	entry := lookup(model)
	if entry != nil && entry.ReferenceModel != "" {
		model = entry.ReferenceModel
		entry = lookup(model)
		if entry != nil && entry.ReferenceModel != "" {
			// Corrupt persisted references cannot recurse or silently recover source
			// prices; an empty lookup identity keeps downstream media reads unpriced.
			return "", nil
		}
	}
	if entry == nil {
		return model, nil
	}
	copy := entry.Clone()
	return model, &copy
}
