package httpapi

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/provider/httpapi/dto"
)

func TestEnrichShadowParentInfo(t *testing.T) {
	pid := int64(100)
	parent := &provider.Record{
		ID: 100,
		Credentials: map[string]any{
			"email":                   "owner@example.com",
			"plan_type":               "pro",
			"subscription_expires_at": "2026-12-31T00:00:00Z",
			"chatgpt_account_id":      "acct_123",
		},
		Extra: map[string]any{"privacy_mode": "training_off"},
	}
	parents := map[int64]*provider.Record{100: parent}

	shadow := ProviderWithConcurrency{Provider: &dto.Provider{ID: 200, ParentProviderID: &pid}}
	normal := ProviderWithConcurrency{Provider: &dto.Provider{ID: 1}}
	orphan := ProviderWithConcurrency{Provider: &dto.Provider{ID: 201, ParentProviderID: ptrInt64(999)}}
	items := []ProviderWithConcurrency{shadow, normal, orphan}

	EnrichShadowParentInfo(items, parents)

	require.Equal(t, "owner@example.com", items[0].ParentEmail, "影子回填母提供商邮箱")
	require.Equal(t, "pro", items[0].ParentPlanType)
	require.Equal(t, "training_off", items[0].ParentPrivacyMode)
	require.Equal(t, "2026-12-31T00:00:00Z", items[0].ParentSubscriptionExpiresAt)
	require.Equal(t, "acct_123", items[0].ParentChatGPTAccountID)

	require.Empty(t, items[1].ParentEmail, "非影子不回填")
	require.Empty(t, items[2].ParentEmail, "母提供商缺失时优雅留空")
}

func ptrInt64(v int64) *int64 { return &v }
