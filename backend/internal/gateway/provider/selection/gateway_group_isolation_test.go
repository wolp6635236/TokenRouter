package selection

import (
	"context"
	"testing"
	time "time"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/config"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Part 1: isProviderInGroup 单元测试
// ============================================================================

func TestIsProviderInGroup(t *testing.T) {
	svc := newGenericSelectionForTest(GenericDependencies{Reads: Reads{}, Shared: Shared{}},

		nil)

	groupID100 := int64(100)
	groupID200 := int64(200)

	tests := []struct {
		name     string
		provider *gatewayprovider.ExecutionProvider
		groupID  *int64
		expected bool
	}{
		// groupID == nil（无分组 API Key）
		{
			"nil_groupID_ungrouped_provider_nil_groups",
			&gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, ProviderGroups: nil}},
			nil, false,
		},
		{
			"nil_groupID_ungrouped_provider_empty_slice",
			&gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, ProviderGroups: []providercore.GroupMembership{}}},
			nil, false,
		},
		{
			"nil_groupID_grouped_provider_single",
			&gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 3, ProviderGroups: []providercore.GroupMembership{{GroupID: 100}}}},
			nil, false,
		},
		{
			"nil_groupID_grouped_provider_multiple",
			&gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 4, ProviderGroups: []providercore.GroupMembership{{GroupID: 100}, {GroupID: 200}}}},
			nil, false,
		},
		// groupID != nil（有分组 API Key）
		{
			"with_groupID_provider_in_group",
			&gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 5, ProviderGroups: []providercore.GroupMembership{{GroupID: 100}}}},
			&groupID100, true,
		},
		{
			"with_groupID_provider_not_in_group",
			&gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 6, ProviderGroups: []providercore.GroupMembership{{GroupID: 200}}}},
			&groupID100, false,
		},
		{
			"with_groupID_ungrouped_provider",
			&gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 7, ProviderGroups: nil}},
			&groupID100, false,
		},
		{
			"with_groupID_multi_group_provider_match_one",
			&gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 8, ProviderGroups: []providercore.GroupMembership{{GroupID: 100}, {GroupID: 200}}}},
			&groupID200, true,
		},
		{
			"with_groupID_multi_group_provider_no_match",
			&gatewayprovider.ExecutionProvider{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 9, ProviderGroups: []providercore.GroupMembership{{GroupID: 300}, {GroupID: 400}}}},
			&groupID100, false,
		},
		// 提供商或分组 ID 为 nil 的场景。
		{
			"nil_provider_nil_groupID",
			nil,
			nil, false,
		},
		{
			"nil_provider_with_groupID",
			nil,
			&groupID100, false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := svc.isProviderInGroup(tt.provider, tt.groupID)
			require.Equal(t, tt.expected, got, "isProviderInGroup 结果不符预期")
		})
	}
}

// ============================================================================
// Part 2: 分组隔离端到端调度测试
// ============================================================================

// groupAwareMockProviderRepo 嵌入 mockProviderRepoForPlatform，覆写分组隔离相关方法。
// allProviders 保存所有提供商，分组查询按 ProviderGroups 字段过滤。
type groupAwareMockProviderRepo struct {
	*mockProviderRepoForPlatform
	allProviders []gatewayprovider.

		// ListSchedulableUngroupedByPlatform 仅返回未分组提供商（ProviderGroups 为空）
		ExecutionProvider
}

func (m *groupAwareMockProviderRepo) ListSchedulableUngroupedByPlatform(ctx context.Context, platform string) ([]gatewayprovider.ExecutionProvider, error) {
	var result []gatewayprovider.ExecutionProvider
	for _, acc := range m.allProviders {
		if acc.Record.Platform == platform && acc.View().IsSchedulable() && len(acc.Record.ProviderGroups) == 0 {
			result = append(result, acc)
		}
	}
	return result, nil
}

