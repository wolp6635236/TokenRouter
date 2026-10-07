package provider

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBuildProviderSchedulingThresholdReason_UsesSourceAndFallbackMessage(t *testing.T) {
	raw := BuildProviderSchedulingThresholdReason(" \t ")

	var payload map[string]string
	require.NoError(t, json.Unmarshal([]byte(raw), &payload))
	require.Equal(t, ProviderSchedulingThresholdReasonSource, payload["source"])
	require.Equal(t, defaultProviderSchedulingThresholdErrorMessage, payload["error_message"])
	require.True(t, IsProviderSchedulingThresholdReason(raw))
}

func TestIsProviderSchedulingThresholdReason(t *testing.T) {
	require.True(t, IsProviderSchedulingThresholdReason(BuildProviderSchedulingThresholdReason("threshold reached")))
	require.False(t, IsProviderSchedulingThresholdReason(BuildTempUnschedReasonPayload("", "temporary block")))
	require.False(t, IsProviderSchedulingThresholdReason("plain text reason"))
}

func TestTempUnschedStateFromStoredReason_EmptyReasonUsesFallbackErrorMessage(t *testing.T) {
	state := tempUnschedStateFromStoredReason(" \n ", 1735689600)

	require.NotNil(t, state)
	require.Equal(t, int64(1735689600), state.UntilUnix)
	require.Equal(t, defaultTempUnschedReasonErrorMessage, state.ErrorMessage)
}

func TestTempUnschedStateFromStoredReason_MissingRuleIndexIsSystemRule(t *testing.T) {
	state := tempUnschedStateFromStoredReason(`{"error_message":"system cooldown"}`, 123)
	require.Equal(t, -1, state.RuleIndex)
}

func TestTempUnschedStateFromStoredReason_SchedulingThresholdJSONWithoutMessageUsesThresholdFallback(t *testing.T) {
	raw := `{"source":"` + ProviderSchedulingThresholdReasonSource + `"}`

	state := tempUnschedStateFromStoredReason(raw, 1735689600)

	require.NotNil(t, state)
	require.Equal(t, int64(1735689600), state.UntilUnix)
	require.Equal(t, defaultProviderSchedulingThresholdErrorMessage, state.ErrorMessage)
}

func TestBuildDetailedProviderSchedulingThresholdReason_IncludesReadableFields(t *testing.T) {
	now := time.Unix(1735689600, 0).UTC()
	until := now.Add(5 * time.Hour)

	raw := BuildDetailedProviderSchedulingThresholdReason(ProviderSchedulingThresholdReasonInput{
		Platform:         PlatformOpenAI,
		Window:           "7d",
		ThresholdPercent: 90,
		UsedPercent:      92.5,
		Until:            until,
		Now:              now,
	})

	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &payload))
	require.Equal(t, ProviderSchedulingThresholdReasonSource, payload["source"])
	require.Equal(t, PlatformOpenAI, payload["platform"])
	require.Equal(t, "7d", payload["window"])
	require.Equal(t, float64(90), payload["threshold_percent"])
	require.Equal(t, float64(92.5), payload["used_percent"])
	require.Equal(t, float64(until.Unix()), payload["until_unix"])
	require.Equal(t, float64(now.Unix()), payload["triggered_at_unix"])
	require.Contains(t, payload["error_message"], "openai scheduling threshold reached")
	require.Contains(t, payload["error_message"], "92.5% used >= 90%")
}
