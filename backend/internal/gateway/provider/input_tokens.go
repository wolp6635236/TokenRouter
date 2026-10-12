package provider

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	protocolforward "github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	"github.com/TokenFlux/TokenRouter/internal/gateway/tokenestimate"
	protocolanthropic "github.com/TokenFlux/TokenRouter/internal/protocol/anthropic"
	protocolbridge "github.com/TokenFlux/TokenRouter/internal/protocol/bridge"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

type InputTokensPrepared struct {
	Request         tokenestimate.Request
	OriginalModel   string
	NormalizedModel string
	BillingModel    string
	UpstreamModel   string
}

func PrepareAnthropicInputTokens(
	body []byte,
	provider *ExecutionProvider,
	defaultMappedModel string,
) (*InputTokensPrepared, error) {
	var anthropicReq protocolanthropic.AnthropicRequest
	if err := json.Unmarshal(body, &anthropicReq); err != nil {
		return nil, fmt.Errorf("parse anthropic count_tokens request: %w", err)
	}

	originalModel := anthropicReq.Model
	normalizedModel := anthropicReq.Model
	billingModel := ExecutionModelPolicy(provider).ForwardModel(normalizedModel, strings.TrimSpace(defaultMappedModel))
	upstreamModel := ExecutionModelPolicy(provider).NormalizeOpenAI(billingModel)

	responsesReq, err := protocolbridge.AnthropicToResponses(&anthropicReq, protocolforward.ConversionOptionsForModel(anthropicReq.Model))
	if err != nil {
		return nil, fmt.Errorf("convert anthropic request to responses: %w", err)
	}

	return &InputTokensPrepared{
		Request: tokenestimate.Request{
			Model:        upstreamModel,
			Instructions: responsesReq.Instructions,
			Input:        responsesReq.Input,
			Tools:        responsesReq.Tools,
			ToolChoice:   responsesReq.ToolChoice,
		},
		OriginalModel:   originalModel,
		NormalizedModel: normalizedModel,
		BillingModel:    billingModel,
		UpstreamModel:   upstreamModel,
	}, nil
}

func PrepareNativeInputTokens(body []byte, provider *ExecutionProvider) (*InputTokensPrepared, error) {
	var req tokenestimate.Request
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("parse responses input_tokens request: %w", err)
	}
	originalModel := strings.TrimSpace(req.Model)
	if originalModel == "" {
		return nil, fmt.Errorf("parse responses input_tokens request: model is required")
	}
	billingModel := ExecutionModelPolicy(provider).ForwardModel(originalModel, "")
	upstreamModel := ExecutionModelPolicy(provider).NormalizeOpenAI(billingModel)
	req.Model = upstreamModel
	return &InputTokensPrepared{
		Request:         req,
		OriginalModel:   originalModel,
		NormalizedModel: originalModel,
		BillingModel:    billingModel,
		UpstreamModel:   upstreamModel,
	}, nil
}

func EstimateInputTokensLocally(provider *ExecutionProvider) bool {
	if provider == nil || provider.View().IsGrok() || provider.View().IsCNProvider() || provider.Record.Type == capability.ProviderTypeUpstream {
		return true
	}
	if provider.Record.Type != capability.ProviderTypeAPIKey {
		return false
	}
	baseURL := strings.TrimSpace(provider.View().GetCredential("base_url"))
	if baseURL == "" {
		return false
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return true
	}
	return !strings.EqualFold(parsed.Hostname(), "api.openai.com")
}
