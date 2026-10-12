package bootstrap

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"

	"github.com/TokenFlux/TokenRouter/ent"
	"github.com/TokenFlux/TokenRouter/internal/config"
	postgresinfra "github.com/TokenFlux/TokenRouter/internal/infra/postgres"
	"github.com/TokenFlux/TokenRouter/migrations"
)

// InitEnt 初始化 Ent ORM 客户端并返回客户端实例和底层的 *sql.DB。
//
// 初始化使用 cfg 中的时区和数据库连接配置，依次设置时区、连接数据库、执行 SQL 迁移并创建 Ent 客户端。
//
// 调用方需要关闭返回的 ent.Client，这会同时关闭底层 driver 和数据库连接。
//
// 底层 *sql.DB 可用于直接执行 SQL，初始化失败时返回错误。
func InitEnt(ctx context.Context, cfg *config.Config) (_ *ent.Client, _ *sql.DB, resultErr error) {
	// 先设置应用时区，后续数据库连接使用同一时区。
	if err := InitTimezone(cfg.Timezone); err != nil {
		return nil, nil, err
	}

	// DSN 将配置的时区传给 PostgreSQL。
	dsn := cfg.Database.DSNWithTimezone(cfg.Timezone)

	// 连接 PostgreSQL，随后将连接交给 Ent 的 SQL 驱动。
	db, err := postgresinfra.Open(dsn, cfg.Server.EnableServerTiming)
	if err != nil {
		return nil, nil, err
	}
	// 取得连接后登记失败回收，成功返回后由 Ent 关闭底层 SQL 连接。
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, db.Close())
		}
	}()
	drv := entsql.OpenDB(dialect.Postgres, db)
	applyDBPoolSettings(drv.DB(), cfg)

	// 按 SQL 迁移文件更新数据库结构，迁移和重试共用十分钟预算。
	migrationCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	if err := postgresinfra.InitializeWithRetry(migrationCtx, func(ctx context.Context) error {
		return postgresinfra.ApplyMigrations(ctx, drv.DB(), migrations.FS)
	}); err != nil {
		return nil, nil, err
	}

	// 创建 Ent 客户端，绑定到已配置的数据库驱动。
	client := ent.NewClient(ent.Driver(drv))

	// 从配置或数据库中取得系统密钥。
	if err := ensureBootstrapSecrets(migrationCtx, client, cfg); err != nil {
		return nil, nil, err
	}

	// 密钥补齐后校验完整配置，JWT 密钥缺失等错误会在启动阶段返回。
	if err := cfg.Validate(); err != nil {
		return nil, nil, fmt.Errorf("validate config after secret bootstrap: %w", err)
	}

	return client, drv.DB(), nil
}

// postgresPoolOptions 将数据库配置转换为连接池选项。
func postgresPoolOptions(cfg *config.Config) postgresinfra.PoolOptions {
	return postgresinfra.PoolOptions{
		MaxOpenConns:           cfg.Database.MaxOpenConns,
		MaxIdleConns:           cfg.Database.MaxIdleConns,
		ConnMaxLifetimeMinutes: cfg.Database.ConnMaxLifetimeMinutes,
		ConnMaxIdleTimeMinutes: cfg.Database.ConnMaxIdleTimeMinutes,
	}
}

// applyDBPoolSettings 设置数据库连接池。
func applyDBPoolSettings(db *sql.DB, cfg *config.Config) {
	postgresinfra.ApplyPoolSettings(db, postgresPoolOptions(cfg))
}

// ApplyMigrations 执行数据库迁移。
func ApplyMigrations(ctx context.Context, db *sql.DB) error {
	return postgresinfra.ApplyMigrations(ctx, db, migrations.FS)
}
