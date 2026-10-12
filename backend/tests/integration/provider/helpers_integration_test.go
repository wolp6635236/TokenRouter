//go:build integration

package provider_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	dbent "github.com/TokenFlux/TokenRouter/ent"
	dbprovider "github.com/TokenFlux/TokenRouter/ent/provider"
	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/egress"
	postgresinfra "github.com/TokenFlux/TokenRouter/internal/infra/postgres"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	providerpostgres "github.com/TokenFlux/TokenRouter/internal/provider/postgres"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

// failConfigurationOutbox 模拟调度 outbox 写入失败。
type failConfigurationOutbox struct{ providerEventsFixture }

// Write 返回用于触发事务回滚的错误。
func (failConfigurationOutbox) Write(ctx context.Context, exec postgresinfra.Executor, _ providerpostgres.ProviderEvent, _, _ *int64, _ any) error {
	// 在事务连接写入 outbox 时返回错误，检查配置更新随事务回滚。
	_, err := exec.ExecContext(ctx, "INSERT INTO test_missing_outbox_fixture DEFAULT VALUES")
	return err
}

// mustCreateGroup 使用 Ent 默认值和传入字段创建测试分组。
func mustCreateGroup(t *testing.T, client *dbent.Client, g *routing.Group) *routing.Group {
	t.Helper()
	ctx := context.Background()

	if g.AllowedProtocols == nil {
		g.AllowedProtocols = capability.DefaultGroupClientProtocols("")
	}
	if g.Status == "" {
		g.Status = billing.StatusActive
	}

	create := client.Group.Create().
		SetName(g.Name).
		SetAllowedProtocols(g.AllowedProtocols).
		SetStatus(g.Status).
		SetRateMultiplier(g.RateMultiplier).
		SetIsExclusive(g.IsExclusive).
		SetForceOpenaiFast(g.ForceOpenAIFast)
	if g.Description != "" {
		create.SetDescription(g.Description)
	}
	if !g.CreatedAt.IsZero() {
		create.SetCreatedAt(g.CreatedAt)
	}
	if !g.UpdatedAt.IsZero() {
		create.SetUpdatedAt(g.UpdatedAt)
	}

	created, err := create.Save(ctx)
	require.NoError(t, err, "create group")

	g.ID = created.ID
	g.CreatedAt = created.CreatedAt
	g.UpdatedAt = created.UpdatedAt
	return g
}

// mustBindProviderToGroup 创建提供商与分组的关系。
func mustBindProviderToGroup(t *testing.T, client *dbent.Client, providerID, groupID int64) {
	t.Helper()
	ctx := context.Background()

	_, err := client.ProviderGroup.Create().
		SetProviderID(providerID).
		SetGroupID(groupID).
		Save(ctx)
	require.NoError(t, err, "create provider_group")
}

// mustCreateProxy 使用给定字段创建代理并回填数据库字段。
func mustCreateProxy(t *testing.T, client *dbent.Client, p *egress.Proxy) *egress.Proxy {
	t.Helper()
	ctx := context.Background()

	if p.Protocol == "" {
		p.Protocol = "http"
	}
	if p.Host == "" {
		p.Host = "127.0.0.1"
	}
	if p.Port == 0 {
		p.Port = 8080
	}
	if p.Status == "" {
		p.Status = providercore.StatusActive
	}

	create := client.Proxy.Create().
		SetName(p.Name).
		SetProtocol(p.Protocol).
		SetHost(p.Host).
		SetPort(p.Port).
		SetStatus(p.Status)
	if p.Username != "" {
		create.SetUsername(p.Username)
	}
	if p.Password != "" {
		create.SetPassword(p.Password)
	}
	if !p.CreatedAt.IsZero() {
		create.SetCreatedAt(p.CreatedAt)
	}
	if !p.UpdatedAt.IsZero() {
		create.SetUpdatedAt(p.UpdatedAt)
	}

	created, err := create.Save(ctx)
	require.NoError(t, err, "create proxy")

	p.ID = created.ID
	p.CreatedAt = created.CreatedAt
	p.UpdatedAt = created.UpdatedAt
	return p
}

// mustCreateProvider 使用给定字段创建提供商并回填数据库字段。
func mustCreateProvider(t *testing.T, client *dbent.Client, a *providercore.Record) *providercore.Record {
	t.Helper()
	ctx := context.Background()

	if a.Platform == "" {
		a.Platform = capability.PlatformAnthropic
	}
	if a.Type == "" {
		a.Type = capability.ProviderTypeOAuth
	}
	if a.Status == "" {
		a.Status = providercore.StatusActive
	}
	if a.Concurrency == 0 {
		a.Concurrency = 3
	}
	if a.Priority == 0 {
		a.Priority = 50
	}
	if !a.Schedulable {
		a.Schedulable = true
	}
	if a.Credentials == nil {
		a.Credentials = map[string]any{}
	}
	if a.Extra == nil {
		a.Extra = map[string]any{}
	}

	create := client.Provider.Create().
		SetName(a.Name).
		SetPlatform(a.Platform).
		SetType(a.Type).
		SetCredentials(a.Credentials).
		SetExtra(a.Extra).
		SetConcurrency(a.Concurrency).
		SetPriority(a.Priority).
		SetStatus(a.Status).
		SetSchedulable(a.Schedulable).
		SetErrorMessage(a.ErrorMessage)

	if a.ProxyID != nil {
		create.SetProxyID(*a.ProxyID)
	}
	if a.LastUsedAt != nil {
		create.SetLastUsedAt(*a.LastUsedAt)
	}
	if a.RateLimitedAt != nil {
		create.SetRateLimitedAt(*a.RateLimitedAt)
	}
	if a.RateLimitResetAt != nil {
		create.SetRateLimitResetAt(*a.RateLimitResetAt)
	}
	if a.OverloadUntil != nil {
		create.SetOverloadUntil(*a.OverloadUntil)
	}
	if a.SessionWindowStart != nil {
		create.SetSessionWindowStart(*a.SessionWindowStart)
	}
	if a.SessionWindowEnd != nil {
		create.SetSessionWindowEnd(*a.SessionWindowEnd)
	}
	if a.SessionWindowStatus != "" {
		create.SetSessionWindowStatus(a.SessionWindowStatus)
	}
	if !a.CreatedAt.IsZero() {
		create.SetCreatedAt(a.CreatedAt)
	}
	if !a.UpdatedAt.IsZero() {
		create.SetUpdatedAt(a.UpdatedAt)
	}
	if a.ParentProviderID != nil {
		create.SetParentProviderID(*a.ParentProviderID)
	}
	if a.QuotaDimension != "" {
		create.SetQuotaDimension(dbprovider.QuotaDimension(a.QuotaDimension))
	}

	created, err := create.Save(ctx)
	require.NoError(t, err, "create provider")

	a.ID = created.ID
	a.CreatedAt = created.CreatedAt
	a.UpdatedAt = created.UpdatedAt
	return a
}

// testEntTx 创建在测试结束时回滚的事务。
// 提交事务的竞争测试自行删除测试行。
func testEntTx(t *testing.T) *dbent.Tx {
	t.Helper()
	tx, err := integrationEntClient.Tx(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	return tx
}

// testEntClient 返回本测试进程共用的 Ent 客户端。
func testEntClient(t *testing.T) *dbent.Client {
	t.Helper()
	return integrationEntClient
}
