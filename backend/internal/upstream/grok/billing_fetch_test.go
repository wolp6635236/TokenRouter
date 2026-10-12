package grok

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsRetryableGrokBillingStatus(t *testing.T) {
	tests := []struct {
		status int
		want   bool
	}{
		{status: http.StatusBadGateway, want: true},
		{status: http.StatusServiceUnavailable, want: true},
		{status: http.StatusGatewayTimeout, want: true},
		{status: http.StatusUnauthorized, want: false},
		{status: http.StatusForbidden, want: false},
		{status: http.StatusTooManyRequests, want: false},
	}

	for _, tt := range tests {
		t.Run(strconv.Itoa(tt.status), func(t *testing.T) {
			require.Equal(t, tt.want, IsRetryableBillingStatus(tt.status))
		})
	}
}
