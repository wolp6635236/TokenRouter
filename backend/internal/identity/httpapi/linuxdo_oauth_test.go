package httpapi

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSingleLineStripsWhitespace(t *testing.T) {
	require.Equal(t, "hello world", OAuthSingleLine("hello\r\nworld"))
	require.Equal(t, "", OAuthSingleLine("\n\t\r"))
}
