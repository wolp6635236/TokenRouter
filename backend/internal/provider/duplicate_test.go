package provider

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestDuplicateProviderNamePreservesSuffixWithinSchemaLimit(t *testing.T) {
	name := duplicateProviderName(strings.Repeat("界", 100))

	require.Equal(t, 100, utf8.RuneCountInString(name))
	require.True(t, strings.HasSuffix(name, " (Copy)"))
}
