package provider

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

// platform_policy_scenario_test.go checks platform declarations through the
// consumers in record.go, protocol_target.go, usage_config.go, and
// input_validation.go. These scenarios protect mode-specific endpoints and
// distinguish forwarding capabilities from monitoring, billing, and health policy.

// TestPlatformPolicyModes exercises endpoint and usage selection for every
// declared access mode, including a historical invalid DeepSeek mode. It checks
// that each platform keeps its own roots, adapter, and save-validation result.
func TestPlatformPolicyModes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		platform string // persisted provider platform identity
		mode     string // supplied access mode spelling
		chat     string // expected Chat endpoint root
		messages string // expected Messages endpoint root
		adapter  string // expected fixed usage adapter
		coding   string // expected coding quota identity
		rejected bool   // mode cannot be saved
	}{
		{PlatformKimi, ProviderModePayG, DefaultKimiPayGBaseURL, DefaultKimiPayGAnthropicBaseURL, UpstreamUsageAdapterKimiBalance, "", false},
		{PlatformKimi, ProviderModeCoding, DefaultKimiCodingBaseURL, DefaultKimiCodingAnthropicBaseURL, UpstreamUsageAdapterKimiCoding, PlatformKimi, false},
		{PlatformZhipu, ProviderModePayG, DefaultZhipuPayGBaseURL, DefaultZhipuAnthropicBaseURL, "", "", false},
		{PlatformZhipu, ProviderModeCoding, DefaultZhipuCodingBaseURL, DefaultZhipuAnthropicBaseURL, UpstreamUsageAdapterZhipuCoding, PlatformZhipu, false},
		{PlatformDeepseek, ProviderModePayG, DefaultDeepseekBaseURL, DefaultDeepseekAnthropicBaseURL, UpstreamUsageAdapterDeepseekBalance, "", false},
		{PlatformDeepseek, ProviderModeCoding, DefaultDeepseekBaseURL, DefaultDeepseekAnthropicBaseURL, "", "", true},
	}
	for _, test := range cases {
		t.Run(test.platform+"/"+test.mode, func(t *testing.T) {
			record := &Record{Platform: test.platform, Type: ProviderTypeAPIKey, Credentials: map[string]any{
				"provider_mode":      test.mode,
				UpstreamProtocolsKey: []string{string(capability.ProtocolAnthropicMessages), string(capability.ProtocolOpenAIChatCompletions)},
			}}
			require.Equal(t, test.chat, record.OpenAIBaseURL(true))
			require.Equal(t, test.messages, (ProtocolTarget{Record: record, Protocol: capability.ProtocolAnthropicMessages}).BaseURL())
			adapter, fixed := PlatformUsageAdapter(record)
			require.True(t, fixed)
			require.Equal(t, test.adapter, adapter)
			require.Equal(t, test.coding, record.GetCodingPlanProvider())
			err := NormalizeTextEndpointCredentials(record, false)
			if test.rejected {
				require.ErrorContains(t, err, "DeepSeek does not support coding plan mode")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestPlatformPoliciesDoNotInferUnrelatedCapabilities compares platforms that
// share a wire protocol but have different health and billing obligations. It
// protects OpenAI and OpenCode from inheriting supplier-specific monitoring or
// stateless behavior merely because they support API-key Responses.
func TestPlatformPoliciesDoNotInferUnrelatedCapabilities(t *testing.T) {
	t.Parallel()
	cases := []struct {
		platform        string // persisted platform under test
		nativeResponses bool   // API-key Responses option exists
		stateless       bool   // continuation must be removed
		monitor         bool   // periodic monitoring is admitted
		balanceCooldown bool   // balance errors are recoverable
		countForbidden  bool   // forbidden failures use counters
		explicitPricing bool   // Claude aliases require configured prices
	}{
		{PlatformKimi, true, true, true, true, true, true},
		{PlatformDeepseek, true, true, true, true, true, true},
		{PlatformZhipu, false, false, true, true, true, true},
		{PlatformOpenAI, true, false, false, false, true, false},
		{PlatformOpenCode, true, false, false, false, false, false},
		{PlatformAnthropic, false, false, false, false, false, false},
		{PlatformGrok, true, false, false, false, false, false},
		{"unknown", false, false, false, false, false, false},
	}
	for _, test := range cases {
		t.Run(test.platform, func(t *testing.T) {
			record := &Record{Platform: test.platform, Type: ProviderTypeAPIKey}
			require.Equal(t, test.nativeResponses, record.SupportsAPIKeyResponses())
			require.Equal(t, test.stateless, record.RequiresStatelessResponses())
			require.Equal(t, test.monitor, record.SupportsUsageMonitoring())
			require.Equal(t, test.balanceCooldown, record.UsesTemporaryBalanceCooldown())
			require.Equal(t, test.countForbidden, record.UsesCountedForbiddenPolicy())
			require.Equal(t, test.explicitPricing, record.RequiresExplicitClaudePricing())
		})
	}
}
