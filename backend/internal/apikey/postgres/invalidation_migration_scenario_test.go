package postgres

// 本文件检查 invalidation_outbox.go 使用的迁移结构和失效触发条件，覆盖
// migrations/212_auth_cache_invalidation_outbox.sql 与
// migrations/258_extend_api_key_auth_cache_invalidation.sql。

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/migrations"
)

func TestAuthCacheInvalidationMigration_SecurityCoverageAndNoPlaintextPayload(t *testing.T) {
	content, err := migrations.FS.ReadFile("212_auth_cache_invalidation_outbox.sql")
	require.NoError(t, err)
	sqlText := string(content)
	for _, required := range []string{
		"encode(sha256(convert_to(raw_key, 'UTF8')), 'hex')",
		"OLD.key", "OLD.status", "OLD.deleted_at", "OLD.user_id", "OLD.group_id",
		"OLD.ip_whitelist", "OLD.ip_blacklist", "OLD.expires_at",
		"trg_users_auth_cache_invalidation", "trg_groups_auth_cache_invalidation",
		"trg_user_allowed_groups_auth_cache_invalidation", "FOR EACH ROW",
		"delivery_stage", "claimed_at", "available_at",
	} {
		require.Contains(t, sqlText, required)
	}
	require.NotContains(t, sqlText, "quota_used IS DISTINCT")
	require.NotContains(t, sqlText, "last_used_at IS DISTINCT")

	plaintext := "sk-plaintext-must-not-be-stored"
	sum := sha256.Sum256([]byte(plaintext))
	require.Len(t, hex.EncodeToString(sum[:]), 64)
	require.NotContains(t, sqlText, plaintext)
}

func TestAuthCacheInvalidationBillingMigrationCoversSubscriptionBinding(t *testing.T) {
	content, err := migrations.FS.ReadFile("258_extend_api_key_auth_cache_invalidation.sql")
	require.NoError(t, err)
	sqlText := string(content)
	// 结算来源变化会影响鉴权快照，必须由同一触发函数写入失效 outbox。
	require.Contains(t, sqlText, "OLD.billing_mode IS DISTINCT FROM NEW.billing_mode")
	require.Contains(t, sqlText, "OLD.preferred_subscription_id IS DISTINCT FROM NEW.preferred_subscription_id")
	require.Contains(t, sqlText, "OLD.key")
	require.NotContains(t, sqlText, "sk-")
}