// ListSchedulableUngroupedByPlatforms 仅返回未分组提供商（多平台版本）
func (m *groupAwareMockProviderRepo) ListSchedulableUngroupedByPlatforms(ctx context.Context, platforms []string) ([]gatewayprovider.ExecutionProvider, error) {
	platformSet := make(map[string]bool, len(platforms))
	for _, p := range platforms {
		platformSet[p] = true
	}
	var result []gatewayprovider.ExecutionProvider
	for _, acc := range m.allProviders {
		if platformSet[acc.Record.Platform] && acc.View().IsSchedulable() && len(acc.Record.ProviderGroups) == 0 {
			result = append(result, acc)
		}
	}
	return result, nil
}

// ListSchedulableByGroupIDAndPlatform 返回属于指定分组的提供商
func (m *groupAwareMockProviderRepo) ListSchedulableByGroupIDAndPlatform(ctx context.Context, groupID int64, platform string) ([]gatewayprovider.ExecutionProvider, error) {
	var result []gatewayprovider.ExecutionProvider
	for _, acc := range m.allProviders {
		if acc.Record.Platform == platform && acc.View().IsSchedulable() && providerBelongsToGroup(acc, groupID) {
			result = append(result, acc)
		}
	}
	return result, nil
}

// ListSchedulableByGroupIDAndPlatforms 返回属于指定分组的提供商（多平台版本）
func (m *groupAwareMockProviderRepo) ListSchedulableByGroupIDAndPlatforms(ctx context.Context, groupID int64, platforms []string) ([]gatewayprovider.ExecutionProvider, error) {
	platformSet := make(map[string]bool, len(platforms))
	for _, p := range platforms {
		platformSet[p] = true
	}
	var result []gatewayprovider.ExecutionProvider
	for _, acc := range m.allProviders {
		if platformSet[acc.Record.Platform] && acc.View().IsSchedulable() && providerBelongsToGroup(acc, groupID) {
			result = append(result, acc)
		}
	}
	return result, nil
}

// providerBelongsToGroup 检查提供商是否属于指定分组
func providerBelongsToGroup(acc gatewayprovider.ExecutionProvider, groupID int64) bool {
	for _, ag := range acc.Record.ProviderGroups {
		if ag.GroupID == groupID {
			return true
		}
	}
	return false
}

// Verify interface implementation

// newGroupAwareMockRepo 创建分组感知的 mock repo
func newGroupAwareMockRepo(providers []gatewayprovider.ExecutionProvider) *groupAwareMockProviderRepo {
	byID := make(map[int64]*gatewayprovider.ExecutionProvider, len(providers))
	for i := range providers {
		byID[providers[i].Record.ID] = &providers[i]
	}
	return &groupAwareMockProviderRepo{
		mockProviderRepoForPlatform: &mockProviderRepoForPlatform{
			providers:     providers,
			providersByID: byID,
		},
		allProviders: providers,
	}
}

func TestGroupIsolation_UngroupedKey_ShouldNotScheduleGroupedProviders(t *testing.T) {
	// 场景：无分组 API Key（groupID=nil），池中只有已分组提供商 → 应返回错误
	ctx := context.Background()

	providers := []gatewayprovider.ExecutionProvider{
		{Record: providercore.Record{
			Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformOpenAI, Priority: 1, Status: billing.StatusActive, Schedulable: true,
			ProviderGroups: []providercore.GroupMembership{{GroupID: 100}},
		}},
		{Record: providercore.Record{
			Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformOpenAI, Priority: 2, Status: billing.StatusActive, Schedulable: true,
			ProviderGroups: []providercore.GroupMembership{{GroupID: 200}},
		}},
	}
	repo := newGroupAwareMockRepo(providers)
	cache := &mockGatewayCacheForPlatform{}

	svc := newGenericSelectionForTest(GenericDependencies{Reads: Reads{Providers: repo}, Shared: Shared{Cache: cache}}, testConfig())

	acc, err := svc.selectProviderForModelWithPlatform(ctx, nil, "", "", nil, capability.PlatformOpenAI)
	require.Error(t, err, "无分组 Key 不应调度到已分组提供商")
	require.Nil(t, acc)
}

