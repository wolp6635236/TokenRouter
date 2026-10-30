package upstream

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestRetryAfterResetTime compares equivalent numeric seconds and a future HTTP
// date against a fixed clock. It also checks rejection of invalid, nonpositive,
// nonfinite, oversized, and past values.
func TestRetryAfterResetTime(t *testing.T) {
	now := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	for _, value := range []string{"17", now.Add(17 * time.Second).Format(http.TimeFormat)} {
		reset := RetryAfterResetTime(http.Header{"Retry-After": []string{value}}, now)
		require.NotNil(t, reset)
		require.Equal(t, now.Add(17*time.Second), *reset)
	}
	for _, value := range []string{"", "invalid", "-1", "0", "NaN", "Inf", "1e100", now.Add(-time.Hour).Format(http.TimeFormat)} {
		require.Nil(t, RetryAfterResetTime(http.Header{"Retry-After": []string{value}}, now), value)
	}
}
