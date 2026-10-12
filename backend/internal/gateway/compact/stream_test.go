package compact

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStreamPayloadInjectsIDOnlyWhenRequired(t *testing.T) {
	calls := 0
	id := func() string { calls++; return "resp_generated" }
	_, ok := StreamPayload([]byte(`{"id":"resp_existing","output":[]}`), id)
	require.True(t, ok)
	require.Zero(t, calls)
	result, ok := StreamPayload([]byte(`{"output":[]}`), id)
	require.True(t, ok)
	require.Equal(t, 1, calls)
	require.Contains(t, string(result), `"id":"resp_generated"`)
}
