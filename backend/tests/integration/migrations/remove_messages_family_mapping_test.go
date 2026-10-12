//go:build integration

package migrations_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/migrations"
)

// TestMigration279RemovesOnlyMessagesFamilyMapping 验证迁移只删除系列字段，精确覆盖、分组 ID 和其它策略保持原值，重复执行不改变结果。
func TestMigration279RemovesOnlyMessagesFamilyMapping(t *testing.T) {
	tx := historicalTx(t, "279_")
	ctx := context.Background()
	_, err := tx.ExecContext(ctx, `INSERT INTO groups(id,name,messages_dispatch_model_config,routing_policy) VALUES
      (97901,'with exact','{"opus_mapped_model":"gpt-5.4","sonnet_mapped_model":"gpt-5.3-codex","haiku_mapped_model":"gpt-5.4-mini","exact_model_mappings":{"claude-sonnet-4-6":"explicit-target"}}','{"enabled":true,"model_mapping":{"public":"upstream"}}'),
      (97902,'family only','{"sonnet_mapped_model":"gpt-5.4"}','{}'),
      (97903,'empty','{}','{}');`)
	require.NoError(t, err)
	sql, err := migrations.FS.ReadFile("279_remove_messages_family_mapping.sql")
	require.NoError(t, err)
	for range 2 {
		_, err = tx.ExecContext(ctx, string(sql))
		require.NoError(t, err)
	}
	var exact, policy, family, empty string
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT messages_dispatch_model_config::text,routing_policy::text FROM groups WHERE id=97901`).Scan(&exact, &policy))
	require.JSONEq(t, `{"exact_model_mappings":{"claude-sonnet-4-6":"explicit-target"}}`, exact)
	require.JSONEq(t, `{"enabled":true,"model_mapping":{"public":"upstream"}}`, policy)
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT messages_dispatch_model_config::text FROM groups WHERE id=97902`).Scan(&family))
	require.JSONEq(t, `{}`, family)
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT messages_dispatch_model_config::text FROM groups WHERE id=97903`).Scan(&empty))
	require.JSONEq(t, `{}`, empty)
}
