package modelidentity_test

import (
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/gateway/provider/modelidentity"
	"github.com/stretchr/testify/require"
)

func TestUsageBillingModelCandidates_BareGPT56ExcludesSol(t *testing.T) {
	require.Equal(t,
		[]string{"gpt-5.6"},
		modelidentity.UsageCandidates("gpt-5.6"),
	)
	require.Equal(t,
		[]string{"openai/gpt-5.6"},
		modelidentity.UsageCandidates("openai/gpt-5.6"),
	)
}

// TestUsageCandidatesPreserveModelID 检查带前缀、日期和档位的计费模型身份。
func TestUsageCandidatesPreserveModelID(t *testing.T) {
	models := []string{
		"gpt-6-astra",
		"openai/gpt-6-astra",
		"gpt-6-astra-preview",
		"openai/gpt-6-astra-20260901",
		"gpt-6-astral",
		"gpt5.6",
		"gpt-5.6-high",
		"gpt-5.6-max",
		"gpt-5.6-2026-07-09",
		"gpt-5.6-20260709",
		"openai/gpt-5.6-max",
	}

	for _, model := range models {
		t.Run(model, func(t *testing.T) {
			require.Equal(t, []string{model}, modelidentity.UsageCandidates(model))
		})
	}
}
