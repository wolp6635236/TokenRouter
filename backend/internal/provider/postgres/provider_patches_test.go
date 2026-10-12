package postgres

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGrokBillingSnapshotIsSchedulerNeutral(t *testing.T) {
	t.Parallel()

	require.True(t, IsSchedulerNeutralExtraKey("grok_billing_snapshot"))
	require.False(t, ShouldEnqueueSchedulerOutboxForExtraUpdates(map[string]any{
		"grok_billing_snapshot": map[string]any{"usage_percent": 50},
	}))
}

func TestOpenAIResetCreditSnapshotIsSchedulerNeutral(t *testing.T) {
	t.Parallel()

	require.True(t, IsSchedulerNeutralExtraKey("codex_reset_credit_snapshot"))
	require.False(t, ShouldEnqueueSchedulerOutboxForExtraUpdates(map[string]any{
		"codex_reset_credit_snapshot": map[string]any{"available_count": 1},
	}))
}

func TestCNUsageMonitorSnapshotIsSchedulerNeutralForGenericUpdates(t *testing.T) {
	t.Parallel()
	require.True(t, IsSchedulerNeutralExtraKey("cn_usage_monitor_snapshot"))
	require.False(t, ShouldEnqueueSchedulerOutboxForExtraUpdates(map[string]any{
		"cn_usage_monitor_snapshot": map[string]any{"version": 1},
	}))
}

func TestShouldEnqueueSchedulerOutboxForExtraUpdates_CompactConfigurationKeysAreRelevant(t *testing.T) {
	updates := map[string]any{
		"openai_compact_mode":              "force_off",
		"openai_native_compaction_v2_mode": "force_on",
	}

	if !ShouldEnqueueSchedulerOutboxForExtraUpdates(updates) {
		t.Fatalf("expected compact capability updates to enqueue scheduler outbox")
	}
}

func TestShouldEnqueueSchedulerOutboxForExtraUpdates_OpenAITextRouteIsRelevant(t *testing.T) {
	updates := map[string]any{
		"openai_text_route_mode": "force_chat_completions",
	}

	if !ShouldEnqueueSchedulerOutboxForExtraUpdates(updates) {
		t.Fatalf("expected responses capability updates to enqueue scheduler outbox")
	}
}

func TestShouldEnqueueSchedulerOutboxForExtraUpdates_QoderQuotaSnapshotIsNeutral(t *testing.T) {
	updates := map[string]any{
		"qoder_quota_snapshot": map[string]any{
			"user_type": "teams",
			"user_quota": map[string]any{
				"total":     2940,
				"used":      2,
				"remaining": 2938,
			},
		},
		"qoder_quota_updated_at": "2026-07-05T10:00:00Z",
	}

	if ShouldEnqueueSchedulerOutboxForExtraUpdates(updates) {
		t.Fatalf("expected qoder quota snapshot updates to skip scheduler outbox")
	}
}
