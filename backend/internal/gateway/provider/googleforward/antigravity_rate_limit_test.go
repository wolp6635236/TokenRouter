package googleforward_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/upstream/antigravity"
	"github.com/stretchr/testify/require"
)

// 编译期接口断言
var _ gatewayprovider.ExecutionProviderStore = (*stubAntigravityProviderRepo)(nil)

type rateLimitCall struct {
	providerID int64
	resetAt    time.Time
}

type modelRateLimitCall struct {
	providerID int64
	modelKey   string // 存储的 key（应该是官方模型 ID，如 "claude-sonnet-4-5"）
	resetAt    time.Time
}

type extraUpdateCall struct {
	providerID int64
	updates    map[string]any
}

type stubAntigravityProviderRepo struct {
	gatewayprovider.ExecutionProviderStore

	rateCalls           []rateLimitCall
	modelRateLimitCalls []modelRateLimitCall
	extraUpdateCalls    []extraUpdateCall
}

func (s *stubAntigravityProviderRepo) SetRateLimited(ctx context.Context, id int64, resetAt time.Time) error {
	s.rateCalls = append(s.rateCalls, rateLimitCall{providerID: id, resetAt: resetAt})
	return nil
}

func (s *stubAntigravityProviderRepo) SetModelRateLimit(ctx context.Context, id int64, modelKey string, resetAt time.Time, reason ...string) error {
	s.modelRateLimitCalls = append(s.modelRateLimitCalls, modelRateLimitCall{providerID: id, modelKey: modelKey, resetAt: resetAt})
	return nil
}

func (s *stubAntigravityProviderRepo) UpdateExtra(ctx context.Context, id int64, updates map[string]any) error {
	s.extraUpdateCalls = append(s.extraUpdateCalls, extraUpdateCall{providerID: id, updates: updates})
	return nil
}

func buildGeminiRateLimitBody(delay string) []byte {
	return []byte(fmt.Sprintf(`{"error":{"message":"too many requests","details":[{"metadata":{"quotaResetDelay":%q}}]}}`, delay))
}

func TestParseGeminiRateLimitResetTime_QuotaResetDelay_RoundsUp(t *testing.T) {
	// Avoid flakiness around Unix second boundaries.
	for {
		now := time.Now()
		if now.Nanosecond() < 800*1e6 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	baseUnix := time.Now().Unix()
	ts := parseGeminiReset(buildGeminiRateLimitBody("0.1s"))
	require.NotNil(t, ts)
	require.Equal(t, baseUnix+1, *ts, "fractional seconds should be rounded up to the next second")
}

func TestResolveAntigravityForwardBaseURL(t *testing.T) {
	oldBaseURLs := append([]string(nil), antigravity.BaseURLs...)
	defer func() {
		antigravity.BaseURLs = oldBaseURLs
	}()

	prodURL := "https://prod.test"
	dailyURL := "https://daily.test"
	antigravity.BaseURLs = []string{prodURL, dailyURL}

	tests := []struct {
		name     string
		env      string
		provider *gatewayprovider.ExecutionProvider
		want     string
	}{
		{
			name:     "pro defaults to daily",
			provider: &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Credentials: map[string]any{"plan_type": " Pro "}}},

			want: dailyURL,
		},

		{
			name:     "ultra defaults to daily",
			provider: &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Credentials: map[string]any{"plan_type": "ULTRA"}}},

			want: dailyURL,
		},

		{
			name:     "free defaults to prod",
			provider: &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Credentials: map[string]any{"plan_type": "free"}}},
			want:     prodURL,
		},

		{
			name:     "abnormal defaults to prod",
			provider: &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Credentials: map[string]any{"plan_type": "Abnormal"}}},
			want:     prodURL,
		},

		{
			name:     "unknown defaults to prod",
			provider: &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Credentials: map[string]any{"plan_type": "enterprise"}}},
			want:     prodURL,
		},

		{
			name:     "malformed defaults to prod",
			provider: &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Credentials: map[string]any{"plan_type": map[string]any{"name": "pro"}}}},
			want:     prodURL,
		},

		{
			name:     "missing defaults to prod",
			provider: &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Credentials: map[string]any{}}},
			want:     prodURL,
		},

		{name: "nil provider defaults to prod", provider: nil, want: prodURL},

		{
			name: "daily override wins for free tier",
			env:  " daily ",

			provider: &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Credentials: map[string]any{"plan_type": "free"}}},

			want: dailyURL,
		},

		{
			name:     "prod override keeps production for paid tier",
			env:      " prod ",
			provider: &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Credentials: map[string]any{"plan_type": "pro"}}},
			want:     prodURL,
		},

		{
			name:     "unknown override keeps production",
			env:      "unknown",
			provider: &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Credentials: map[string]any{"plan_type": "pro"}}},
			want:     prodURL,
		},

		{
			name: "prod override wins for paid tier",
			env:  " PROD ",

			provider: &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Credentials: map[string]any{"plan_type": "pro"}}},

			want: prodURL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GATEWAY_ANTIGRAVITY_FORWARD_BASE_URL", tt.env)
			require.Equal(t, tt.want, antigravity.ResolveAntigravityForwardBaseURL(os.Getenv("GATEWAY_ANTIGRAVITY_FORWARD_BASE_URL"), provideradapter.AntigravityPaidTier(gatewayprovider.ExecutionRecord(tt.provider))))
		})
	}
}