func TestGroupIsolation_GroupedKey_ShouldNotScheduleUngroupedProviders(t *testing.T) {
	// 场景：有分组 API Key（groupID=100），池中只有未分组提供商 → 应返回错误
	ctx := context.Background()
	groupID := int64(100)

	providers := []gatewayprovider.ExecutionProvider{
		{Record: providercore.Record{
			Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformOpenAI, Priority: 1, Status: billing.StatusActive, Schedulable: true,
			ProviderGroups: nil,
		}},
		{Record: providercore.Record{
			Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformOpenAI, Priority: 2, Status: billing.StatusActive, Schedulable: true,
			ProviderGroups: []providercore.GroupMembership{},
		}},
	}
	repo := newGroupAwareMockRepo(providers)
	cache := &mockGatewayCacheForPlatform{}

	svc := newGenericSelectionForTest(GenericDependencies{Reads: Reads{Providers: repo}, Shared: Shared{Cache: cache}}, testConfig())

	acc, err := svc.selectProviderForModelWithPlatform(ctx, &groupID, "", "", nil, capability.PlatformOpenAI)
	require.Error(t, err, "有分组 Key 不应调度到未分组提供商")
	require.Nil(t, acc)
}

func TestGroupIsolation_UngroupedKey_RejectsUngroupedProviders(t *testing.T) {
	// 场景：无分组 API Key（groupID=nil），池中有未分组和已分组提供商 → 应只选中未分组的
	ctx := context.Background()

	providers := []gatewayprovider.ExecutionProvider{
		{Record: providercore.Record{
			Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformOpenAI, Priority: 1, Status: billing.StatusActive, Schedulable: true,
			ProviderGroups: []providercore.GroupMembership{{GroupID: 100}},
		}}, // 已分组，不应被选中
		{Record: providercore.Record{
			Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformOpenAI, Priority: 2, Status: billing.StatusActive, Schedulable: true,
			ProviderGroups: nil,
		}}, // 未分组，应被选中
		{Record: providercore.Record{
			Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 3, Platform: capability.PlatformOpenAI, Priority: 3, Status: billing.StatusActive, Schedulable: true,
			ProviderGroups: []providercore.GroupMembership{{GroupID: 200}},
		}}, // 已分组，不应被选中
	}
	repo := newGroupAwareMockRepo(providers)
	cache := &mockGatewayCacheForPlatform{}

	svc := newGenericSelectionForTest(GenericDependencies{Reads: Reads{Providers: repo}, Shared: Shared{Cache: cache}}, testConfig())

	acc, err := svc.selectProviderForModelWithPlatform(ctx, nil, "", "", nil, capability.PlatformOpenAI)
	require.Error(t, err, "请求必须明确绑定分组")
	require.Nil(t, acc)
}

func TestGroupIsolation_GroupedKey_ShouldOnlyScheduleMatchingGroupProviders(t *testing.T) {
	// 场景：有分组 API Key（groupID=100），池中有未分组和多个分组提供商 → 应只选中分组 100 内的
	ctx := context.Background()
	groupID := int64(100)

	providers := []gatewayprovider.ExecutionProvider{
		{Record: providercore.Record{
			Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformOpenAI, Priority: 1, Status: billing.StatusActive, Schedulable: true,
			ProviderGroups: nil,
		}}, // 未分组，不应被选中
		{Record: providercore.Record{
			Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformOpenAI, Priority: 2, Status: billing.StatusActive, Schedulable: true,
			ProviderGroups: []providercore.GroupMembership{{GroupID: 200}},
		}}, // 属于分组 200，不应被选中
		{Record: providercore.Record{
			Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 3, Platform: capability.PlatformOpenAI, Priority: 3, Status: billing.StatusActive, Schedulable: true,
			ProviderGroups: []providercore.GroupMembership{{GroupID: 100}},
		}}, // 属于分组 100，应被选中
	}
	repo := newGroupAwareMockRepo(providers)
	cache := &mockGatewayCacheForPlatform{}

	svc := newGenericSelectionForTest(GenericDependencies{Reads: Reads{Providers: repo}, Shared: Shared{Cache: cache}}, testConfig())

	acc, err := svc.selectProviderForModelWithPlatform(ctx, &groupID, "", "", nil, capability.PlatformOpenAI)
	require.NoError(t, err, "应成功调度分组内提供商")
	require.NotNil(t, acc)
	require.Equal(t, int64(3), acc.Record.ID, "应选中分组 100 内的提供商 ID=3")
}

