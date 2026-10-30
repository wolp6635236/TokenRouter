package provider

// kimi_rules.go declares Kimi access modes, protocol roots, usage queries, and
// provider policies. Pay-as-you-go uses Moonshot balance queries, while coding
// uses percentage quota windows; both modes use stateless Responses and permit
// recoverable balance cooldowns under the existing provider health workflow.

// Kimi endpoint constants supply mode-specific forwarding defaults shared with
// administrator presets; protocol request builders append their operation paths.
const (
	DefaultKimiPayGBaseURL            = "https://api.moonshot.cn/v1"
	DefaultKimiCodingBaseURL          = "https://api.kimi.com/coding/v1"
	DefaultKimiPayGAnthropicBaseURL   = "https://api.moonshot.cn/anthropic"
	DefaultKimiCodingAnthropicBaseURL = "https://api.kimi.com/coding"
)

// kimiRules declares Kimi's endpoint modes and independently selected provider policies.
func kimiRules() platformRules {
	return platformRules{
		text: &textEndpointRules{
			defaultMode:        ProviderModePayG,
			statelessResponses: true,
			modes: map[string]textModeRules{
				ProviderModePayG: {
					chatBase:      DefaultKimiPayGBaseURL,
					messagesBase:  DefaultKimiPayGAnthropicBaseURL,
					responsesBase: DefaultKimiPayGBaseURL,
					usageAdapter:  UpstreamUsageAdapterKimiBalance,
				},
				ProviderModeCoding: {
					chatBase:       DefaultKimiCodingBaseURL,
					messagesBase:   DefaultKimiCodingAnthropicBaseURL,
					responsesBase:  DefaultKimiCodingBaseURL,
					usageAdapter:   UpstreamUsageAdapterKimiCoding,
					codingProvider: PlatformKimi,
				},
			},
		},
		usage: usageMonitoringRules{
			enabled:       true,
			fixedAdapter:  true,
			officialHosts: []string{"api.moonshot.cn", "api.kimi.com"},
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
