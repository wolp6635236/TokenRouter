package requeststate

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractGrokResponsesReasoningEffortSupportsOpenAICompatibleField(t *testing.T) {
	t.Parallel()

	effort := ExtractOpenAIReasoningEffortFromBody(
		[]byte(`{"model":"grok-4.3","reasoning_effort":"high"}`))
	require.NotNil(t, effort)
	require.Equal(t, "high", *effort)
}