// ============================================================================
// 测试请求分组对候选范围的限制。
// ============================================================================

func TestGroupIsolation_RequiresExplicitGroup(t *testing.T) {
	// 缺少明确分组时，不允许读取全局提供商池。
	// 测试非 useMixed 路径（platform=openai，不会触发 mixed 调度逻辑）。
	ctx := context.Background()

	// 混合未分组和已分组提供商，调用仍须指定分组。
	providers := []gatewayprovider.ExecutionProvider{
		{Record: providercore.Record{
			Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformOpenAI, Priority: 2, Status: billing.StatusActive, Schedulable: true,
			ProviderGroups: []providercore.GroupMembership{{GroupID: 100}},
		}}, // 已分组
		{Record: providercore.Record{
			Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformOpenAI, Priority: 1, Status: billing.StatusActive, Schedulable: true,
			ProviderGroups: nil,
		}}, // 未分组
	}

	// 使用基础 mock（ListSchedulableByPlatform 返回所有匹配平台的提供商，不做分组过滤）
	byID := make(map[int64]*gatewayprovider.ExecutionProvider, len(providers))
	for i := range providers {
		byID[providers[i].Record.ID] = &providers[i]
	}
	repo := &mockProviderRepoForPlatform{
		providers:     providers,
		providersByID: byID,
	}
	cache := &mockGatewayCacheForPlatform{}

	svc := newGenericSelectionForTest(GenericDependencies{Reads: Reads{Providers: repo}, Shared: Shared{Cache: cache}}, &config.Config{})

	// groupID=nil 时直接拒绝，不读取任何提供商池。
	acc, err := svc.selectProviderForModelWithPlatform(ctx, nil, "", "", nil, capability.PlatformOpenAI)
	require.Error(t, err, "请求必须明确绑定分组")
	require.Nil(t, acc)
}

func TestGroupIsolation_RejectsImplicitGroupedProvider(t *testing.T) {
	// groupID=nil 时，即使有已分组提供商也不允许调度。
	ctx := context.Background()

	// 调用方需要指定目标分组，提供商的分组关系用于筛选候选。
	providers := []gatewayprovider.ExecutionProvider{
		{Record: providercore.Record{
			Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformOpenAI, Priority: 1, Status: billing.StatusActive, Schedulable: true,
			ProviderGroups: []providercore.GroupMembership{{GroupID: 100}},
		}},
	}

	byID := make(map[int64]*gatewayprovider.ExecutionProvider, len(providers))
	for i := range providers {
		byID[providers[i].Record.ID] = &providers[i]
	}
	repo := &mockProviderRepoForPlatform{
		providers:     providers,
		providersByID: byID,
	}
	cache := &mockGatewayCacheForPlatform{}

	svc := newGenericSelectionForTest(GenericDependencies{Reads: Reads{Providers: repo}, Shared: Shared{Cache: cache}}, &config.Config{})

	acc, err := svc.selectProviderForModelWithPlatform(ctx, nil, "", "", nil, capability.PlatformOpenAI)
	require.Error(t, err, "请求必须明确绑定分组")
	require.Nil(t, acc)
}
