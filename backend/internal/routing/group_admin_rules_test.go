package routing

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

func TestSanitizeGroupMessagesDispatchFieldsPreservesExplicitConfig(t *testing.T) {
	t.Parallel()

	group := &Group{
		AllowedProtocols:      []capability.ProtocolID{capability.ProtocolAnthropicMessages},
		AllowMessagesDispatch: true,
		DefaultMappedModel:    "gpt-5.6-sol",
	}

	SanitizeGroupMessagesDispatchFields(group)

	require.True(t, group.AllowMessagesDispatch)
	require.Equal(t, "gpt-5.6-sol", group.DefaultMappedModel)
}
