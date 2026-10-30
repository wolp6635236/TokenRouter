package provider

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// codex_image_config_test.go verifies OAuth image driver selection and storage
// validation. Provider overrides belong to the selected record and blank values
// restore the default, while API-key providers cannot consume OAuth overrides.

// TestCodexImageResponsesModel checks missing, blank and explicit provider overrides.
// OAuth records use their own configured driver while unrelated credential types
// cannot accidentally inherit an OAuth-only selection.
func TestCodexImageResponsesModel(t *testing.T) {
	for _, value := range []any{nil, "", "  ", " gpt-6.1-sol ", false} {
		record := &Record{Platform: PlatformOpenAI, Type: ProviderTypeOAuth, Extra: map[string]any{CodexImageResponsesModelKey: value}}
		expected := DefaultCodexImageResponsesModel
		if value == " gpt-6.1-sol " {
			expected = "gpt-6.1-sol"
		}
		require.Equal(t, expected, record.CodexImageResponsesModel())
		record.Type = ProviderTypeAPIKey
		require.Equal(t, DefaultCodexImageResponsesModel, record.CodexImageResponsesModel())
	}
}

// TestNormalizeCodexImageResponsesModel verifies saved and merge-patch values.
// Empty and null updates can clear an override; malformed identifiers fail before
// persistence and whitespace around a valid model is removed.
func TestNormalizeCodexImageResponsesModel(t *testing.T) {
	for _, value := range []any{nil, "", "  ", " gpt-6.1-sol ", "invalid model", "model\nextra", true, 42} {
		extra := map[string]any{CodexImageResponsesModelKey: value, "keep": true}
		err := NormalizeCodexImageResponsesModel(extra)
		switch value {
		case "invalid model", "model\nextra", true, 42:
			require.Error(t, err)
		default:
			require.NoError(t, err)
			if value == " gpt-6.1-sol " {
				require.Equal(t, "gpt-6.1-sol", extra[CodexImageResponsesModelKey])
			}
		}
		require.Equal(t, true, extra["keep"])
	}
	require.NoError(t, NormalizeCodexImageResponsesModel(nil))
}
