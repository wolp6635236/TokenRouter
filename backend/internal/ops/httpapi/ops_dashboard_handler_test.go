package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestParseOpsTokenStatsDuration(t *testing.T) {
	tests := []struct {
		input string
		want  time.Duration
		ok    bool
	}{
		{input: "30m", want: 30 * time.Minute, ok: true},
		{input: "1h", want: time.Hour, ok: true},
		{input: "1d", want: 24 * time.Hour, ok: true},
		{input: "15d", want: 15 * 24 * time.Hour, ok: true},
		{input: "30d", want: 30 * 24 * time.Hour, ok: true},
		{input: "7d", want: 0, ok: false},
	}

	for _, tt := range tests {
		got, ok := parseOpsTokenStatsDuration(tt.input)
		require.Equal(t, tt.ok, ok, "input=%s", tt.input)
		require.Equal(t, tt.want, got, "input=%s", tt.input)
	}
}

func TestParseOpsTokenStatsFilter_Defaults(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	before := time.Now().UTC()
	filter, err := parseOpsTokenStatsFilter(c)
	after := time.Now().UTC()

	require.NoError(t, err)
	require.NotNil(t, filter)
	require.Equal(t, "30d", filter.TimeRange)
	require.Equal(t, 1, filter.Page)
	require.Equal(t, 20, filter.PageSize)
	require.Equal(t, 0, filter.TopN)
	require.Nil(t, filter.GroupID)
	require.Equal(t, "", filter.Platform)
	require.True(t, filter.StartTime.Before(filter.EndTime))
	require.WithinDuration(t, before.Add(-30*24*time.Hour), filter.StartTime, 2*time.Second)
	require.WithinDuration(t, after, filter.EndTime, 2*time.Second)
}

func TestParseOpsTokenStatsFilter_WithTopN(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(
		http.MethodGet,
		"/?time_range=1h&platform=anthropic&group_id=12&top_n=50",
		nil,
	)

	filter, err := parseOpsTokenStatsFilter(c)
	require.NoError(t, err)
	require.Equal(t, "1h", filter.TimeRange)
	require.Equal(t, "anthropic", filter.Platform)
	require.NotNil(t, filter.GroupID)
	require.Equal(t, int64(12), *filter.GroupID)
	require.Equal(t, 50, filter.TopN)
	require.Equal(t, 0, filter.Page)
	require.Equal(t, 0, filter.PageSize)
}

func TestParseOpsTokenStatsFilter_InvalidParams(t *testing.T) {
	tests := []string{
		"/?time_range=7d",
		"/?group_id=0",
		"/?group_id=abc",
		"/?top_n=0",
		"/?top_n=101",
		"/?top_n=10&page=1",
		"/?top_n=10&page_size=20",
		"/?page=0",
		"/?page_size=0",
		"/?page_size=101",
	}

	for _, rawURL := range tests {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, rawURL, nil)

		_, err := parseOpsTokenStatsFilter(c)
		require.Error(t, err, "url=%s", rawURL)
	}
}

func TestParseOpsLatencyBucketBoundaries(t *testing.T) {
	defaults, err := parseOpsLatencyBucketBoundaries("")
	require.NoError(t, err)
	require.Equal(t, []int64{100, 200, 500, 1000, 2000}, defaults)

	custom, err := parseOpsLatencyBucketBoundaries("1000, 2000,5000,10000,20000")
	require.NoError(t, err)
	require.Equal(t, []int64{1000, 2000, 5000, 10000, 20000}, custom)

	invalid := []string{
		"100,200",
		"0,200,500,1000,2000",
		"100,200,200,1000,2000",
		"100,200,500,1000,86400001",
		"100,abc,500,1000,2000",
	}
	for _, raw := range invalid {
		_, err := parseOpsLatencyBucketBoundaries(raw)
		require.Error(t, err, "raw=%s", raw)
	}
}

func TestPickThroughputBucketSeconds(t *testing.T) {
	require.Equal(t, 60, pickThroughputBucketSeconds(30*time.Minute))
	require.Equal(t, 300, pickThroughputBucketSeconds(6*time.Hour))
	require.Equal(t, 3600, pickThroughputBucketSeconds(48*time.Hour))
}
