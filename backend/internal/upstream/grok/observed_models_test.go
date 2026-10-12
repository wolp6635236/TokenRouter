package grok

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractGrokModelIDsFromModelsBody(t *testing.T) {
	body := []byte(`{"object":"list","data":[{"id":"grok-4.5"},{"id":"grok-4.3"},{"id":"grok-4.5"}]}`)
	ids := ExtractModelIDs(body)
	require.Equal(t, []string{"grok-4.5", "grok-4.3"}, ids)
}
