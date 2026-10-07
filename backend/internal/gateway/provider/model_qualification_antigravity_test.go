package provider

import (
	"context"
	"testing"
	"time"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

func TestProviderIsSchedulableForModel_AntigravityRateLimits(t *testing.T) {
	now := time.Now()
	future := now.Add(10 * time.Minute)

	provider := &providercore.Record{
		ID:          1,
		Name:        "acc",
		Platform:    capability.PlatformAntigravity,
		Status:      billing.StatusActive,
		Schedulable: true,
	}

	provider.RateLimitResetAt = &future
	require.False(t, (ModelPolicy{Record: provider}).Schedulable(context.Background(), "claude-sonnet-4-5"))
	require.False(t, (ModelPolicy{Record: provider}).Schedulable(context.Background(), "gemini-3-flash"))

	provider.RateLimitResetAt = nil
	require.True(t, (ModelPolicy{Record: provider}).Schedulable(context.Background(), "claude-sonnet-4-5"))
	require.True(t, (ModelPolicy{Record: provider}).Schedulable(context.Background(), "gemini-3-flash"))
}
