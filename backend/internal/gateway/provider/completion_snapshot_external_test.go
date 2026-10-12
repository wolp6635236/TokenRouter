package provider_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	billingtestkit "github.com/TokenFlux/TokenRouter/internal/billing/testkit"
	"github.com/TokenFlux/TokenRouter/internal/gateway/completion"
	forwardcore "github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/tierpolicy"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/upstream"
)

func TestClaudeUsageSpeedDrivesFastBilling(t *testing.T) {
	billing := billingtestkit.Calculator(nil, nil)
	svc := completion.NewRecorder(completion.Dependencies{Calculator: billing, Prices: billingtestkit.PriceResolver(nil, billing)}, completion.RecorderOptions{DefaultMultiplier: 1})

	groupID := int64(11)
	apiKey := &apikey.APIKey{GroupID: &groupID, Group: &routing.Group{ID: groupID}}
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformAnthropic, Type: capability.ProviderTypeAPIKey}}
	base := &forwardcore.MessagesResult{Usage: upstream.TokenUsage{InputTokens: 1000, OutputTokens: 100}, Model: "claude-opus-4-8"}
	fast := *base
	fast.Usage.Speed = "fast"

	baseCost := svc.CalculateTokenCost(context.Background(), gatewayprovider.ProjectMessagesCompletionResult(base, gatewayprovider.ExecutionCompletionRecord(provider)), gatewayprovider.ProjectCompletionKey(apiKey), gatewayprovider.ProjectCompletionProvider(gatewayprovider.ExecutionCompletionRecord(provider)), "claude-opus-4-8", "claude-opus-4-8", "", "", 1, nil)
	fastCost := svc.CalculateTokenCost(context.Background(), gatewayprovider.ProjectMessagesCompletionResult(&fast, gatewayprovider.ExecutionCompletionRecord(provider)), gatewayprovider.ProjectCompletionKey(apiKey), gatewayprovider.ProjectCompletionProvider(gatewayprovider.ExecutionCompletionRecord(provider)), "claude-opus-4-8", "claude-opus-4-8", "", "", 1, nil)
	require.InDelta(t, baseCost.ActualCost*2, fastCost.ActualCost, 1e-12)
	require.Equal(t, tierpolicy.OpenAIFastTierPriority, completion.ClaudeServiceTier(fast.Usage.Speed))
}
