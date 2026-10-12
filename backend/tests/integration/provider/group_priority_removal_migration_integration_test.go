//go:build integration

package provider_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	dbmigrations "github.com/TokenFlux/TokenRouter/migrations"
)

// TestMigration240RemovesProviderGroupPriority 验证关联优先级迁移可重复执行且清理完整。
func TestMigration240RemovesProviderGroupPriority(t *testing.T) {
	ctx := context.Background()
	migrationSQL, err := dbmigrations.FS.ReadFile("240_remove_account_group_priority.sql")
	require.NoError(t, err)
	tx := testTx(t)

	// 测试数据库已应用全部迁移，在事务内恢复迁移前的表结构后执行升级。
	_, err = tx.ExecContext(ctx, `
ALTER TABLE provider_groups RENAME TO account_groups;
ALTER TABLE account_groups RENAME COLUMN provider_id TO account_id;
ALTER TABLE account_groups
    ADD COLUMN priority INTEGER NOT NULL DEFAULT 50;
CREATE INDEX idx_account_groups_priority
    ON account_groups (priority);
CREATE INDEX idx_account_groups_group_priority_account
    ON account_groups (group_id, priority, account_id);
CREATE INDEX idx_account_groups_account_priority_group
    ON account_groups (account_id, priority, group_id);
`)
	require.NoError(t, err)

	_, err = tx.ExecContext(ctx, string(migrationSQL))
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(migrationSQL))
	require.NoError(t, err)

	var columnCount int
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM information_schema.columns
WHERE table_schema = current_schema()
  AND table_name = 'account_groups'
  AND column_name = 'priority'
`).Scan(&columnCount))
	require.Zero(t, columnCount)

	var indexCount int
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM pg_indexes
WHERE schemaname = current_schema()
  AND indexname IN (
      'idx_account_groups_priority',
      'idx_account_groups_group_priority_account',
      'idx_account_groups_account_priority_group'
  )
`).Scan(&indexCount))
	require.Zero(t, indexCount)

	// 历史迁移验证完成后回到当前名称，再验证实际提供商仓储的排序。
	_, err = tx.ExecContext(ctx, `ALTER TABLE account_groups RENAME COLUMN account_id TO provider_id; ALTER TABLE account_groups RENAME TO provider_groups`)
	require.NoError(t, err)

	var groupID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO groups (name)
VALUES ('migration-240-capacity-order')
RETURNING id
`).Scan(&groupID))

	// 先插入低优先级提供商，使 ID 顺序与优先级顺序相反；同优先级提供商再按 ID 稳定排序。
	insertProvider := func(name string, priority int) int64 {
		t.Helper()
		var providerID int64
		require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO providers (name, platform, type, priority)
VALUES ($1, 'openai', 'apikey', $2)
RETURNING id
`, name, priority).Scan(&providerID))
		return providerID
	}
	lowPriorityID := insertProvider("migration-240-low-priority", 20)
	highPriorityFirstID := insertProvider("migration-240-high-priority-first", 10)
	highPrioritySecondID := insertProvider("migration-240-high-priority-second", 10)

	// 打乱关联写入顺序，检查容量查询仍按提供商优先级排序。
	for _, providerID := range []int64{lowPriorityID, highPrioritySecondID, highPriorityFirstID} {
		_, err = tx.ExecContext(ctx, `
INSERT INTO provider_groups (provider_id, group_id)
VALUES ($1, $2)
`, providerID, groupID)
		require.NoError(t, err)
	}

	repo := newProviderStoreContract(nil, tx, nil)
	capacityRows, err := repo.ListSchedulableCapacityByGroupIDs(ctx, []int64{groupID})
	require.NoError(t, err)
	require.Len(t, capacityRows, 3)
	require.Equal(t, []int64{highPriorityFirstID, highPrioritySecondID, lowPriorityID}, []int64{
		capacityRows[0].ProviderID,
		capacityRows[1].ProviderID,
		capacityRows[2].ProviderID,
	})
}

// testTx 为迁移测试创建事务，测试结束时回滚结构变更。
func testTx(t *testing.T) *sql.Tx {
	t.Helper()
	tx, err := integrationDB.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tx.Rollback()) })
	return tx
}
