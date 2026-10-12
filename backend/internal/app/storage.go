package app

import (
	"context"
	"database/sql"
	"errors"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/redis/go-redis/v9"

	"github.com/TokenFlux/TokenRouter/ent"
	"github.com/TokenFlux/TokenRouter/internal/app/bootstrap"
	"github.com/TokenFlux/TokenRouter/internal/app/lifecycle"
	"github.com/TokenFlux/TokenRouter/internal/config"
	"github.com/TokenFlux/TokenRouter/internal/idempotency"
	idempotencypostgres "github.com/TokenFlux/TokenRouter/internal/idempotency/postgres"
	"github.com/TokenFlux/TokenRouter/internal/infra/postgres"
	redisinfra "github.com/TokenFlux/TokenRouter/internal/infra/redis"
	"github.com/TokenFlux/TokenRouter/internal/pkg/timezone"
	"github.com/TokenFlux/TokenRouter/internal/settings"
	settingspostgres "github.com/TokenFlux/TokenRouter/internal/settings/postgres"
)

// provideCalendar 在 bootstrap 完成时区初始化后固定日期计算位置；依赖关系保证装配顺序。
func provideCalendar(_ *ent.Client) timezone.Calendar {
	return timezone.NewCalendar(time.Local)
}

// databaseAdvisoryLease 绑定数据库连接来源，调用方决定锁身份和失败时的处理方式。
func databaseAdvisoryLease(db *sql.DB) func(context.Context, string) (func(), bool) {
	if db == nil {
		return nil
	}
	return func(ctx context.Context, key string) (func(), bool) {
		return postgres.TryAcquireDBAdvisoryLock(ctx, db, postgres.HashAdvisoryLockID(key))
	}
}

func provideEnt(ctx context.Context, cfg *config.Config, manager *lifecycle.Manager) (*ent.Client, error) {
	client, _, err := bootstrap.InitEnt(ctx, cfg)
	if err != nil {
		return nil, err
	}
	manager.Register(lifecycle.Hook{Name: "Ent", StartOrder: -100, StopOrder: 910, Stop: func(context.Context) error { return client.Close() }})
	return client, nil
}

func provideRedis(ctx context.Context, cfg *config.Config, manager *lifecycle.Manager) (*redis.Client, error) {
	client := bootstrap.InitRedis(cfg)
	// 运行状态迁移完成后再构造后台任务和开放流量，限额从迁移后的键读取。
	migrationCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	if err := redisinfra.MigrateProviderNames(migrationCtx, client); err != nil {
		_ = client.Close()
		return nil, err
	}
	manager.Register(lifecycle.Hook{Name: "Redis", StartOrder: -90, StopOrder: 900, Stop: func(context.Context) error { return client.Close() }})
	return client, nil
}

// provideSettingsStore 直接构造唯一设置实例，所有接口共享版本、通知与更新协调器。
// @project-doc docs/interfaces/configuration.md#runtime_settings
func provideSettingsStore(client *ent.Client) *settings.Store {
	return settings.New(settingspostgres.NewSettingRepository(client))
}

// provideIdempotencyRepository 使用应用共享的 SQL 连接池构造幂等存储。
func provideIdempotencyRepository(db *sql.DB) idempotency.IdempotencyRepository {
	return idempotencypostgres.NewIdempotencyRepository(db)
}

// provideSQLDB 返回 Ent 的连接池，连接池随 Ent 关闭。
func provideSQLDB(client *ent.Client) (*sql.DB, error) {
	if client == nil {
		return nil, errors.New("nil ent client")
	}
	driver, ok := client.Driver().(*entsql.Driver)
	if !ok {
		return nil, errors.New("ent driver does not expose *sql.DB")
	}
	return driver.DB(), nil
}
