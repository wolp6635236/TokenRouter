//go:build integration

package app

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/protocol"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

// NewModelAvailabilityForTest 为集成测试构造模型诊断组件。
var NewModelAvailabilityForTest = provideGatewayModelAvailability

// TestModelAvailabilityUsesPersistentProviderStore 检查诊断读取持久配置，处于瞬时冷却的已配置模型仍可被查到。
func TestModelAvailabilityUsesPersistentProviderStore(t *testing.T) {
	f := newDatabaseFixture(t)
	ctx := t.Context()
	group, err := f.client.Group.Create().SetName("test-diagnostic-group").SetAllowedProtocols([]protocol.ProtocolID{protocol.ProtocolOpenAIResponses}).Save(ctx)
	require.NoError(t, err)
	cooldown := time.Now().Add(time.Hour)
	// 白名单限定可诊断型号，模型映射负责上游名称转换。
	row, err := f.client.Provider.Create().
		SetName("test-diagnostic-provider").
		SetPlatform(capability.PlatformOpenAI).
		SetType(capability.ProviderTypeAPIKey).
		SetCredentials(map[string]any{
			"model_mapping":   map[string]any{"public-known": "public-known"},
			"model_whitelist": []string{"public-known"},
		}).
		SetRateLimitResetAt(cooldown).
		SetOverloadUntil(cooldown).
		SetTempUnschedulableUntil(cooldown).
		Save(ctx)
	require.NoError(t, err)
	_, err = f.client.ProviderGroup.Create().SetProviderID(row.ID).SetGroupID(group.ID).Save(ctx)
	require.NoError(t, err)
	store := NewProviderStoreForTest(f.client, f.db, nil)
	diagnoser := NewModelAvailabilityForTest(store, nil)
	t.Run("configured_cooling_provider", func(t *testing.T) {
		result := diagnoser.Compatible.DiagnoseModelAvailabilityForPlatform(ctx, &group.ID, "public-known", capability.PlatformOpenAI)
		require.True(t, result.HasProvidersInPool)
		require.True(t, result.HasModelSupport)
	})
	t.Run("missing_model_with_existing_pool", func(t *testing.T) {
		result := diagnoser.Resolved.DiagnoseModelAvailabilityForPlatform(ctx, &group.ID, "not-configured", capability.PlatformOpenAI)
		require.True(t, result.HasProvidersInPool)
		require.False(t, result.HasModelSupport)
	})
	t.Run("ungrouped_scope", func(t *testing.T) {
		result := diagnoser.Compatible.DiagnoseModelAvailabilityForPlatform(ctx, nil, "public-known", capability.PlatformOpenAI)
		require.False(t, result.HasProvidersInPool)
		require.False(t, result.HasModelSupport)
	})
	t.Run("requires_explicit_group", func(t *testing.T) {
		result := diagnoser.Compatible.DiagnoseModelAvailabilityForPlatform(ctx, nil, "public-known", capability.PlatformOpenAI)
		require.False(t, result.HasProvidersInPool)
		require.False(t, result.HasModelSupport)
		result = diagnoser.Compatible.DiagnoseModelAvailabilityForPlatform(ctx, &group.ID, "public-known", capability.PlatformOpenAI)
		require.True(t, result.HasProvidersInPool)
		require.True(t, result.HasModelSupport)
	})
	// 管理禁用的提供商从诊断候选中排除。
	require.NoError(t, f.client.Provider.UpdateOneID(row.ID).SetStatus(provider.StatusDisabled).Exec(ctx))
	result := diagnoser.Compatible.DiagnoseModelAvailabilityForPlatform(ctx, &group.ID, "public-known", capability.PlatformOpenAI)
	require.False(t, result.HasProvidersInPool)
}
