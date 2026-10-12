package anthropic

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFullClaudeCodeMimicryBetas_DoesNotDefaultRedactThinking(t *testing.T) {
	required := FullClaudeCodeMimicryBetas()

	require.NotContains(t, required, BetaRedactThinking)
	require.Contains(t, required, BetaClaudeCode)
	require.Contains(t, required, BetaOAuth)
	require.Contains(t, required, BetaInterleavedThinking)
}
