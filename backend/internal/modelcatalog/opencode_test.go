package modelcatalog

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestOpenCodeProtocolsSeparatesModesAndCopiesDefaults compares an exact shared
// model across the bundled service offerings. Mutating one result cannot alter
// subsequent route lookups or another service mode.
func TestOpenCodeProtocolsSeparatesModesAndCopiesDefaults(t *testing.T) {
	payg, goMode := OpenCodeProtocols("payg"), OpenCodeProtocols("go")
	require.Equal(t, "openai_chat_completions", payg["minimax-m3"])
	require.Equal(t, "anthropic_messages", goMode["minimax-m3"])
	require.Equal(t, "systemone", payg["jev-1.13"])
	payg["minimax-m3"] = "changed"
	require.Equal(t, "openai_chat_completions", OpenCodeProtocols("payg")["minimax-m3"])
	require.Empty(t, OpenCodeProtocols("unknown"))
}

// TestParseOpenCodeOfferingsUsesDeclaredSDK gives one provider default and
// several explicit model SDKs. Unknown encodings stay unavailable rather than
// being guessed from otherwise familiar model names.
func TestParseOpenCodeOfferingsUsesDeclaredSDK(t *testing.T) {
	body := []byte(`{"providers":{"opencode":{"npm":"@ai-sdk/openai-compatible","models":{"chat":{},"response":{"provider":{"npm":"@ai-sdk/openai"}},"messages":{"provider":{"npm":"@ai-sdk/anthropic"}},"google":{"provider":{"npm":"@ai-sdk/google"}},"decision":{"type":"decision"},"gpt-unknown":{"provider":{"npm":"unknown"}}}}}}`)
	routes := offeringProtocols(ParseOpenCodeOfferings(body)["payg"])
	require.Len(t, routes, 5)
	require.Equal(t, "gemini_generate_content", routes["google"])
	require.Equal(t, "systemone", routes["decision"])
	require.NotContains(t, routes, "gpt-unknown")
}

// TestOpenCodeCanonicalMetadataKeepsOfferingAndPriceIdentities resolves a service
// alias whose author has public metadata. Limits and encoding come from their
// respective identities while the service offering stays excluded from prices.
func TestOpenCodeCanonicalMetadataKeepsOfferingAndPriceIdentities(t *testing.T) {
	body, err := Offline()
	require.NoError(t, err)
	catalog, err := Parse(body)
	require.NoError(t, err)
	model := "nemotron-3.5-lightning-free"
	resolved := catalog.View().Resolve(Target{Platform: "opencode", ProviderType: "apikey", ProviderMode: "payg", Model: model, UpstreamProtocol: "openai_chat_completions"}, Attributes{})
	require.Equal(t, model, resolved.Target.Model)
	require.Equal(t, "nvidia/nemotron-3.5-lightning", resolved.Target.AttributeModel)
	require.NotNil(t, resolved.Attributes.Context)
	require.Equal(t, 262144, *resolved.Attributes.Context)
	_, priced := catalog.Lookup([]string{"opencode/" + model})
	require.False(t, priced)
	require.Equal(t, "openai_chat_completions", OpenCodeProtocols("payg")[model])
}

// TestOpenCodeClaudeEncodingUsesModelRules resolves the same Claude Messages
// model through an intermediary and its author. Adaptive thinking and conditional
// temperature follow the model while provider identity remains OpenCode.
func TestOpenCodeClaudeEncodingUsesModelRules(t *testing.T) {
	body, err := Offline()
	require.NoError(t, err)
	catalog, err := Parse(body)
	require.NoError(t, err)
	for _, platform := range []string{"anthropic", "opencode"} {
		resolved := catalog.View().Resolve(Target{Platform: platform, ProviderType: "apikey", Model: "claude-opus-4-7", UpstreamProtocol: "anthropic_messages"}, Attributes{})
		require.Equal(t, platform, resolved.Target.Platform)
		if platform == "opencode" {
			require.Equal(t, "anthropic/claude-opus-4-7", resolved.Target.AttributeModel)
			_, priced := catalog.Lookup([]string{"opencode/claude-opus-4-7"})
			require.False(t, priced)
		}
		require.Equal(t, "adaptive", resolved.Rules.ThinkingMode)
		require.Equal(t, "anthropic", resolved.Rules.SignatureMode)
		require.NotNil(t, resolved.ForRequest(RequestState{}).Attributes.Temperature)
		require.False(t, *resolved.ForRequest(RequestState{}).Attributes.Temperature)
		require.Empty(t, resolved.Rules.DropFields)
	}
}

// TestOpenCodeLiveCanonicalMetadataResolvesUnbundledOffering parses a publication
// containing a new service alias. Its declared canonical metadata is usable
// even when no embedded offering or price entry exists for that alias.
func TestOpenCodeLiveCanonicalMetadataResolvesUnbundledOffering(t *testing.T) {
	catalog, err := Parse([]byte(`{"models":{"author/new":{"limit":{"context":12345}}},"providers":{"opencode":{"npm":"@ai-sdk/openai-compatible","models":{"new-offering":{"canonical_model_id":"author/new","cost":{"input":99,"output":99}}}}}}`))
	require.NoError(t, err)
	resolved := catalog.View().Resolve(Target{Platform: "opencode", Model: "new-offering"}, Attributes{})
	require.Equal(t, "author/new", resolved.Target.AttributeModel)
	require.NotNil(t, resolved.Attributes.Context)
	require.Equal(t, 12345, *resolved.Attributes.Context)
	_, priced := catalog.Lookup([]string{"new-offering"})
	require.False(t, priced)
}

// TestParseOpenCodeOfferingsUsesUniqueExactPublicModels resolves missing canonical
// declarations from public author records. Explicit references win, ambiguous
// authors stay unknown, and model name fragments never select a public identity.
func TestParseOpenCodeOfferingsUsesUniqueExactPublicModels(t *testing.T) {
	body := []byte(`{"models":{"anthropic/claude-opus-4-7":{},"openai/shared":{},"another/shared":{},"openai/shared-longer":{},"third/shared":{}},"providers":{"opencode":{"npm":"@ai-sdk/anthropic","models":{"claude-opus-4-7":{},"shared":{},"shared-longer":{},"claude-opus-4-7-free":{},"shared-alias":{"canonical_model_id":"openai/shared"}}},"opencode-go":{"npm":"@ai-sdk/anthropic","models":{"claude-opus-4-7":{},"shared":{"canonical_model_id":"another/shared"}}}}}`)
	offerings := ParseOpenCodeOfferings(body)
	require.Equal(t, "anthropic/claude-opus-4-7", offerings["payg"]["claude-opus-4-7"].CanonicalModelID)
	require.Equal(t, "anthropic/claude-opus-4-7", offerings["go"]["claude-opus-4-7"].CanonicalModelID)
	require.Empty(t, offerings["payg"]["shared"].CanonicalModelID)
	require.Empty(t, offerings["payg"]["claude-opus-4-7-free"].CanonicalModelID)
	require.Equal(t, "openai/shared-longer", offerings["payg"]["shared-longer"].CanonicalModelID)
	require.Equal(t, "openai/shared", offerings["payg"]["shared-alias"].CanonicalModelID)
	require.Equal(t, "another/shared", offerings["go"]["shared"].CanonicalModelID)
}
