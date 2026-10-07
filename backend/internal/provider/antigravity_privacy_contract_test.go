package provider_test

import (
	"testing"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
)

func applyAntigravitySubscriptionResult(provider *providercore.Record, result providercore.AntigravitySubscriptionResult) (map[string]any, map[string]any) {
	credentials := make(map[string]any)
	for k, v := range provider.Credentials {
		credentials[k] = v
	}
	credentials["plan_type"] = result.PlanType

	extra := make(map[string]any)
	for k, v := range provider.Extra {
		extra[k] = v
	}
	if result.SubscriptionStatus != "" {
		extra["subscription_status"] = result.SubscriptionStatus
	} else {
		delete(extra, "subscription_status")
	}
	if result.SubscriptionError != "" {
		extra["subscription_error"] = result.SubscriptionError
	} else {
		delete(extra, "subscription_error")
	}
	return credentials, extra
}

func TestApplyAntigravityPrivacyMode_SetsInMemoryExtra(t *testing.T) {
	provider := &providercore.Record{}

	providercore.ApplyAntigravityPrivacyMode(provider, providercore.AntigravityPrivacySet)

	if provider.Extra == nil {
		t.Fatal("expected provider.Extra to be initialized")
	}
	if got := provider.Extra["privacy_mode"]; got != providercore.AntigravityPrivacySet {
		t.Fatalf("expected privacy_mode %q, got %v", providercore.AntigravityPrivacySet, got)
	}
}

func TestApplyAntigravityPrivacyMode_PreservedBySubscriptionResult(t *testing.T) {
	provider := &providercore.Record{
		Credentials: map[string]any{
			"access_token": "token",
		},
		Extra: map[string]any{
			"existing": "value",
		},
	}
	providercore.ApplyAntigravityPrivacyMode(provider, providercore.AntigravityPrivacySet)

	_, extra := applyAntigravitySubscriptionResult(provider, providercore.AntigravitySubscriptionResult{
		PlanType: "Pro",
	})

	if got := extra["privacy_mode"]; got != providercore.AntigravityPrivacySet {
		t.Fatalf("expected subscription writeback to keep privacy_mode %q, got %v", providercore.AntigravityPrivacySet, got)
	}
	if got := extra["existing"]; got != "value" {
		t.Fatalf("expected existing extra fields to be preserved, got %v", got)
	}
}
