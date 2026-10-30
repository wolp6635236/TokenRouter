package moderation

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestContentModerationWeightedAPIKeySelectionHonorsPriorityAndFreeze uses two
// keys with a five-to-one scheduling ratio and then freezes the larger weight.
// It checks sixty selections split fifty/ten and subsequent selections
// restricted to the remaining unfrozen key.
func TestContentModerationWeightedAPIKeySelectionHonorsPriorityAndFreeze(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.APIKeys = []string{"sk-high", "sk-low"}
	cfg.APIKeyMetadata = []ContentModerationAPIKeyMetadata{
		{KeyHash: moderationAPIKeyHash("sk-high"), Priority: 100, Note: "Tier 5"},
		{KeyHash: moderationAPIKeyHash("sk-low"), Priority: 20, Note: "Tier 1"},
	}
	cfg.normalize()
	svc := newTestModeration(nil, nil, nil, nil, nil, nil, nil)
	svc.Start()
	counts := map[string]int{}
	for range 60 {
		key, ok := svc.nextUsableAPIKey(cfg)
		require.True(t, ok)
		counts[key]++
	}
	require.Equal(t, 50, counts["sk-high"])
	require.Equal(t, 10, counts["sk-low"])

	// Publish the fixture freeze under the same lock used by weighted selection,
	// so subsequent calls see that exclusion alongside their accumulated eligible key weights.
	svc.keyHealthMu.Lock()
	svc.keyHealth[moderationAPIKeyHash("sk-high")] = &contentModerationKeyHealth{FrozenUntil: time.Now().Add(time.Minute)}
	svc.keyHealthMu.Unlock()
	for range 10 {
		key, ok := svc.nextUsableAPIKey(cfg)
		require.True(t, ok)
		require.Equal(t, "sk-low", key)
	}
}
