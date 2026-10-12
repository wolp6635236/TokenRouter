package rediscache

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFingerprintKey(t *testing.T) {
	tests := []struct {
		name       string
		providerID int64
		expected   string
	}{
		{
			name:       "normal_account_id",
			providerID: 123,
			expected:   "fingerprint:123",
		},
		{
			name:       "zero_account_id",
			providerID: 0,
			expected:   "fingerprint:0",
		},
		{
			name:       "negative_account_id",
			providerID: -1,
			expected:   "fingerprint:-1",
		},
		{
			name:       "max_int64",
			providerID: math.MaxInt64,
			expected:   "fingerprint:9223372036854775807",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := fingerprintKey(tc.providerID)
			require.Equal(t, tc.expected, got)
		})
	}
}
