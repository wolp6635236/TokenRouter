package bridge

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeToolParameters(t *testing.T) {
	tests := []struct {
		name     string
		input    json.RawMessage
		expected string
	}{
		{
			name:     "nil input",
			input:    nil,
			expected: `{"type":"object","properties":{}}`,
		},
		{
			name:     "empty input",
			input:    json.RawMessage(``),
			expected: `{"type":"object","properties":{}}`,
		},
		{
			name:     "null input",
			input:    json.RawMessage(`null`),
			expected: `{"type":"object","properties":{}}`,
		},
		{
			name:     "object without properties",
			input:    json.RawMessage(`{"type":"object"}`),
			expected: `{"type":"object","properties":{}}`,
		},
		{
			name:     "object with properties",
			input:    json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}}}`),
			expected: `{"type":"object","properties":{"city":{"type":"string"}}}`,
		},
		{
			name:     "non-object type",
			input:    json.RawMessage(`{"type":"string"}`),
			expected: `{"type":"string"}`,
		},
		{
			name:     "object with additional fields preserved",
			input:    json.RawMessage(`{"type":"object","required":["name"]}`),
			expected: `{"type":"object","required":["name"],"properties":{}}`,
		},
		{
			name:     "invalid JSON passthrough",
			input:    json.RawMessage(`not json`),
			expected: `not json`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeToolParameters(tt.input)
			if tt.name == "invalid JSON passthrough" {
				assert.Equal(t, tt.expected, string(result))
			} else {
				assert.JSONEq(t, tt.expected, string(result))
			}
		})
	}
}
