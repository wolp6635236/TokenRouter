package openai

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseOpenAIRateLimitResetCreditDetails_CompatibleContainers(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "credits",
			body: `{"credits":[{"id":"secret-id","expires_at":"2026-07-03T04:05:06Z"}]}`,
			want: []string{"2026-07-03T04:05:06Z"},
		},
		{
			name: "rate limit reset credits",
			body: `{"rate_limit_reset_credits":[{"expiresAt":"2026-07-04T04:05:06Z"}]}`,
			want: []string{"2026-07-04T04:05:06Z"},
		},
		{
			name: "items",
			body: `{"items":[{"expires_at":"2026-07-05T04:05:06Z"}]}`,
			want: []string{"2026-07-05T04:05:06Z"},
		},
		{
			name: "data",
			body: `{"data":[{"expires_at":"2026-07-06T04:05:06Z"}]}`,
			want: []string{"2026-07-06T04:05:06Z"},
		},
		{
			name: "array",
			body: `[{"expires_at":"2026-07-07T04:05:06Z"}]`,
			want: []string{"2026-07-07T04:05:06Z"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseOpenAIRateLimitResetCreditDetails([]byte(tt.body))
			require.NoError(t, err)
			require.Len(t, got.Credits, len(tt.want))
			for i := range tt.want {
				require.Equal(t, tt.want[i], got.Credits[i].ExpiresAt)
			}
			encoded, err := json.Marshal(got.Credits)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "secret-id")
		})
	}
}

func TestParseOpenAIRateLimitResetCreditDetails_PreservesAvailableCreditOrder(t *testing.T) {
	body := []byte(`{
		"availableCount":"2",
		"credits":[
			{"reset_type":"codex_rate_limits","status":"redeemed","expires_at":"2026-07-01T04:05:06Z"},
			{"reset_type":"codex_rate_limits","status":"available","expires_at":"2026-07-04T04:05:06Z"},
			{"resetType":"codex_rate_limits","status":"available","expiresAt":"2026-07-03T04:05:06Z"},
			{"reset_type":"other","status":"available","expires_at":"2026-07-02T04:05:06Z"}
		]
	}`)

	details, err := ParseOpenAIRateLimitResetCreditDetails(body)
	require.NoError(t, err)
	require.NotNil(t, details.AvailableCount)
	require.Equal(t, 2, *details.AvailableCount)
	require.Equal(t, []OpenAIRateLimitResetCreditDetail{
		{ExpiresAt: "2026-07-04T04:05:06Z"},
		{ExpiresAt: "2026-07-03T04:05:06Z"},
	}, details.Credits)
}
