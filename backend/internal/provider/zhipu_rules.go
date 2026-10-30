package provider

// zhipu_rules.go declares Zhipu access modes, protocol roots, quota queries,
// and provider policies. Coding mode exposes percentage quota windows, while
// pay-as-you-go has no fixed usage query. Endpoint defaults retain the legacy
// Responses-shaped root for address lookup; protocol admission separately
// limits execution to Messages and Chat.

// Zhipu endpoint constants distinguish paid usage and coding Chat roots while
// retaining the shared Messages root used by administrator endpoint presets.
const (
	DefaultZhipuPayGBaseURL      = "https://open.bigmodel.cn/api/paas/v4"
	DefaultZhipuCodingBaseURL    = "https://open.bigmodel.cn/api/coding/paas/v4"
	DefaultZhipuAnthropicBaseURL = "https://open.bigmodel.cn/api/anthropic"
)

// zhipuRules declares Zhipu's quota formats and independently selected provider policies.
func zhipuRules() platformRules {
	return platformRules{
		text: &textEndpointRules{
			defaultMode: ProviderModePayG,
			modes: map[string]textModeRules{
				ProviderModePayG: {
					chatBase:      DefaultZhipuPayGBaseURL,
					messagesBase:  DefaultZhipuAnthropicBaseURL,
					responsesBase: DefaultZhipuPayGBaseURL,
				},
				ProviderModeCoding: {
					chatBase:       DefaultZhipuCodingBaseURL,
					messagesBase:   DefaultZhipuAnthropicBaseURL,
					responsesBase:  DefaultZhipuCodingBaseURL,
					usageAdapter:   UpstreamUsageAdapterZhipuCoding,
					codingProvider: PlatformZhipu,
				},
			},
		},
		usage: usageMonitoringRules{
			enabled:       true,
			fixedAdapter:  true,
			officialHosts: []string{"open.bigmodel.cn", "api.z.ai"},
		},
		countForbidden:           true,
		temporaryBalanceCooldown: true,
		explicitClaudePricing:    true,
		messagesAuthOverride:     true,
		apiKeyTLSFingerprint:     true,
		repairToolSchema:         true,
		apiKeyHeaderOverrides:    true,
	}
}
