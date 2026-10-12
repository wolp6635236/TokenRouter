package httpapi

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildProviderTodayStatsBatchCacheKey(t *testing.T) {
	tests := []struct {
		name string
		ids  []int64
		want string
	}{
		{"empty", nil, "providers_today_stats_empty"},
		{"single", []int64{42}, "providers_today_stats:42"},
		{"multiple", []int64{1, 2, 3}, "providers_today_stats:1,2,3"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildProviderTodayStatsBatchCacheKey(tc.ids)
			require.Equal(t, tc.want, got)
		})
	}
}
