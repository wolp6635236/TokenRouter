package selection

import (
	"context"
	"errors"
	"testing"
	"time"

	routingtestkit "github.com/TokenFlux/TokenRouter/internal/routing/testkit"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	logging "github.com/TokenFlux/TokenRouter/internal/infra/telemetry/logging"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/config"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/scheduler"
	"github.com/stretchr/testify/require"
)

// testConfig 返回一个用于测试的默认配置
func testConfig() *config.Config {
	return &config.Config{}
}

// mockProviderRepoForPlatform 单平台测试用的 mock
type mockProviderRepoForPlatform struct {
	providers        []gatewayprovider.ExecutionProvider
	providersByID    map[int64]*gatewayprovider.ExecutionProvider
	listPlatformFunc func(ctx context.Context, platform string) ([]gatewayprovider.ExecutionProvider, error)
	getByIDCalls     int
}

func (m *mockProviderRepoForPlatform) GetByID(ctx context.Context, id int64) (*gatewayprovider.ExecutionProvider, error) {
	m.getByIDCalls++
	if acc, ok := m.providersByID[id]; ok {
		prepareSelectionFixtureProvider(ctx, acc, nil)
		return acc, nil
	}
	return nil, errors.New("provider not found")
}

func (m *mockProviderRepoForPlatform) ListSchedulableByPlatform(ctx context.Context, platform string) ([]gatewayprovider.ExecutionProvider, error) {
	if m.listPlatformFunc != nil {
		return m.listPlatformFunc(ctx, platform)
	}
	var result []gatewayprovider.ExecutionProvider
	for _, acc := range m.providers {
		if acc.Record.Platform == platform && acc.View().IsSchedulable() {
			result = append(result, acc)
		}
	}
	return result, nil
}

func (m *mockProviderRepoForPlatform) ListSchedulableByGroupIDAndPlatform(ctx context.Context, groupID int64, platform string) ([]gatewayprovider.ExecutionProvider, error) {
	for i := range m.providers {
		prepareSelectionFixtureProvider(ctx, &m.providers[i], &groupID)
	}
	return m.ListSchedulableByPlatform(ctx, platform)
}

// Stub methods to implement ProviderRepository interface

func (m *mockProviderRepoForPlatform) ListSchedulableByGroupID(ctx context.Context, groupID int64) ([]gatewayprovider.ExecutionProvider, error) {
	return nil, nil
}

func (m *mockProviderRepoForPlatform) ListSchedulableByPlatforms(ctx context.Context, platforms []string) ([]gatewayprovider.ExecutionProvider, error) {
	var result []gatewayprovider.ExecutionProvider
	platformSet := make(map[string]bool)
	for _, p := range platforms {
		platformSet[p] = true
	}
	for _, acc := range m.providers {
		if platformSet[acc.Record.Platform] && acc.View().IsSchedulable() {
			result = append(result, acc)
		}
	}
	return result, nil
}

func (m *mockProviderRepoForPlatform) ListSchedulableByGroupIDAndPlatforms(ctx context.Context, groupID int64, platforms []string) ([]gatewayprovider.ExecutionProvider, error) {
	for i := range m.providers {
		prepareSelectionFixtureProvider(ctx, &m.providers[i], &groupID)
	}
	return m.ListSchedulableByPlatforms(ctx, platforms)
}

func (m *mockProviderRepoForPlatform) ListSchedulableUngroupedByPlatform(ctx context.Context, platform string) ([]gatewayprovider.ExecutionProvider, error) {
	return m.ListSchedulableByPlatform(ctx, platform)
}

func (m *mockProviderRepoForPlatform) ListSchedulableUngroupedByPlatforms(ctx context.Context, platforms []string) ([]gatewayprovider.ExecutionProvider, error) {
	return m.ListSchedulableByPlatforms(ctx, platforms)
}

// Verify interface implementation

// mockGatewayCacheForPlatform 单平台测试用的 cache mock
type mockGatewayCacheForPlatform struct {
	sessionBindings map[string]int64
	deletedSessions map[string]int
}

func (m *mockGatewayCacheForPlatform) GetSessionProviderID(ctx context.Context, groupID int64, sessionHash string) (int64, error) {
	if id, ok := m.sessionBindings[sessionHash]; ok {
		return id, nil
	}
	return 0, errors.New("not found")
}

func (m *mockGatewayCacheForPlatform) SetSessionProviderID(ctx context.Context, groupID int64, sessionHash string, providerID int64, ttl time.Duration) error {
	if m.sessionBindings == nil {
		m.sessionBindings = make(map[string]int64)
	}
	m.sessionBindings[sessionHash] = providerID
	return nil
}

func (m *mockGatewayCacheForPlatform) RefreshSessionTTL(ctx context.Context, groupID int64, sessionHash string, ttl time.Duration) error {
	return nil
}

func (m *mockGatewayCacheForPlatform) DeleteSessionProviderID(ctx context.Context, groupID int64, sessionHash string) error {
	if m.sessionBindings == nil {
		return nil
	}
	if m.deletedSessions == nil {
		m.deletedSessions = make(map[string]int)
	}
	m.deletedSessions[sessionHash]++
	delete(m.sessionBindings, sessionHash)
	return nil
}

type mockGroupRepoForGateway struct {
	groups           map[int64]*routing.Group
	getByIDCalls     int
	getByIDLiteCalls int
}

func (m *mockGroupRepoForGateway) GetByID(ctx context.Context, id int64) (*routing.Group, error) {
	m.getByIDCalls++
	if g, ok := m.groups[id]; ok {
		return g, nil
	}
	return nil, routing.ErrGroupNotFound
}

func (m *mockGroupRepoForGateway) GetByIDLite(ctx context.Context, id int64) (*routing.Group, error) {
	m.getByIDLiteCalls++
	if g, ok := m.groups[id]; ok {
		return g, nil
	}
	return nil, routing.ErrGroupNotFound
}

func ptr[T any](v T) *T {
	return &v
}

// TestGatewayService_SelectProviderForModelWithPlatform_Anthropic 测试 anthropic 单平台选择
func TestGatewayService_SelectProviderForModelWithPlatform_Anthropic(t *testing.T) {
	ctx := context.Background()

	repo := &mockProviderRepoForPlatform{
		providers: []gatewayprovider.ExecutionProvider{
			{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true}},
			{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformAnthropic, Priority: 2, Status: billing.StatusActive, Schedulable: true}},
			{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 3, Platform: capability.PlatformAntigravity, Priority: 1, Status: billing.StatusActive, Schedulable: true}}, // 应被隔离
		},
		providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
	}
	for i := range repo.providers {
		repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
	}

	cache := &mockGatewayCacheForPlatform{}

	svc := newGenericSelectionForTest(GenericDependencies{Reads: Reads{Providers: repo}, Shared: Shared{Cache: cache}}, testConfig())

	acc, err := svc.selectProviderForModelWithPlatform(ctx, selectionFixtureGroupID(ctx), "", "claude-3-5-sonnet-20241022", nil, capability.PlatformAnthropic)
	require.NoError(t, err)
	require.NotNil(t, acc)
	require.Equal(t, int64(1), acc.Record.ID, "应选择优先级最高的 anthropic 提供商")
	require.Equal(t, capability.PlatformAnthropic, acc.Record.Platform, "应只返回 anthropic 平台提供商")
}

// TestGatewayService_SelectProviderForModelWithPlatform_Antigravity 测试 antigravity 单平台选择
func TestGatewayService_SelectProviderForModelWithPlatform_Antigravity(t *testing.T) {
	ctx := context.Background()

	repo := &mockProviderRepoForPlatform{
		providers: []gatewayprovider.ExecutionProvider{
			{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true}}, // 应被隔离
			{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformAntigravity, Priority: 1, Status: billing.StatusActive, Schedulable: true}},
		},
		providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
	}
	for i := range repo.providers {
		repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
	}

	cache := &mockGatewayCacheForPlatform{}

	svc := newGenericSelectionForTest(GenericDependencies{Reads: Reads{Providers: repo}, Shared: Shared{Cache: cache}}, testConfig())

	acc, err := svc.selectProviderForModelWithPlatform(ctx, selectionFixtureGroupID(ctx), "", "claude-sonnet-4-5", nil, capability.PlatformAntigravity)
	require.NoError(t, err)
	require.NotNil(t, acc)
	require.Equal(t, int64(2), acc.Record.ID)
	require.Equal(t, capability.PlatformAntigravity, acc.Record.Platform, "应只返回 antigravity 平台提供商")
}

// TestGatewayService_SelectProviderForModelWithPlatform_PriorityAndLastUsed 测试优先级和最后使用时间
func TestGatewayService_SelectProviderForModelWithPlatform_PriorityAndLastUsed(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	repo := &mockProviderRepoForPlatform{
		providers: []gatewayprovider.ExecutionProvider{
			{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true, LastUsedAt: ptr(now.Add(-1 * time.Hour))}},
			{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true, LastUsedAt: ptr(now.Add(-2 * time.Hour))}},
		},
		providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
	}
	for i := range repo.providers {
		repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
	}

	cache := &mockGatewayCacheForPlatform{}

	svc := newGenericSelectionForTest(GenericDependencies{Reads: Reads{Providers: repo}, Shared: Shared{Cache: cache}}, testConfig())

	acc, err := svc.selectProviderForModelWithPlatform(ctx, selectionFixtureGroupID(ctx), "", "claude-3-5-sonnet-20241022", nil, capability.PlatformAnthropic)
	require.NoError(t, err)
	require.NotNil(t, acc)
	require.Equal(t, int64(2), acc.Record.ID, "同优先级应选择最久未用的提供商")
}

func TestGatewayService_SelectProviderForModelWithPlatform_GeminiOAuthPreference(t *testing.T) {
	ctx := context.Background()

	repo := &mockProviderRepoForPlatform{
		providers: []gatewayprovider.ExecutionProvider{
			{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformGemini, Priority: 1, Status: billing.StatusActive, Schedulable: true, Type: capability.ProviderTypeAPIKey}},
			{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformGemini, Priority: 1, Status: billing.StatusActive, Schedulable: true, Type: capability.ProviderTypeOAuth}},
		},
		providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
	}
	for i := range repo.providers {
		repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
	}

	cache := &mockGatewayCacheForPlatform{}

	svc := newGenericSelectionForTest(GenericDependencies{Reads: Reads{Providers: repo}, Shared: Shared{Cache: cache}}, testConfig())

	acc, err := svc.selectProviderForModelWithPlatform(ctx, selectionFixtureGroupID(ctx), "", "gemini-2.5-pro", nil, capability.PlatformGemini)
	require.NoError(t, err)
	require.NotNil(t, acc)
	require.Equal(t, int64(2), acc.Record.ID, "同优先级且未使用时应优先选择OAuth提供商")
}

// TestGatewayService_SelectProviderForModelWithPlatform_NoAvailableProviders 测试无可用提供商
func TestGatewayService_SelectProviderForModelWithPlatform_NoAvailableProviders(t *testing.T) {
	ctx := context.Background()

	repo := &mockProviderRepoForPlatform{
		providers:     []gatewayprovider.ExecutionProvider{},
		providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
	}

	cache := &mockGatewayCacheForPlatform{}

	svc := newGenericSelectionForTest(GenericDependencies{Reads: Reads{Providers: repo}, Shared: Shared{Cache: cache}}, testConfig())

	acc, err := svc.selectProviderForModelWithPlatform(ctx, selectionFixtureGroupID(ctx), "", "claude-3-5-sonnet-20241022", nil, capability.PlatformAnthropic)
	require.Error(t, err)
	require.Nil(t, acc)
	require.ErrorIs(t, err, scheduler.ErrNoAvailableProviders)
}

// TestGatewayService_SelectProviderForModelWithPlatform_AllExcluded 测试所有提供商被排除
func TestGatewayService_SelectProviderForModelWithPlatform_AllExcluded(t *testing.T) {
	ctx := context.Background()

	repo := &mockProviderRepoForPlatform{
		providers: []gatewayprovider.ExecutionProvider{
			{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true}},
			{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true}},
		},
		providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
	}
	for i := range repo.providers {
		repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
	}

	cache := &mockGatewayCacheForPlatform{}

	svc := newGenericSelectionForTest(GenericDependencies{Reads: Reads{Providers: repo}, Shared: Shared{Cache: cache}}, testConfig())

	excludedIDs := map[int64]struct{}{1: {}, 2: {}}
	acc, err := svc.selectProviderForModelWithPlatform(ctx, selectionFixtureGroupID(ctx), "", "claude-3-5-sonnet-20241022", excludedIDs, capability.PlatformAnthropic)
	require.Error(t, err)
	require.Nil(t, acc)
}

// TestGatewayService_SelectProviderForModelWithPlatform_Schedulability 测试提供商可调度性检查
func TestGatewayService_SelectProviderForModelWithPlatform_Schedulability(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	tests := []struct {
		name       string
		providers  []gatewayprovider.ExecutionProvider
		expectedID int64
	}{
		{
			name: "过载提供商被跳过",
			providers: []gatewayprovider.ExecutionProvider{
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true, OverloadUntil: ptr(now.Add(1 * time.Hour))}},
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformAnthropic, Priority: 2, Status: billing.StatusActive, Schedulable: true}},
			},
			expectedID: 2,
		},
		{
			name: "限流提供商被跳过",
			providers: []gatewayprovider.ExecutionProvider{
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true, RateLimitResetAt: ptr(now.Add(1 * time.Hour))}},
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformAnthropic, Priority: 2, Status: billing.StatusActive, Schedulable: true}},
			},
			expectedID: 2,
		},
		{
			name: "非active提供商被跳过",
			providers: []gatewayprovider.ExecutionProvider{
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAnthropic, Priority: 1, Status: "error", Schedulable: true}},
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformAnthropic, Priority: 2, Status: billing.StatusActive, Schedulable: true}},
			},
			expectedID: 2,
		},
		{
			name: "schedulable=false被跳过",
			providers: []gatewayprovider.ExecutionProvider{
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: false}},
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformAnthropic, Priority: 2, Status: billing.StatusActive, Schedulable: true}},
			},
			expectedID: 2,
		},
		{
			name: "过期的过载提供商可调度",
			providers: []gatewayprovider.ExecutionProvider{
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true, OverloadUntil: ptr(now.Add(-1 * time.Hour))}},
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformAnthropic, Priority: 2, Status: billing.StatusActive, Schedulable: true}},
			},
			expectedID: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockProviderRepoForPlatform{
				providers:     tt.providers,
				providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
			}
			for i := range repo.providers {
				repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
			}

			cache := &mockGatewayCacheForPlatform{}

			svc := newGenericSelectionForTest(GenericDependencies{Reads: Reads{Providers: repo}, Shared: Shared{Cache: cache}}, testConfig())

			acc, err := svc.selectProviderForModelWithPlatform(ctx, selectionFixtureGroupID(ctx), "", "claude-3-5-sonnet-20241022", nil, capability.PlatformAnthropic)
			require.NoError(t, err)
			require.NotNil(t, acc)
			require.Equal(t, tt.expectedID, acc.Record.ID)
		})
	}
}

// TestGatewayService_SelectProviderForModelWithPlatform_StickySession 测试粘性会话
func TestGatewayService_SelectProviderForModelWithPlatform_StickySession(t *testing.T) {
	ctx := context.Background()

	t.Run("粘性会话命中-同平台", func(t *testing.T) {
		repo := &mockProviderRepoForPlatform{
			providers: []gatewayprovider.ExecutionProvider{
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAnthropic, Priority: 2, Status: billing.StatusActive, Schedulable: true}},
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true}},
			},
			providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
		}
		for i := range repo.providers {
			repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
		}

		cache := &mockGatewayCacheForPlatform{
			sessionBindings: map[string]int64{"session-123": 1},
		}

		svc := newGenericSelectionForTest(GenericDependencies{Reads: Reads{Providers: repo}, Shared: Shared{Cache: cache}}, testConfig())

		acc, err := svc.selectProviderForModelWithPlatform(ctx, selectionFixtureGroupID(ctx), "session-123", "claude-3-5-sonnet-20241022", nil, capability.PlatformAnthropic)
		require.NoError(t, err)
		require.NotNil(t, acc)
		require.Equal(t, int64(1), acc.Record.ID, "应返回粘性会话绑定的提供商")
	})

	t.Run("粘性会话不匹配平台-降级选择", func(t *testing.T) {
		repo := &mockProviderRepoForPlatform{
			providers: []gatewayprovider.ExecutionProvider{
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAntigravity, Priority: 2, Status: billing.StatusActive, Schedulable: true}}, // 粘性会话绑定但平台不匹配
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true}},
			},
			providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
		}
		for i := range repo.providers {
			repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
		}

		cache := &mockGatewayCacheForPlatform{
			sessionBindings: map[string]int64{"session-123": 1}, // 绑定 antigravity 提供商
		}

		svc := newGenericSelectionForTest(GenericDependencies{Reads: Reads{Providers: repo}, Shared: Shared{Cache: cache}}, testConfig())

		// 请求 anthropic 平台，但粘性会话绑定的是 antigravity 提供商
		acc, err := svc.selectProviderForModelWithPlatform(ctx, selectionFixtureGroupID(ctx), "session-123", "claude-3-5-sonnet-20241022", nil, capability.PlatformAnthropic)
		require.NoError(t, err)
		require.NotNil(t, acc)
		require.Equal(t, int64(2), acc.Record.ID, "粘性会话提供商平台不匹配，应降级选择同平台提供商")
		require.Equal(t, capability.PlatformAnthropic, acc.Record.Platform)
	})

	t.Run("粘性会话提供商被排除-降级选择", func(t *testing.T) {
		repo := &mockProviderRepoForPlatform{
			providers: []gatewayprovider.ExecutionProvider{
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAnthropic, Priority: 2, Status: billing.StatusActive, Schedulable: true}},
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true}},
			},
			providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
		}
		for i := range repo.providers {
			repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
		}

		cache := &mockGatewayCacheForPlatform{
			sessionBindings: map[string]int64{"session-123": 1},
		}

		svc := newGenericSelectionForTest(GenericDependencies{Reads: Reads{Providers: repo}, Shared: Shared{Cache: cache}}, testConfig())

		excludedIDs := map[int64]struct{}{1: {}}
		acc, err := svc.selectProviderForModelWithPlatform(ctx, selectionFixtureGroupID(ctx), "session-123", "claude-3-5-sonnet-20241022", excludedIDs, capability.PlatformAnthropic)
		require.NoError(t, err)
		require.NotNil(t, acc)
		require.Equal(t, int64(2), acc.Record.ID, "粘性会话提供商被排除，应选择其他提供商")
	})

	t.Run("粘性会话提供商不可调度-降级选择", func(t *testing.T) {
		repo := &mockProviderRepoForPlatform{
			providers: []gatewayprovider.ExecutionProvider{
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAnthropic, Priority: 2, Status: "error", Schedulable: true}},
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true}},
			},
			providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
		}
		for i := range repo.providers {
			repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
		}

		cache := &mockGatewayCacheForPlatform{
			sessionBindings: map[string]int64{"session-123": 1},
		}

		svc := newGenericSelectionForTest(GenericDependencies{Reads: Reads{Providers: repo}, Shared: Shared{Cache: cache}}, testConfig())

		acc, err := svc.selectProviderForModelWithPlatform(ctx, selectionFixtureGroupID(ctx), "session-123", "claude-3-5-sonnet-20241022", nil, capability.PlatformAnthropic)
		require.NoError(t, err)
		require.NotNil(t, acc)
		require.Equal(t, int64(2), acc.Record.ID, "粘性会话提供商不可调度，应选择其他提供商")
	})
}

func TestGatewayService_SelectProviderForModelWithExclusions_ForcePlatform(t *testing.T) {
	ctx := context.Background()
	ctx = apikey.WithForcePlatform(ctx, capability.PlatformAntigravity)

	repo := &mockProviderRepoForPlatform{
		providers: []gatewayprovider.ExecutionProvider{
			{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true}},
			{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformAntigravity, Priority: 2, Status: billing.StatusActive, Schedulable: true}},
		},
		providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
	}
	for i := range repo.providers {
		repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
	}

	cache := &mockGatewayCacheForPlatform{}

	svc := newGenericSelectionForTest(GenericDependencies{Reads: Reads{Providers: repo}, Shared: Shared{Cache: cache}}, testConfig())

	acc, err := svc.SelectProviderForModelWithExclusions(ctx, selectionFixtureGroupID(ctx), "", "claude-sonnet-4-5", nil)
	require.NoError(t, err)
	require.NotNil(t, acc)
	require.Equal(t, int64(2), acc.Record.ID)
	require.Equal(t, capability.PlatformAntigravity, acc.Record.Platform)
}

func TestGatewayService_SelectProviderForModelWithPlatform_RoutedStickySessionClears(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10)
	requestedModel := "claude-3-5-sonnet-20241022"

	repo := &mockProviderRepoForPlatform{
		providers: []gatewayprovider.ExecutionProvider{
			{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAnthropic, Priority: 2, Status: billing.StatusDisabled, Schedulable: true}},
			{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true}},
		},
		providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
	}
	for i := range repo.providers {
		repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
	}

	cache := &mockGatewayCacheForPlatform{
		sessionBindings: map[string]int64{"session-123": 1},
	}

	groupRepo := &mockGroupRepoForGateway{
		groups: map[int64]*routing.Group{
			groupID: {
				ID:   groupID,
				Name: "route-group",

				Status:              billing.StatusActive,
				Hydrated:            true,
				ModelRoutingEnabled: true,
				ModelRouting: map[string][]int64{
					requestedModel: {1, 2},
				},
			},
		},
	}

	svc := newGenericSelectionForTest(GenericDependencies{
		Reads: Reads{
			Groups:    groupRepo,
			Providers: repo,
		},
		Shared: Shared{Cache: cache},
	}, testConfig())

	acc, err := svc.selectProviderForModelWithPlatform(ctx, &groupID, "session-123", requestedModel, nil, capability.PlatformAnthropic)
	require.NoError(t, err)
	require.NotNil(t, acc)
	require.Equal(t, int64(2), acc.Record.ID)
	require.Equal(t, 1, cache.deletedSessions["session-123"])
	require.Equal(t, int64(2), cache.sessionBindings["session-123"])
}

func TestGatewayService_SelectProviderForModelWithPlatform_RoutedStickySessionHit(t *testing.T) {
	ctx := context.Background()
	groupID := int64(11)
	requestedModel := "claude-3-5-sonnet-20241022"

	repo := &mockProviderRepoForPlatform{
		providers: []gatewayprovider.ExecutionProvider{
			{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true}},
			{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformAnthropic, Priority: 2, Status: billing.StatusActive, Schedulable: true}},
		},
		providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
	}
	for i := range repo.providers {
		repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
	}

	cache := &mockGatewayCacheForPlatform{
		sessionBindings: map[string]int64{"session-456": 1},
	}

	groupRepo := &mockGroupRepoForGateway{
		groups: map[int64]*routing.Group{
			groupID: {
				ID:   groupID,
				Name: "route-group-hit",

				Status:              billing.StatusActive,
				Hydrated:            true,
				ModelRoutingEnabled: true,
				ModelRouting: map[string][]int64{
					requestedModel: {1, 2},
				},
			},
		},
	}

	svc := newGenericSelectionForTest(GenericDependencies{
		Reads: Reads{
			Groups:    groupRepo,
			Providers: repo,
		},
		Shared: Shared{Cache: cache},
	}, testConfig())

	acc, err := svc.selectProviderForModelWithPlatform(ctx, &groupID, "session-456", requestedModel, nil, capability.PlatformAnthropic)
	require.NoError(t, err)
	require.NotNil(t, acc)
	require.Equal(t, int64(1), acc.Record.ID)
}

func TestGatewayService_SelectProviderForModelWithPlatform_RoutedFallbackToNormal(t *testing.T) {
	ctx := context.Background()
	groupID := int64(12)
	requestedModel := "claude-3-5-sonnet-20241022"

	repo := &mockProviderRepoForPlatform{
		providers: []gatewayprovider.ExecutionProvider{
			{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true}},
			{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformAnthropic, Priority: 2, Status: billing.StatusActive, Schedulable: true}},
		},
		providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
	}
	for i := range repo.providers {
		repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
	}

	cache := &mockGatewayCacheForPlatform{}

	groupRepo := &mockGroupRepoForGateway{
		groups: map[int64]*routing.Group{
			groupID: {
				ID:   groupID,
				Name: "route-fallback",

				Status:              billing.StatusActive,
				Hydrated:            true,
				ModelRoutingEnabled: true,
				ModelRouting: map[string][]int64{
					requestedModel: {99},
				},
			},
		},
	}

	svc := newGenericSelectionForTest(GenericDependencies{Reads: Reads{Providers: repo, Groups: groupRepo}, Shared: Shared{Cache: cache}}, testConfig())

	acc, err := svc.selectProviderForModelWithPlatform(ctx, &groupID, "", requestedModel, nil, capability.PlatformAnthropic)
	require.NoError(t, err)
	require.NotNil(t, acc)
	require.Equal(t, int64(1), acc.Record.ID)
}

func TestGatewayService_SelectProviderForModelWithPlatform_NoModelSupport(t *testing.T) {
	ctx := context.Background()

	repo := &mockProviderRepoForPlatform{
		providers: []gatewayprovider.ExecutionProvider{
			{
				Record: providercore.Record{
					LoadLocation: time.LoadLocation, ID: 1,
					Platform:    capability.PlatformAnthropic,
					Priority:    1,
					Status:      billing.StatusActive,
					Schedulable: true,
					Credentials: map[string]any{"model_mapping": map[string]any{"claude-3-5-haiku-20241022": "claude-3-5-haiku-20241022"}},
				},
			},
		},
		providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
	}
	for i := range repo.providers {
		repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
	}

	cache := &mockGatewayCacheForPlatform{}

	svc := newGenericSelectionForTest(GenericDependencies{Reads: Reads{Providers: repo}, Shared: Shared{Cache: cache}}, testConfig())

	acc, err := svc.selectProviderForModelWithPlatform(ctx, selectionFixtureGroupID(ctx), "", "claude-3-5-sonnet-20241022", nil, capability.PlatformAnthropic)
	require.Error(t, err)
	require.Nil(t, acc)
	var modelErr *routing.GroupModelUnsupportedError
	require.True(t, errors.As(err, &modelErr))
	require.Equal(t, capability.PlatformAnthropic, modelErr.Platform)
	require.Equal(t, "claude-3-5-sonnet-20241022", modelErr.RequestedModel)
	require.Equal(t, []string{"claude-3-5-haiku-20241022"}, modelErr.AvailableModels)
	require.Contains(t, err.Error(), `The current group does not support the requested model "claude-3-5-sonnet-20241022"`)
	require.Contains(t, err.Error(), "Available models: claude-3-5-haiku-20241022")
}

func TestGatewayService_SelectProviderForModelWithPlatform_ModelRateLimitedNotGroupUnsupported(t *testing.T) {
	ctx := context.Background()
	resetAt := time.Now().Add(10 * time.Minute).Format(time.RFC3339)

	repo := &mockProviderRepoForPlatform{
		providers: []gatewayprovider.ExecutionProvider{
			{
				Record: providercore.Record{
					LoadLocation: time.LoadLocation, ID: 1,
					Platform:    capability.PlatformAnthropic,
					Priority:    1,
					Status:      billing.StatusActive,
					Schedulable: true,
					Credentials: map[string]any{"model_mapping": map[string]any{"claude-3-5-sonnet-20241022": "claude-3-5-sonnet-20241022"}},
					Extra: map[string]any{
						"model_rate_limits": map[string]any{
							"claude-3-5-sonnet-20241022": map[string]any{"rate_limit_reset_at": resetAt},
						},
					},
				},
			},
			{
				Record: providercore.Record{
					LoadLocation: time.LoadLocation, ID: 2,
					Platform:    capability.PlatformAnthropic,
					Priority:    2,
					Status:      billing.StatusActive,
					Schedulable: true,
					Credentials: map[string]any{"model_mapping": map[string]any{"claude-3-5-haiku-20241022": "claude-3-5-haiku-20241022"}},
				},
			},
		},
		providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
	}
	for i := range repo.providers {
		repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
	}

	svc := newGenericSelectionForTest(GenericDependencies{
		Reads:  Reads{Providers: repo},
		Shared: Shared{Cache: &mockGatewayCacheForPlatform{}},
	}, testConfig())

	acc, err := svc.selectProviderForModelWithPlatform(ctx, selectionFixtureGroupID(ctx), "", "claude-3-5-sonnet-20241022", nil, capability.PlatformAnthropic)
	require.Error(t, err)
	require.Nil(t, acc)
	var modelErr *routing.GroupModelUnsupportedError
	require.False(t, errors.As(err, &modelErr))
	require.ErrorIs(t, err, scheduler.ErrNoAvailableProviders)
}

func TestGatewayService_SelectProviderForModelWithPlatform_GeminiPreferOAuth(t *testing.T) {
	ctx := context.Background()

	repo := &mockProviderRepoForPlatform{
		providers: []gatewayprovider.ExecutionProvider{
			{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformGemini, Priority: 1, Status: billing.StatusActive, Schedulable: true, Type: capability.ProviderTypeAPIKey}},
			{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformGemini, Priority: 1, Status: billing.StatusActive, Schedulable: true, Type: capability.ProviderTypeOAuth}},
		},
		providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
	}
	for i := range repo.providers {
		repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
	}

	cache := &mockGatewayCacheForPlatform{}

	svc := newGenericSelectionForTest(GenericDependencies{Reads: Reads{Providers: repo}, Shared: Shared{Cache: cache}}, testConfig())

	acc, err := svc.selectProviderForModelWithPlatform(ctx, selectionFixtureGroupID(ctx), "", "gemini-2.5-pro", nil, capability.PlatformGemini)
	require.NoError(t, err)
	require.NotNil(t, acc)
	require.Equal(t, int64(2), acc.Record.ID)
}

func TestGatewayService_SelectProviderForModelWithPlatform_GeminiAPIKeyModelMappingFilter(t *testing.T) {
	ctx := context.Background()

	repo := &mockProviderRepoForPlatform{
		providers: []gatewayprovider.ExecutionProvider{
			{
				Record: providercore.Record{
					LoadLocation: time.LoadLocation, ID: 1,
					Platform:    capability.PlatformGemini,
					Type:        capability.ProviderTypeAPIKey,
					Priority:    1,
					Status:      billing.StatusActive,
					Schedulable: true,
					Credentials: map[string]any{"model_mapping": map[string]any{"gemini-2.5-pro": "gemini-2.5-pro"}},
				},
			},
			{
				Record: providercore.Record{
					LoadLocation: time.LoadLocation, ID: 2,
					Platform:    capability.PlatformGemini,
					Type:        capability.ProviderTypeAPIKey,
					Priority:    2,
					Status:      billing.StatusActive,
					Schedulable: true,
					Credentials: map[string]any{"model_mapping": map[string]any{"gemini-2.5-flash": "gemini-2.5-flash"}},
				},
			},
		},
		providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
	}
	for i := range repo.providers {
		repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
	}

	cache := &mockGatewayCacheForPlatform{}

	svc := newGenericSelectionForTest(GenericDependencies{Reads: Reads{Providers: repo}, Shared: Shared{Cache: cache}}, testConfig())

	acc, err := svc.selectProviderForModelWithPlatform(ctx, selectionFixtureGroupID(ctx), "", "gemini-2.5-flash", nil, capability.PlatformGemini)
	require.NoError(t, err)
	require.NotNil(t, acc)
	require.Equal(t, int64(2), acc.Record.ID, "应过滤不支持请求模型的 APIKey 提供商")

	acc, err = svc.selectProviderForModelWithPlatform(ctx, selectionFixtureGroupID(ctx), "", "gemini-3-pro-preview", nil, capability.PlatformGemini)
	require.Error(t, err)
	require.Nil(t, acc)
	var modelErr *routing.GroupModelUnsupportedError
	require.True(t, errors.As(err, &modelErr))
	require.Equal(t, capability.PlatformGemini, modelErr.Platform)
	require.Equal(t, "gemini-3-pro-preview", modelErr.RequestedModel)
	require.Equal(t, []string{"gemini-2.5-flash", "gemini-2.5-pro"}, modelErr.AvailableModels)
	require.Contains(t, err.Error(), `The current group does not support the requested model "gemini-3-pro-preview"`)
	require.Contains(t, err.Error(), "Available models: gemini-2.5-flash, gemini-2.5-pro")
}

func TestGatewayService_SelectProviderForModelWithPlatform_StickyInGroup(t *testing.T) {
	ctx := context.Background()
	groupID := int64(50)

	repo := &mockProviderRepoForPlatform{
		providers: []gatewayprovider.ExecutionProvider{
			{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true, ProviderGroups: []providercore.GroupMembership{{GroupID: groupID}}}},
			{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformAnthropic, Priority: 2, Status: billing.StatusActive, Schedulable: true, ProviderGroups: []providercore.GroupMembership{{GroupID: groupID}}}},
		},
		providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
	}
	for i := range repo.providers {
		repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
	}

	cache := &mockGatewayCacheForPlatform{
		sessionBindings: map[string]int64{"session-group": 1},
	}

	svc := newGenericSelectionForTest(GenericDependencies{Reads: Reads{Providers: repo}, Shared: Shared{Cache: cache}}, testConfig())

	acc, err := svc.selectProviderForModelWithPlatform(ctx, &groupID, "session-group", "", nil, capability.PlatformAnthropic)
	require.NoError(t, err)
	require.NotNil(t, acc)
	require.Equal(t, int64(1), acc.Record.ID)
}

func TestGatewayService_SelectProviderForModelWithPlatform_StickyModelMismatchFallback(t *testing.T) {
	ctx := context.Background()

	repo := &mockProviderRepoForPlatform{
		providers: []gatewayprovider.ExecutionProvider{
			{
				Record: providercore.Record{
					LoadLocation: time.LoadLocation, ID: 1,
					Platform:    capability.PlatformAnthropic,
					Priority:    1,
					Status:      billing.StatusActive,
					Schedulable: true,
					Credentials: map[string]any{"model_mapping": map[string]any{"claude-3-5-haiku-20241022": "claude-3-5-haiku-20241022"}},
				},
			},
			{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformAnthropic, Priority: 2, Status: billing.StatusActive, Schedulable: true}},
		},
		providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
	}
	for i := range repo.providers {
		repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
	}

	cache := &mockGatewayCacheForPlatform{
		sessionBindings: map[string]int64{"session-miss": 1},
	}

	svc := newGenericSelectionForTest(GenericDependencies{Reads: Reads{Providers: repo}, Shared: Shared{Cache: cache}}, testConfig())

	acc, err := svc.selectProviderForModelWithPlatform(ctx, selectionFixtureGroupID(ctx), "session-miss", "claude-3-5-sonnet-20241022", nil, capability.PlatformAnthropic)
	require.NoError(t, err)
	require.NotNil(t, acc)
	require.Equal(t, int64(2), acc.Record.ID)
}

func TestGatewayService_SelectProviderForModelWithPlatform_PreferNeverUsed(t *testing.T) {
	ctx := context.Background()
	lastUsed := time.Now().Add(-1 * time.Hour)

	repo := &mockProviderRepoForPlatform{
		providers: []gatewayprovider.ExecutionProvider{
			{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true, LastUsedAt: &lastUsed}},
			{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true}},
		},
		providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
	}
	for i := range repo.providers {
		repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
	}

	cache := &mockGatewayCacheForPlatform{}

	svc := newGenericSelectionForTest(GenericDependencies{Reads: Reads{Providers: repo}, Shared: Shared{Cache: cache}}, testConfig())

	acc, err := svc.selectProviderForModelWithPlatform(ctx, selectionFixtureGroupID(ctx), "", "claude-3-5-sonnet-20241022", nil, capability.PlatformAnthropic)
	require.NoError(t, err)
	require.NotNil(t, acc)
	require.Equal(t, int64(2), acc.Record.ID)
}

func TestGatewayService_SelectProviderForModelWithPlatform_NoProviders(t *testing.T) {
	ctx := context.Background()
	repo := &mockProviderRepoForPlatform{
		providers:     []gatewayprovider.ExecutionProvider{},
		providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
	}

	cache := &mockGatewayCacheForPlatform{}

	svc := newGenericSelectionForTest(GenericDependencies{Reads: Reads{Providers: repo}, Shared: Shared{Cache: cache}}, testConfig())

	acc, err := svc.selectProviderForModelWithPlatform(ctx, selectionFixtureGroupID(ctx), "", "", nil, capability.PlatformAnthropic)
	require.Error(t, err)
	require.Nil(t, acc)
	require.ErrorIs(t, err, scheduler.ErrNoAvailableProviders)
}

func TestGatewayService_isModelSupportedByProvider(t *testing.T) {
	tests := []struct {
		name     string
		provider *gatewayprovider.ExecutionProvider
		model    string
		expected bool
	}{
		{
			name:     "Antigravity平台-支持默认映射中的claude模型",
			provider: &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformAntigravity}},
			model:    "claude-sonnet-4-5",
			expected: true,
		},
		{
			name:     "Antigravity平台-不支持非默认映射中的claude模型",
			provider: &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformAntigravity}},
			model:    "claude-3-5-sonnet-20241022",
			expected: false,
		},
		{
			name:     "Antigravity平台-支持gemini模型",
			provider: &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformAntigravity}},
			model:    "gemini-2.5-flash",
			expected: true,
		},
		{
			name:     "Antigravity平台-不支持gpt模型",
			provider: &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformAntigravity}},
			model:    "gpt-4",
			expected: false,
		},
		{
			name:     "Anthropic平台-无映射配置-支持所有模型",
			provider: &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformAnthropic}},
			model:    "claude-3-5-sonnet-20241022",
			expected: false,
		},
		{
			name: "Anthropic平台-有映射配置-未命中映射时按透传支持模型",
			provider: &gatewayprovider.ExecutionProvider{
				Record: providercore.Record{
					LoadLocation: time.LoadLocation, Platform: capability.PlatformAnthropic,
					Credentials: map[string]any{"model_mapping": map[string]any{"claude-opus-4": "x"}},
				},
			},
			model:    "claude-3-5-sonnet-20241022",
			expected: false,
		},
		{
			name: "Anthropic平台-有映射配置-支持配置的模型",
			provider: &gatewayprovider.ExecutionProvider{
				Record: providercore.Record{
					LoadLocation: time.LoadLocation, Platform: capability.PlatformAnthropic,
					Credentials: map[string]any{"model_mapping": map[string]any{"claude-3-5-sonnet-20241022": "x"}},
				},
			},
			model:    "claude-3-5-sonnet-20241022",
			expected: true,
		},
		{
			name:     "Gemini平台-无映射配置-支持所有模型",
			provider: &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformGemini, Type: capability.ProviderTypeAPIKey}},
			model:    "gemini-2.5-flash",
			expected: true,
		},
		{
			name: "Gemini平台-有映射配置-未命中映射时按透传支持模型",
			provider: &gatewayprovider.ExecutionProvider{
				Record: providercore.Record{
					LoadLocation: time.LoadLocation, Platform: capability.PlatformGemini,
					Type: capability.ProviderTypeAPIKey,
					Credentials: map[string]any{
						"model_mapping": map[string]any{"gemini-2.5-pro": "upstream-model"},
					},
				},
			},
			model:    "gemini-2.5-flash",
			expected: true,
		},
		{
			name: "Gemini平台-有映射配置-支持配置的模型",
			provider: &gatewayprovider.ExecutionProvider{
				Record: providercore.Record{
					LoadLocation: time.LoadLocation, Platform: capability.PlatformGemini,
					Type: capability.ProviderTypeAPIKey,
					Credentials: map[string]any{
						"model_mapping": map[string]any{"gemini-2.5-pro": "gemini-2.5-pro"},
					},
				},
			},
			model:    "gemini-2.5-pro",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gatewayprovider.ExecutionModelPolicy(tt.provider).Supports(context.Background(), tt.model)
			require.Equal(t, tt.expected, got)
		})
	}
}

// TestGenericGroupIncludesAntigravityWithoutMixedFlag 测试混合调度
func TestGenericGroupIncludesAntigravityWithoutMixedFlag(t *testing.T) {
	groupID := int64(1)
	values := []gatewayprovider.ExecutionProvider{
		mixedGroupProvider(1, capability.PlatformAnthropic, "*", groupID),
		mixedGroupProvider(2, capability.PlatformAntigravity, "*", groupID),
	}
	values[0].Record.Priority = 2
	values[1].Record.Type = capability.ProviderTypeOAuth
	values[1].Record.Extra = map[string]any{"mixed_scheduling": false}
	repo := advancedSchedulerRegressionProviderRepo(values)
	svc := newGenericSelectionForTest(GenericDependencies{Reads: Reads{Providers: repo}}, nil)
	ctx := requeststate.WithGroup(context.Background(), &routing.Group{ID: groupID, Status: billing.StatusActive, Hydrated: true})
	selected, err := svc.SelectProviderForModelWithExclusions(ctx, &groupID, "", "claude-sonnet-4-5", nil)
	require.NoError(t, err)
	require.Equal(t, int64(2), selected.Record.ID)
	forced := apikey.WithForcePlatform(ctx, capability.PlatformAnthropic)
	selected, err = svc.SelectProviderForModelWithExclusions(forced, &groupID, "", "claude-sonnet-4-5", nil)
	require.NoError(t, err)
	require.Equal(t, int64(1), selected.Record.ID)
}

type mockConcurrencyCache struct {
	acquireProviderCalls int
	loadBatchCalls       int
	acquireResults       map[int64]bool
	loadBatchErr         error
	loadMap              map[int64]*scheduler.ProviderLoadInfo
	waitCounts           map[int64]int
	skipDefaultLoad      bool
}

func (m *mockConcurrencyCache) AcquireProviderSlot(ctx context.Context, providerID int64, maxConcurrency int, requestID string) (bool, error) {
	m.acquireProviderCalls++
	if m.acquireResults != nil {
		if result, ok := m.acquireResults[providerID]; ok {
			return result, nil
		}
	}
	return true, nil
}

func (m *mockConcurrencyCache) ReleaseProviderSlot(ctx context.Context, providerID int64, requestID string) error {
	return nil
}

func (m *mockConcurrencyCache) GetProviderConcurrency(ctx context.Context, providerID int64) (int, error) {
	return 0, nil
}

func (m *mockConcurrencyCache) GetProviderConcurrencyBatch(ctx context.Context, providerIDs []int64) (map[int64]int, error) {
	result := make(map[int64]int, len(providerIDs))
	for _, providerID := range providerIDs {
		result[providerID] = 0
	}
	return result, nil
}

func (m *mockConcurrencyCache) IncrementProviderWaitCount(ctx context.Context, providerID int64, maxWait int) (bool, error) {
	return true, nil
}

func (m *mockConcurrencyCache) DecrementProviderWaitCount(ctx context.Context, providerID int64) error {
	return nil
}

func (m *mockConcurrencyCache) GetProviderWaitingCount(ctx context.Context, providerID int64) (int, error) {
	if m.waitCounts != nil {
		if count, ok := m.waitCounts[providerID]; ok {
			return count, nil
		}
	}
	return 0, nil
}

func (m *mockConcurrencyCache) AcquireUserSlot(ctx context.Context, userID int64, maxConcurrency int, requestID string) (bool, error) {
	return true, nil
}

func (m *mockConcurrencyCache) ReleaseUserSlot(ctx context.Context, userID int64, requestID string) error {
	return nil
}

func (m *mockConcurrencyCache) GetUserConcurrency(ctx context.Context, userID int64) (int, error) {
	return 0, nil
}

func (m *mockConcurrencyCache) IncrementWaitCount(ctx context.Context, userID int64, maxWait int) (bool, error) {
	return true, nil
}

func (m *mockConcurrencyCache) DecrementWaitCount(ctx context.Context, userID int64) error {
	return nil
}

func (m *mockConcurrencyCache) GetProvidersLoadBatch(ctx context.Context, providers []scheduler.ProviderWithConcurrency) (map[int64]*scheduler.ProviderLoadInfo, error) {
	m.loadBatchCalls++
	if m.loadBatchErr != nil {
		return nil, m.loadBatchErr
	}
	result := make(map[int64]*scheduler.ProviderLoadInfo, len(providers))
	if m.skipDefaultLoad && m.loadMap != nil {
		for _, acc := range providers {
			if load, ok := m.loadMap[acc.ID]; ok {
				result[acc.ID] = load
			}
		}
		return result, nil
	}
	for _, acc := range providers {
		if m.loadMap != nil {
			if load, ok := m.loadMap[acc.ID]; ok {
				result[acc.ID] = load
				continue
			}
		}
		result[acc.ID] = &scheduler.ProviderLoadInfo{
			ProviderID:         acc.ID,
			CurrentConcurrency: 0,
			WaitingCount:       0,
			LoadRate:           0,
		}
	}
	return result, nil
}

func (m *mockConcurrencyCache) CleanupExpiredProviderSlots(ctx context.Context, providerID int64) error {
	return nil
}

func (m *mockConcurrencyCache) CleanupExpiredProviderSlotKeys(ctx context.Context) error {
	return nil
}

func (m *mockConcurrencyCache) CleanupStaleProcessSlots(ctx context.Context, activeRequestPrefix string) error {
	return nil
}

func (m *mockConcurrencyCache) GetUsersLoadBatch(ctx context.Context, users []scheduler.UserWithConcurrency) (map[int64]*scheduler.UserLoadInfo, error) {
	result := make(map[int64]*scheduler.UserLoadInfo, len(users))
	for _, user := range users {
		result[user.ID] = &scheduler.UserLoadInfo{
			UserID:             user.ID,
			CurrentConcurrency: 0,
			WaitingCount:       0,
			LoadRate:           0,
		}
	}
	return result, nil
}

func TestSelectProviderWithLoadAwareness_FiltersUpstreamRestrictedProviders(t *testing.T) {
	for _, loadBatchEnabled := range []bool{false, true} {
		loadMode := "旧版调度"
		if loadBatchEnabled {
			loadMode = "负载批量调度"
		}
		for _, modelRoutingEnabled := range []bool{false, true} {
			stickyMode := "普通粘性提供商"
			if modelRoutingEnabled {
				stickyMode = "模型路由粘性提供商"
			}
			t.Run(loadMode+"/"+stickyMode, func(t *testing.T) {
				groupID := int64(4210)
				pricingConfig := routingtestkit.Configuration{
					ID:                 76,
					Status:             billing.StatusActive,
					RestrictModels:     true,
					BillingModelSource: routing.BillingModelSourceUpstream,
					ModelMapping:       map[string]string{"client-alias": "group-model"},
					ModelPricing: []routing.ModelPricingEntry{{
						Models: []string{"allowed-upstream"},
					}},
				}
				providers := []gatewayprovider.ExecutionProvider{
					{
						Record: providercore.Record{
							LoadLocation: time.LoadLocation, ID: 1,
							Platform:    capability.PlatformAnthropic,
							Priority:    1,
							Status:      billing.StatusActive,
							Schedulable: true,
							Concurrency: 5,
							ProviderGroups: []providercore.GroupMembership{{
								ProviderID: 1,
								GroupID:    groupID,
							}},
							Credentials: map[string]any{"model_mapping": map[string]any{"group-model": "blocked-upstream"}},
						},
					},
					{
						Record: providercore.Record{
							LoadLocation: time.LoadLocation, ID: 2,
							Platform:    capability.PlatformAnthropic,
							Priority:    2,
							Status:      billing.StatusActive,
							Schedulable: true,
							Concurrency: 5,
							ProviderGroups: []providercore.GroupMembership{{
								ProviderID: 2,
								GroupID:    groupID,
							}},
							Credentials: map[string]any{"model_mapping": map[string]any{"group-model": "allowed-upstream"}},
						},
					},
				}
				providerRepo := &mockProviderRepoForPlatform{providers: providers, providersByID: map[int64]*gatewayprovider.ExecutionProvider{}}
				for i := range providerRepo.providers {
					providerRepo.providersByID[providerRepo.providers[i].Record.ID] = &providerRepo.providers[i]
				}
				group := &routing.Group{
					ID: groupID,

					Status:              billing.StatusActive,
					Hydrated:            true,
					ModelRoutingEnabled: modelRoutingEnabled,
				}
				if modelRoutingEnabled {
					group.ModelRouting = map[string][]int64{"group-model": {1, 2}}
				}

				cfg := testConfig()
				cfg.Gateway.Scheduling.LoadBatchEnabled = loadBatchEnabled
				svc := newGenericSelectionForTest(GenericDependencies{
					Reads: Reads{
						Providers: providerRepo,

						Groups: &mockGroupRepoForGateway{groups: map[int64]*routing.Group{groupID: group}},
					},
					Shared: Shared{
						Concurrency: scheduler.NewConcurrencyService(&mockConcurrencyCache{}, scheduler.Diagnostics{
							Logf:  logging.LegacyPrintf,
							Event: logging.Event,
						}),
						GroupPolicies: routingtestkit.PricingConfig(groupID,
							capability.PlatformAnthropic, pricingConfig),
						Cache: &mockGatewayCacheForPlatform{
							sessionBindings: map[string]int64{"sticky": 1},
						},
					},
				}, cfg)

				result, err := svc.SelectProviderWithLoadAwareness(context.Background(), &groupID, "sticky", "client-alias", nil, "", 0)
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, int64(2), result.Provider.Record.ID)
			})
		}
	}
}

// TestSelectProviderWithLoadAwareness_AppliesGroupMappingOnce 验证调度入口只把客户端模型 R 映射为一次 C。
func TestSelectProviderWithLoadAwareness_AppliesGroupMappingOnce(t *testing.T) {
	groupID := int64(4212)
	pricingConfig := routingtestkit.Configuration{
		ID:           78,
		Status:       billing.StatusActive,
		ModelMapping: map[string]string{"client-alias": "group-model", "group-model": "double-mapped-model"},
	}
	provider := gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: 1,
			Platform:    capability.PlatformGemini,
			Status:      billing.StatusActive,
			Schedulable: true,
			Concurrency: 5,
			ProviderGroups: []providercore.GroupMembership{{
				ProviderID: 1,
				GroupID:    groupID,
			}},
			Credentials: map[string]any{
				"model_mapping":   map[string]any{"group-model": "upstream-model"},
				"model_whitelist": []any{"upstream-model"},
			},
		},
	}
	providerRepo := &mockProviderRepoForPlatform{
		providers:     []gatewayprovider.ExecutionProvider{provider},
		providersByID: map[int64]*gatewayprovider.ExecutionProvider{provider.Record.ID: &provider},
	}
	group := &routing.Group{
		ID: groupID,

		Status:   billing.StatusActive,
		Hydrated: true,
	}
	svc := newGenericSelectionForTest(GenericDependencies{
		Reads: Reads{
			Providers: providerRepo,

			Groups: &mockGroupRepoForGateway{groups: map[int64]*routing.Group{groupID: group}},
		},
		Shared: Shared{GroupPolicies: routingtestkit.PricingConfig(groupID,
			capability.PlatformGemini, pricingConfig)},
	}, testConfig())

	result, err := svc.SelectProviderWithLoadAwareness(context.Background(), &groupID, "", "client-alias", nil, "", 0)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, provider.Record.ID, result.Provider.Record.ID)
}

func TestLegacySchedulers_FilterUpstreamRestrictedProvidersInEveryShortcut(t *testing.T) {
	testCases := []struct {
		name                string
		mixed               bool
		modelRoutingEnabled bool
		sessionHash         string
	}{
		{name: "单平台普通粘性", sessionHash: "sticky"},
		{name: "单平台路由粘性", modelRoutingEnabled: true, sessionHash: "sticky"},
		{name: "单平台路由候选", modelRoutingEnabled: true},
		{name: "混合调度普通粘性", mixed: true, sessionHash: "sticky"},
		{name: "混合调度路由粘性", mixed: true, modelRoutingEnabled: true, sessionHash: "sticky"},
		{name: "混合调度路由候选", mixed: true, modelRoutingEnabled: true},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			groupID := int64(4211)
			pricingConfig := routingtestkit.Configuration{
				ID:                 77,
				Status:             billing.StatusActive,
				RestrictModels:     true,
				BillingModelSource: routing.BillingModelSourceUpstream,
				ModelMapping:       map[string]string{"client-alias": "group-model"},
				ModelPricing: []routing.ModelPricingEntry{{
					Models: []string{"allowed-upstream"},
				}},
			}
			providers := []gatewayprovider.ExecutionProvider{
				{
					Record: providercore.Record{
						LoadLocation: time.LoadLocation, ID: 1,
						Platform:    capability.PlatformAnthropic,
						Priority:    1,
						Status:      billing.StatusActive,
						Schedulable: true,
						Concurrency: 5,
						ProviderGroups: []providercore.GroupMembership{{
							ProviderID: 1,
							GroupID:    groupID,
						}},
						Credentials: map[string]any{"model_mapping": map[string]any{"group-model": "blocked-upstream"}},
					},
				},
				{
					Record: providercore.Record{
						LoadLocation: time.LoadLocation, ID: 2,
						Platform:    capability.PlatformAnthropic,
						Priority:    2,
						Status:      billing.StatusActive,
						Schedulable: true,
						Concurrency: 5,
						ProviderGroups: []providercore.GroupMembership{{
							ProviderID: 2,
							GroupID:    groupID,
						}},
						Credentials: map[string]any{"model_mapping": map[string]any{"group-model": "allowed-upstream"}},
					},
				},
			}
			providerRepo := &mockProviderRepoForPlatform{providers: providers, providersByID: map[int64]*gatewayprovider.ExecutionProvider{}}
			for i := range providerRepo.providers {
				providerRepo.providersByID[providerRepo.providers[i].Record.ID] = &providerRepo.providers[i]
			}
			group := &routing.Group{
				ID: groupID,

				Status:              billing.StatusActive,
				Hydrated:            true,
				ModelRoutingEnabled: tt.modelRoutingEnabled,
			}
			if tt.modelRoutingEnabled {
				group.ModelRouting = map[string][]int64{"group-model": {1, 2}}
			}
			cache := &mockGatewayCacheForPlatform{sessionBindings: map[string]int64{"sticky": 1}}
			svc := newGenericSelectionForTest(GenericDependencies{
				Reads: Reads{
					Providers: providerRepo,

					Groups: &mockGroupRepoForGateway{groups: map[int64]*routing.Group{groupID: group}},
				},
				Shared: Shared{
					GroupPolicies: routingtestkit.PricingConfig(groupID,
						capability.PlatformAnthropic, pricingConfig),
					Cache: cache,
				},
			}, testConfig())

			ctx := svc.withGroupContext(context.Background(), group)

			var (
				selected *gatewayprovider.ExecutionProvider
				err      error
			)
			if tt.mixed {
				selected, err = svc.selectProviderWithMixedScheduling(ctx, &groupID, tt.sessionHash, "client-alias", nil, capability.PlatformAnthropic)
			} else {
				selected, err = svc.selectProviderForModelWithPlatform(ctx, &groupID, tt.sessionHash, "client-alias", nil, capability.PlatformAnthropic)
			}

			require.NoError(t, err)
			require.NotNil(t, selected)
			require.Equal(t, int64(2), selected.Record.ID)
		})
	}
}

// TestGatewayService_SelectProviderWithLoadAwareness tests load-aware provider selection
func TestGatewayService_SelectProviderWithLoadAwareness(t *testing.T) {
	ctx := context.Background()

	t.Run("禁用负载批量查询-降级到传统选择", func(t *testing.T) {
		repo := &mockProviderRepoForPlatform{
			providers: []gatewayprovider.ExecutionProvider{
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true, Concurrency: 5}},
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformAnthropic, Priority: 2, Status: billing.StatusActive, Schedulable: true, Concurrency: 5}},
			},
			providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
		}
		for i := range repo.providers {
			repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
		}

		cache := &mockGatewayCacheForPlatform{}

		cfg := testConfig()
		cfg.Gateway.Scheduling.LoadBatchEnabled = false

		svc := newGenericSelectionForTest(GenericDependencies{Reads: Reads{Providers: repo}, Shared: Shared{Cache: cache, Concurrency: nil}}, cfg)

		// No concurrency service

		result, err := svc.SelectProviderWithLoadAwareness(ctx, selectionFixtureGroupID(ctx), "", "claude-3-5-sonnet-20241022", nil, "", int64(0))
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, result.Provider)
		require.Equal(t, int64(1), result.Provider.Record.ID, "应选择优先级最高的提供商")
	})

	t.Run("模型路由-无ConcurrencyService也生效", func(t *testing.T) {
		groupID := int64(1)
		sessionHash := "sticky"

		repo := &mockProviderRepoForPlatform{
			providers: []gatewayprovider.ExecutionProvider{
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true, Concurrency: 5, ProviderGroups: []providercore.GroupMembership{{GroupID: groupID}}}},
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true, Concurrency: 5, ProviderGroups: []providercore.GroupMembership{{GroupID: groupID}}}},
			},
			providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
		}
		for i := range repo.providers {
			repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
		}

		cache := &mockGatewayCacheForPlatform{
			sessionBindings: map[string]int64{sessionHash: 1},
		}

		groupRepo := &mockGroupRepoForGateway{
			groups: map[int64]*routing.Group{
				groupID: {
					ID: groupID,

					Status:              billing.StatusActive,
					Hydrated:            true,
					ModelRoutingEnabled: true,
					ModelRouting: map[string][]int64{
						"claude-a": {1},
						"claude-b": {2},
					},
				},
			},
		}

		cfg := testConfig()
		cfg.Gateway.Scheduling.LoadBatchEnabled = true

		svc := newGenericSelectionForTest(GenericDependencies{
			Reads:  Reads{Providers: repo, Groups: groupRepo},
			Shared: Shared{Cache: cache, Concurrency: nil},
		}, cfg)

		// legacy path

		result, err := svc.SelectProviderWithLoadAwareness(ctx, &groupID, sessionHash, "claude-b", nil, "", int64(0))
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, result.Provider)
		require.Equal(t, int64(2), result.Provider.Record.ID, "切换到 claude-b 时应按模型路由切换提供商")
		require.Equal(t, int64(2), cache.sessionBindings[sessionHash], "粘性绑定应更新为路由选择的提供商")
	})

	t.Run("无ConcurrencyService-降级到传统选择", func(t *testing.T) {
		repo := &mockProviderRepoForPlatform{
			providers: []gatewayprovider.ExecutionProvider{
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAnthropic, Priority: 2, Status: billing.StatusActive, Schedulable: true, Concurrency: 5}},
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true, Concurrency: 5}},
			},
			providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
		}
		for i := range repo.providers {
			repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
		}

		cache := &mockGatewayCacheForPlatform{}

		cfg := testConfig()
		cfg.Gateway.Scheduling.LoadBatchEnabled = true

		svc := newGenericSelectionForTest(GenericDependencies{Reads: Reads{Providers: repo}, Shared: Shared{Cache: cache, Concurrency: nil}}, cfg)

		result, err := svc.SelectProviderWithLoadAwareness(ctx, selectionFixtureGroupID(ctx), "", "claude-3-5-sonnet-20241022", nil, "", int64(0))
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, result.Provider)
		require.Equal(t, int64(2), result.Provider.Record.ID, "应选择优先级最高的提供商")
	})

	t.Run("排除提供商-不选择被排除的提供商", func(t *testing.T) {
		repo := &mockProviderRepoForPlatform{
			providers: []gatewayprovider.ExecutionProvider{
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true, Concurrency: 5}},
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformAnthropic, Priority: 2, Status: billing.StatusActive, Schedulable: true, Concurrency: 5}},
			},
			providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
		}
		for i := range repo.providers {
			repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
		}

		cache := &mockGatewayCacheForPlatform{}

		cfg := testConfig()
		cfg.Gateway.Scheduling.LoadBatchEnabled = false

		svc := newGenericSelectionForTest(GenericDependencies{Reads: Reads{Providers: repo}, Shared: Shared{Concurrency: nil, Cache: cache}}, cfg)

		excludedIDs := map[int64]struct{}{1: {}}
		result, err := svc.SelectProviderWithLoadAwareness(ctx, selectionFixtureGroupID(ctx), "", "claude-3-5-sonnet-20241022", excludedIDs, "", int64(0))
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, result.Provider)
		require.Equal(t, int64(2), result.Provider.Record.ID, "不应选择被排除的提供商")
	})

	t.Run("粘性命中-不调用GetByID", func(t *testing.T) {
		repo := &mockProviderRepoForPlatform{
			providers: []gatewayprovider.ExecutionProvider{
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true, Concurrency: 5}},
			},
			providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
		}
		for i := range repo.providers {
			repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
		}

		cache := &mockGatewayCacheForPlatform{
			sessionBindings: map[string]int64{"sticky": 1},
		}

		cfg := testConfig()
		cfg.Gateway.Scheduling.LoadBatchEnabled = true

		concurrencyCache := &mockConcurrencyCache{}

		svc := newGenericSelectionForTest(GenericDependencies{
			Reads: Reads{Providers: repo},
			Shared: Shared{
				Cache: cache,
				Concurrency: scheduler.NewConcurrencyService(concurrencyCache,

					scheduler.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
			},
		}, cfg)

		result, err := svc.SelectProviderWithLoadAwareness(ctx, selectionFixtureGroupID(ctx), "sticky", "claude-3-5-sonnet-20241022", nil, "", int64(0))
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, result.Provider)
		require.Equal(t, int64(1), result.Provider.Record.ID)
		require.Equal(t, 0, repo.getByIDCalls, "粘性命中不应调用GetByID")
		require.Equal(t, 0, concurrencyCache.loadBatchCalls, "粘性命中应在负载批量查询前返回")
	})

	t.Run("粘性提供商不在候选集-回退负载感知选择", func(t *testing.T) {
		repo := &mockProviderRepoForPlatform{
			providers: []gatewayprovider.ExecutionProvider{
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true, Concurrency: 5}},
			},
			providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
		}
		for i := range repo.providers {
			repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
		}

		cache := &mockGatewayCacheForPlatform{
			sessionBindings: map[string]int64{"sticky": 1},
		}

		cfg := testConfig()
		cfg.Gateway.Scheduling.LoadBatchEnabled = true

		concurrencyCache := &mockConcurrencyCache{}

		svc := newGenericSelectionForTest(GenericDependencies{
			Reads: Reads{Providers: repo},
			Shared: Shared{
				Cache: cache,
				Concurrency: scheduler.NewConcurrencyService(concurrencyCache,

					scheduler.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
			},
		}, cfg)

		result, err := svc.SelectProviderWithLoadAwareness(ctx, selectionFixtureGroupID(ctx), "sticky", "claude-3-5-sonnet-20241022", nil, "", int64(0))
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, result.Provider)
		require.Equal(t, int64(2), result.Provider.Record.ID, "粘性提供商不在候选集时应回退到可用提供商")
		require.Equal(t, 0, repo.getByIDCalls, "粘性提供商缺失不应回退到GetByID")
		require.Equal(t, 1, concurrencyCache.loadBatchCalls, "应继续进行负载批量查询")
	})

	t.Run("粘性提供商禁用-清理会话并回退选择", func(t *testing.T) {
		testCtx := apikey.WithForcePlatform(ctx, capability.PlatformAnthropic)
		repo := &mockProviderRepoForPlatform{
			providers: []gatewayprovider.ExecutionProvider{
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: false, Concurrency: 5}},
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformAnthropic, Priority: 2, Status: billing.StatusActive, Schedulable: true, Concurrency: 5}},
			},
			providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
		}
		for i := range repo.providers {
			repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
		}
		repo.listPlatformFunc = func(ctx context.Context, platform string) ([]gatewayprovider.ExecutionProvider, error) {
			return repo.providers, nil
		}

		cache := &mockGatewayCacheForPlatform{
			sessionBindings: map[string]int64{"sticky": 1},
		}

		cfg := testConfig()
		cfg.Gateway.Scheduling.LoadBatchEnabled = true

		concurrencyCache := &mockConcurrencyCache{}

		svc := newGenericSelectionForTest(GenericDependencies{
			Reads: Reads{Providers: repo},
			Shared: Shared{
				Cache: cache,
				Concurrency: scheduler.NewConcurrencyService(concurrencyCache,

					scheduler.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
			},
		}, cfg)

		result, err := svc.SelectProviderWithLoadAwareness(testCtx, selectionFixtureGroupID(testCtx), "sticky", "claude-3-5-sonnet-20241022", nil, "", int64(0))
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, result.Provider)
		require.Equal(t, int64(2), result.Provider.Record.ID, "粘性提供商禁用时应回退到可用提供商")
		updatedID, ok := cache.sessionBindings["sticky"]
		require.True(t, ok, "粘性会话应更新绑定")
		require.Equal(t, int64(2), updatedID, "粘性会话应绑定到新提供商")
	})

	t.Run("无可用提供商-返回错误", func(t *testing.T) {
		repo := &mockProviderRepoForPlatform{
			providers:     []gatewayprovider.ExecutionProvider{},
			providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
		}

		cache := &mockGatewayCacheForPlatform{}

		cfg := testConfig()
		cfg.Gateway.Scheduling.LoadBatchEnabled = false

		svc := newGenericSelectionForTest(GenericDependencies{Reads: Reads{Providers: repo}, Shared: Shared{Concurrency: nil, Cache: cache}}, cfg)

		result, err := svc.SelectProviderWithLoadAwareness(ctx, selectionFixtureGroupID(ctx), "", "claude-3-5-sonnet-20241022", nil, "", int64(0))
		require.Error(t, err)
		require.Nil(t, result)
		require.ErrorIs(t, err, scheduler.ErrNoAvailableProviders)
	})

	t.Run("过滤不可调度提供商-限流提供商被跳过", func(t *testing.T) {
		now := time.Now()
		resetAt := now.Add(10 * time.Minute)

		repo := &mockProviderRepoForPlatform{
			providers: []gatewayprovider.ExecutionProvider{
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true, Concurrency: 5, RateLimitResetAt: &resetAt}},
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformAnthropic, Priority: 2, Status: billing.StatusActive, Schedulable: true, Concurrency: 5}},
			},
			providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
		}
		for i := range repo.providers {
			repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
		}

		cache := &mockGatewayCacheForPlatform{}
		cfg := testConfig()
		cfg.Gateway.Scheduling.LoadBatchEnabled = false

		svc := newGenericSelectionForTest(GenericDependencies{Reads: Reads{Providers: repo}, Shared: Shared{Cache: cache, Concurrency: nil}}, cfg)

		result, err := svc.SelectProviderWithLoadAwareness(ctx, selectionFixtureGroupID(ctx), "", "claude-3-5-sonnet-20241022", nil, "", int64(0))
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, result.Provider)
		require.Equal(t, int64(2), result.Provider.Record.ID, "应跳过限流提供商，选择可用提供商")
	})

	t.Run("过滤不可调度提供商-过载提供商被跳过", func(t *testing.T) {
		now := time.Now()
		overloadUntil := now.Add(10 * time.Minute)

		repo := &mockProviderRepoForPlatform{
			providers: []gatewayprovider.ExecutionProvider{
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true, Concurrency: 5, OverloadUntil: &overloadUntil}},
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformAnthropic, Priority: 2, Status: billing.StatusActive, Schedulable: true, Concurrency: 5}},
			},
			providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
		}
		for i := range repo.providers {
			repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
		}

		cache := &mockGatewayCacheForPlatform{}
		cfg := testConfig()
		cfg.Gateway.Scheduling.LoadBatchEnabled = false

		svc := newGenericSelectionForTest(GenericDependencies{Reads: Reads{Providers: repo}, Shared: Shared{Concurrency: nil, Cache: cache}}, cfg)

		result, err := svc.SelectProviderWithLoadAwareness(ctx, selectionFixtureGroupID(ctx), "", "claude-3-5-sonnet-20241022", nil, "", int64(0))
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, result.Provider)
		require.Equal(t, int64(2), result.Provider.Record.ID, "应跳过过载提供商，选择可用提供商")
	})

	t.Run("粘性提供商槽位满-返回粘性等待计划", func(t *testing.T) {
		repo := &mockProviderRepoForPlatform{
			providers: []gatewayprovider.ExecutionProvider{
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true, Concurrency: 5}},
			},
			providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
		}
		for i := range repo.providers {
			repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
		}

		cache := &mockGatewayCacheForPlatform{
			sessionBindings: map[string]int64{"sticky": 1},
		}

		cfg := testConfig()
		cfg.Gateway.Scheduling.LoadBatchEnabled = true
		cfg.Gateway.Scheduling.StickySessionMaxWaiting = 1

		concurrencyCache := &mockConcurrencyCache{
			acquireResults: map[int64]bool{1: false},
			waitCounts:     map[int64]int{1: 0},
		}

		svc := newGenericSelectionForTest(GenericDependencies{
			Reads: Reads{Providers: repo},
			Shared: Shared{
				Cache: cache,
				Concurrency: scheduler.NewConcurrencyService(concurrencyCache,

					scheduler.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
			},
		}, cfg)

		result, err := svc.SelectProviderWithLoadAwareness(ctx, selectionFixtureGroupID(ctx), "sticky", "claude-3-5-sonnet-20241022", nil, "", int64(0))
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, result.WaitPlan)
		require.Equal(t, int64(1), result.Provider.Record.ID)
		require.Equal(t, 0, concurrencyCache.loadBatchCalls)
	})

	t.Run("负载批量查询失败-降级旧顺序选择", func(t *testing.T) {
		repo := &mockProviderRepoForPlatform{
			providers: []gatewayprovider.ExecutionProvider{
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAnthropic, Priority: 2, Status: billing.StatusActive, Schedulable: true, Concurrency: 5}},
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true, Concurrency: 5}},
			},
			providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
		}
		for i := range repo.providers {
			repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
		}

		cache := &mockGatewayCacheForPlatform{}

		cfg := testConfig()
		cfg.Gateway.Scheduling.LoadBatchEnabled = true

		concurrencyCache := &mockConcurrencyCache{
			loadBatchErr: errors.New("load batch failed"),
		}

		svc := newGenericSelectionForTest(GenericDependencies{
			Reads: Reads{Providers: repo},
			Shared: Shared{
				Cache: cache,
				Concurrency: scheduler.NewConcurrencyService(concurrencyCache,

					scheduler.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
			},
		}, cfg)

		result, err := svc.SelectProviderWithLoadAwareness(ctx, selectionFixtureGroupID(ctx), "legacy", "claude-3-5-sonnet-20241022", nil, "", int64(0))
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, result.Provider)
		require.Equal(t, int64(2), result.Provider.Record.ID)
		require.Equal(t, int64(2), cache.sessionBindings["legacy"])
	})

	t.Run("模型路由-粘性提供商等待计划", func(t *testing.T) {
		groupID := int64(20)
		sessionHash := "route-sticky"

		repo := &mockProviderRepoForPlatform{
			providers: []gatewayprovider.ExecutionProvider{
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true, Concurrency: 5}},
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformAnthropic, Priority: 2, Status: billing.StatusActive, Schedulable: true, Concurrency: 5}},
			},
			providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
		}
		for i := range repo.providers {
			repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
		}

		cache := &mockGatewayCacheForPlatform{
			sessionBindings: map[string]int64{sessionHash: 1},
		}

		groupRepo := &mockGroupRepoForGateway{
			groups: map[int64]*routing.Group{
				groupID: {
					ID: groupID,

					Status:              billing.StatusActive,
					Hydrated:            true,
					ModelRoutingEnabled: true,
					ModelRouting: map[string][]int64{
						"claude-3-5-sonnet-20241022": {1, 2},
					},
				},
			},
		}

		cfg := testConfig()
		cfg.Gateway.Scheduling.LoadBatchEnabled = true
		cfg.Gateway.Scheduling.StickySessionMaxWaiting = 1

		concurrencyCache := &mockConcurrencyCache{
			acquireResults: map[int64]bool{1: false},
			waitCounts:     map[int64]int{1: 0},
		}

		svc := newGenericSelectionForTest(GenericDependencies{
			Reads: Reads{Providers: repo, Groups: groupRepo},
			Shared: Shared{
				Concurrency: scheduler.NewConcurrencyService(concurrencyCache,

					scheduler.Diagnostics{
						Logf:  logging.LegacyPrintf,
						Event: logging.Event,
					}),
				Cache: cache,
			},
		}, cfg)

		result, err := svc.SelectProviderWithLoadAwareness(ctx, &groupID, sessionHash, "claude-3-5-sonnet-20241022", nil, "", int64(0))
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, result.WaitPlan)
		require.Equal(t, int64(1), result.Provider.Record.ID)
	})

	t.Run("模型路由-粘性提供商命中", func(t *testing.T) {
		groupID := int64(20)
		sessionHash := "route-hit"

		repo := &mockProviderRepoForPlatform{
			providers: []gatewayprovider.ExecutionProvider{
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true, Concurrency: 5}},
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformAnthropic, Priority: 2, Status: billing.StatusActive, Schedulable: true, Concurrency: 5}},
			},
			providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
		}
		for i := range repo.providers {
			repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
		}

		cache := &mockGatewayCacheForPlatform{
			sessionBindings: map[string]int64{sessionHash: 1},
		}

		groupRepo := &mockGroupRepoForGateway{
			groups: map[int64]*routing.Group{
				groupID: {
					ID: groupID,

					Status:              billing.StatusActive,
					Hydrated:            true,
					ModelRoutingEnabled: true,
					ModelRouting: map[string][]int64{
						"claude-3-5-sonnet-20241022": {1, 2},
					},
				},
			},
		}

		cfg := testConfig()
		cfg.Gateway.Scheduling.LoadBatchEnabled = true

		concurrencyCache := &mockConcurrencyCache{}

		svc := newGenericSelectionForTest(GenericDependencies{
			Reads: Reads{Providers: repo, Groups: groupRepo},
			Shared: Shared{
				Cache: cache,
				Concurrency: scheduler.NewConcurrencyService(
					concurrencyCache, scheduler.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
			},
		}, cfg)

		result, err := svc.SelectProviderWithLoadAwareness(ctx, &groupID, sessionHash, "claude-3-5-sonnet-20241022", nil, "", int64(0))
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, result.Provider)
		require.Equal(t, int64(1), result.Provider.Record.ID)
		require.Equal(t, 0, concurrencyCache.loadBatchCalls)
	})

	t.Run("模型路由-粘性提供商缺失-清理并回退", func(t *testing.T) {
		groupID := int64(22)
		sessionHash := "route-missing"

		repo := &mockProviderRepoForPlatform{
			providers: []gatewayprovider.ExecutionProvider{
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true, Concurrency: 5}},
			},
			providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
		}
		for i := range repo.providers {
			repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
		}

		cache := &mockGatewayCacheForPlatform{
			sessionBindings: map[string]int64{sessionHash: 1},
		}

		groupRepo := &mockGroupRepoForGateway{
			groups: map[int64]*routing.Group{
				groupID: {
					ID: groupID,

					Status:              billing.StatusActive,
					Hydrated:            true,
					ModelRoutingEnabled: true,
					ModelRouting: map[string][]int64{
						"claude-3-5-sonnet-20241022": {1, 2},
					},
				},
			},
		}

		cfg := testConfig()
		cfg.Gateway.Scheduling.LoadBatchEnabled = true

		concurrencyCache := &mockConcurrencyCache{}

		svc := newGenericSelectionForTest(GenericDependencies{
			Reads: Reads{
				Groups:    groupRepo,
				Providers: repo,
			},
			Shared: Shared{
				Cache: cache,
				Concurrency: scheduler.NewConcurrencyService(
					concurrencyCache, scheduler.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
			},
		}, cfg)

		result, err := svc.SelectProviderWithLoadAwareness(ctx, &groupID, sessionHash, "claude-3-5-sonnet-20241022", nil, "", int64(0))
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, result.Provider)
		require.Equal(t, int64(2), result.Provider.Record.ID)
		require.Equal(t, 1, cache.deletedSessions[sessionHash])
		require.Equal(t, int64(2), cache.sessionBindings[sessionHash])
	})

	t.Run("模型路由-按负载选择提供商", func(t *testing.T) {
		groupID := int64(21)

		repo := &mockProviderRepoForPlatform{
			providers: []gatewayprovider.ExecutionProvider{
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true, Concurrency: 5}},
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true, Concurrency: 5}},
			},
			providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
		}
		for i := range repo.providers {
			repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
		}

		cache := &mockGatewayCacheForPlatform{}

		groupRepo := &mockGroupRepoForGateway{
			groups: map[int64]*routing.Group{
				groupID: {
					ID: groupID,

					Status:              billing.StatusActive,
					Hydrated:            true,
					ModelRoutingEnabled: true,
					ModelRouting: map[string][]int64{
						"claude-3-5-sonnet-20241022": {1, 2},
					},
				},
			},
		}

		cfg := testConfig()
		cfg.Gateway.Scheduling.LoadBatchEnabled = true

		concurrencyCache := &mockConcurrencyCache{
			loadMap: map[int64]*scheduler.ProviderLoadInfo{
				1: {ProviderID: 1, LoadRate: 80},
				2: {ProviderID: 2, LoadRate: 20},
			},
		}

		svc := newGenericSelectionForTest(GenericDependencies{
			Reads: Reads{Providers: repo, Groups: groupRepo},
			Shared: Shared{
				Concurrency: scheduler.NewConcurrencyService(concurrencyCache,

					scheduler.Diagnostics{
						Logf:  logging.LegacyPrintf,
						Event: logging.Event,
					}),
				Cache: cache,
			},
		}, cfg)

		result, err := svc.SelectProviderWithLoadAwareness(ctx, &groupID, "route", "claude-3-5-sonnet-20241022", nil, "", int64(0))
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, result.Provider)
		require.Equal(t, int64(2), result.Provider.Record.ID)
		require.Equal(t, int64(2), cache.sessionBindings["route"])
	})

	t.Run("模型路由-路由提供商全满返回等待计划", func(t *testing.T) {
		groupID := int64(23)

		repo := &mockProviderRepoForPlatform{
			providers: []gatewayprovider.ExecutionProvider{
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true, Concurrency: 5}},
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true, Concurrency: 5}},
			},
			providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
		}
		for i := range repo.providers {
			repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
		}

		cache := &mockGatewayCacheForPlatform{}

		groupRepo := &mockGroupRepoForGateway{
			groups: map[int64]*routing.Group{
				groupID: {
					ID: groupID,

					Status:              billing.StatusActive,
					Hydrated:            true,
					ModelRoutingEnabled: true,
					ModelRouting: map[string][]int64{
						"claude-3-5-sonnet-20241022": {1, 2},
					},
				},
			},
		}

		cfg := testConfig()
		cfg.Gateway.Scheduling.LoadBatchEnabled = true

		concurrencyCache := &mockConcurrencyCache{
			acquireResults: map[int64]bool{1: false, 2: false},
			loadMap: map[int64]*scheduler.ProviderLoadInfo{
				1: {ProviderID: 1, LoadRate: 10},
				2: {ProviderID: 2, LoadRate: 20},
			},
		}

		svc := newGenericSelectionForTest(GenericDependencies{
			Reads: Reads{Providers: repo, Groups: groupRepo},
			Shared: Shared{
				Concurrency: scheduler.NewConcurrencyService(concurrencyCache,

					scheduler.Diagnostics{
						Logf:  logging.LegacyPrintf,
						Event: logging.Event,
					}),
				Cache: cache,
			},
		}, cfg)

		result, err := svc.SelectProviderWithLoadAwareness(ctx, &groupID, "route-full", "claude-3-5-sonnet-20241022", nil, "", int64(0))
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, result.WaitPlan)
		require.Equal(t, int64(1), result.Provider.Record.ID)
	})

	t.Run("模型路由-路由提供商全满-回退普通选择", func(t *testing.T) {
		groupID := int64(22)

		repo := &mockProviderRepoForPlatform{
			providers: []gatewayprovider.ExecutionProvider{
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true, Concurrency: 5}},
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformAnthropic, Priority: 2, Status: billing.StatusActive, Schedulable: true, Concurrency: 5}},
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 3, Platform: capability.PlatformAnthropic, Priority: 0, Status: billing.StatusActive, Schedulable: true, Concurrency: 5}},
			},
			providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
		}
		for i := range repo.providers {
			repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
		}

		cache := &mockGatewayCacheForPlatform{}

		groupRepo := &mockGroupRepoForGateway{
			groups: map[int64]*routing.Group{
				groupID: {
					ID: groupID,

					Status:              billing.StatusActive,
					Hydrated:            true,
					ModelRoutingEnabled: true,
					ModelRouting: map[string][]int64{
						"claude-3-5-sonnet-20241022": {1, 2},
					},
				},
			},
		}

		cfg := testConfig()
		cfg.Gateway.Scheduling.LoadBatchEnabled = true

		concurrencyCache := &mockConcurrencyCache{
			loadMap: map[int64]*scheduler.ProviderLoadInfo{
				1: {ProviderID: 1, LoadRate: 100},
				2: {ProviderID: 2, LoadRate: 100},
				3: {ProviderID: 3, LoadRate: 0},
			},
		}

		svc := newGenericSelectionForTest(GenericDependencies{
			Reads: Reads{Providers: repo, Groups: groupRepo},
			Shared: Shared{
				Cache: cache,
				Concurrency: scheduler.NewConcurrencyService(
					concurrencyCache, scheduler.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
			},
		}, cfg)

		result, err := svc.SelectProviderWithLoadAwareness(ctx, &groupID, "fallback", "claude-3-5-sonnet-20241022", nil, "", int64(0))
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, result.Provider)
		require.Equal(t, int64(3), result.Provider.Record.ID)
		require.Equal(t, int64(3), cache.sessionBindings["fallback"])
	})

	t.Run("负载批量失败且无法获取-兜底等待", func(t *testing.T) {
		repo := &mockProviderRepoForPlatform{
			providers: []gatewayprovider.ExecutionProvider{
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true, Concurrency: 5}},
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformAnthropic, Priority: 2, Status: billing.StatusActive, Schedulable: true, Concurrency: 5}},
			},
			providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
		}
		for i := range repo.providers {
			repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
		}

		cache := &mockGatewayCacheForPlatform{}

		cfg := testConfig()
		cfg.Gateway.Scheduling.LoadBatchEnabled = true

		concurrencyCache := &mockConcurrencyCache{
			loadBatchErr:   errors.New("load batch failed"),
			acquireResults: map[int64]bool{1: false, 2: false},
		}

		svc := newGenericSelectionForTest(GenericDependencies{
			Reads: Reads{Providers: repo},
			Shared: Shared{
				Cache: cache,
				Concurrency: scheduler.NewConcurrencyService(concurrencyCache,

					scheduler.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
			},
		}, cfg)

		result, err := svc.SelectProviderWithLoadAwareness(ctx, selectionFixtureGroupID(ctx), "", "claude-3-5-sonnet-20241022", nil, "", int64(0))
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, result.WaitPlan)
		require.Equal(t, int64(1), result.Provider.Record.ID)
	})

	t.Run("跨平台基础排序-同优先级候选均可调度", func(t *testing.T) {
		groupID := int64(24)

		repo := &mockProviderRepoForPlatform{
			providers: []gatewayprovider.ExecutionProvider{
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformGemini, Priority: 1, Status: billing.StatusActive, Schedulable: true, Concurrency: 5, Type: capability.ProviderTypeAPIKey}},
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformGemini, Priority: 1, Status: billing.StatusActive, Schedulable: true, Concurrency: 5, Type: capability.ProviderTypeOAuth}},
			},
			providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
		}
		for i := range repo.providers {
			repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
		}

		cache := &mockGatewayCacheForPlatform{}

		groupRepo := &mockGroupRepoForGateway{
			groups: map[int64]*routing.Group{
				groupID: {
					ID: groupID,

					Status:   billing.StatusActive,
					Hydrated: true,
				},
			},
		}

		cfg := testConfig()
		cfg.Gateway.Scheduling.LoadBatchEnabled = true

		concurrencyCache := &mockConcurrencyCache{
			loadMap: map[int64]*scheduler.ProviderLoadInfo{
				1: {ProviderID: 1, LoadRate: 10},
				2: {ProviderID: 2, LoadRate: 10},
			},
		}

		svc := newGenericSelectionForTest(GenericDependencies{
			Reads: Reads{Providers: repo, Groups: groupRepo},
			Shared: Shared{
				Cache: cache,
				Concurrency: scheduler.NewConcurrencyService(
					concurrencyCache, scheduler.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
			},
		}, cfg)

		result, err := svc.SelectProviderWithLoadAwareness(ctx, &groupID, "gemini", "gemini-2.5-pro", nil, "", int64(0))
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, result.Provider)
		require.Contains(t, []int64{1, 2}, result.Provider.Record.ID)
	})

	t.Run("模型路由-过滤路径覆盖", func(t *testing.T) {
		groupID := int64(70)
		now := time.Now().Add(10 * time.Minute)
		repo := &mockProviderRepoForPlatform{
			providers: []gatewayprovider.ExecutionProvider{
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true, Concurrency: 5}},
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 3, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: false, Concurrency: 5}},
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 4, Platform: capability.PlatformAntigravity, Priority: 1, Status: billing.StatusActive, Schedulable: true, Concurrency: 5}},
				{
					Record: providercore.Record{
						Credentials:  map[string]any{"model_whitelist": []string{"*"}},
						LoadLocation: time.LoadLocation, ID: 5,
						Platform:    capability.PlatformAnthropic,
						Priority:    1,
						Status:      billing.StatusActive,
						Schedulable: true,
						Concurrency: 5,
						Extra: map[string]any{
							"model_rate_limits": map[string]any{
								"claude-3-5-sonnet-20241022": map[string]any{
									"rate_limit_reset_at": now.Format(time.RFC3339),
								},
							},
						},
					},
				},
				{
					Record: providercore.Record{
						LoadLocation: time.LoadLocation, ID: 6,
						Platform:    capability.PlatformAnthropic,
						Priority:    1,
						Status:      billing.StatusActive,
						Schedulable: true,
						Concurrency: 5,
						Credentials: map[string]any{"model_mapping": map[string]any{"claude-3-5-haiku-20241022": "claude-3-5-haiku-20241022"}},
					},
				},
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 7, Platform: capability.PlatformAnthropic, Priority: 2, Status: billing.StatusActive, Schedulable: true, Concurrency: 5}},
			},
			providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
		}
		for i := range repo.providers {
			repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
		}

		cache := &mockGatewayCacheForPlatform{}

		groupRepo := &mockGroupRepoForGateway{
			groups: map[int64]*routing.Group{
				groupID: {
					ID: groupID,

					Status:              billing.StatusActive,
					Hydrated:            true,
					ModelRoutingEnabled: true,
					ModelRouting: map[string][]int64{
						"claude-3-5-sonnet-20241022": {1, 2, 3, 4, 5, 6},
					},
				},
			},
		}

		cfg := testConfig()
		cfg.Gateway.Scheduling.LoadBatchEnabled = true

		concurrencyCache := &mockConcurrencyCache{}

		svc := newGenericSelectionForTest(GenericDependencies{
			Reads: Reads{Providers: repo, Groups: groupRepo},
			Shared: Shared{
				Cache: cache,
				Concurrency: scheduler.NewConcurrencyService(
					concurrencyCache, scheduler.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
			},
		}, cfg)

		excluded := map[int64]struct{}{1: {}}
		result, err := svc.SelectProviderWithLoadAwareness(ctx, &groupID, "", "claude-3-5-sonnet-20241022", excluded, "", int64(0))
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, result.Provider)
		require.Equal(t, int64(4), result.Provider.Record.ID)
	})

	t.Run("ClaudeCode限制-入口已授权回退分组", func(t *testing.T) {
		groupID := int64(60)
		fallbackID := int64(61)

		repo := &mockProviderRepoForPlatform{
			providers: []gatewayprovider.ExecutionProvider{
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformGemini, Priority: 1, Status: billing.StatusActive, Schedulable: true}},
			},
			providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
		}
		for i := range repo.providers {
			repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
		}

		groupRepo := &mockGroupRepoForGateway{
			groups: map[int64]*routing.Group{
				groupID: {
					ID: groupID,

					Status:         billing.StatusActive,
					Hydrated:       true,
					ClaudeCodeOnly: true,
					FallbackGroupID: func() *int64 {
						v := fallbackID
						return &v
					}(),
				},
				fallbackID: {
					ID: fallbackID,

					Status:   billing.StatusActive,
					Hydrated: true,
				},
			},
		}

		cfg := testConfig()
		cfg.Gateway.Scheduling.LoadBatchEnabled = false

		svc := newGenericSelectionForTest(GenericDependencies{
			Reads:  Reads{Providers: repo, Groups: groupRepo},
			Shared: Shared{Cache: &mockGatewayCacheForPlatform{}, Concurrency: nil},
		},

			cfg)

		admitted := requeststate.WithGroup(ctx, groupRepo.groups[fallbackID])
		admitted = requeststate.WithRoutePlan(admitted, routing.Plan(routing.PlanInput{Group: groupRepo.groups[fallbackID]}))
		result, err := svc.SelectProviderWithLoadAwareness(admitted, &fallbackID, "", "gemini-2.5-pro", nil, "", int64(0))
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, result.Provider)
		require.Equal(t, int64(1), result.Provider.Record.ID)
	})

	t.Run("ClaudeCode限制-无降级返回错误", func(t *testing.T) {
		groupID := int64(62)

		groupRepo := &mockGroupRepoForGateway{
			groups: map[int64]*routing.Group{
				groupID: {
					ID: groupID,

					Status:         billing.StatusActive,
					Hydrated:       true,
					ClaudeCodeOnly: true,
				},
			},
		}

		cfg := testConfig()
		cfg.Gateway.Scheduling.LoadBatchEnabled = false

		svc := newGenericSelectionForTest(GenericDependencies{
			Reads:  Reads{Providers: &mockProviderRepoForPlatform{}, Groups: groupRepo},
			Shared: Shared{Cache: &mockGatewayCacheForPlatform{}, Concurrency: nil},
		}, cfg)

		result, err := svc.SelectProviderWithLoadAwareness(ctx, &groupID, "", "claude-3-5-sonnet-20241022", nil, "", int64(0))
		require.Error(t, err)
		require.Nil(t, result)
		require.ErrorIs(t, err, routing.ErrClaudeCodeOnly)
	})

	t.Run("负载可用但无法获取槽位-兜底等待", func(t *testing.T) {
		repo := &mockProviderRepoForPlatform{
			providers: []gatewayprovider.ExecutionProvider{
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true, Concurrency: 5}},
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformAnthropic, Priority: 2, Status: billing.StatusActive, Schedulable: true, Concurrency: 5}},
			},
			providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
		}
		for i := range repo.providers {
			repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
		}

		cache := &mockGatewayCacheForPlatform{}

		cfg := testConfig()
		cfg.Gateway.Scheduling.LoadBatchEnabled = true

		concurrencyCache := &mockConcurrencyCache{
			acquireResults: map[int64]bool{1: false, 2: false},
			loadMap: map[int64]*scheduler.ProviderLoadInfo{
				1: {ProviderID: 1, LoadRate: 10},
				2: {ProviderID: 2, LoadRate: 20},
			},
		}

		svc := newGenericSelectionForTest(GenericDependencies{
			Reads: Reads{Providers: repo},
			Shared: Shared{
				Cache: cache,
				Concurrency: scheduler.NewConcurrencyService(concurrencyCache,

					scheduler.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
			},
		}, cfg)

		result, err := svc.SelectProviderWithLoadAwareness(ctx, selectionFixtureGroupID(ctx), "wait", "claude-3-5-sonnet-20241022", nil, "", int64(0))
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, result.WaitPlan)
		require.Equal(t, int64(1), result.Provider.Record.ID)
	})

	t.Run("负载信息缺失-使用默认负载", func(t *testing.T) {
		repo := &mockProviderRepoForPlatform{
			providers: []gatewayprovider.ExecutionProvider{
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true, Concurrency: 5}},
				{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 2, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true, Concurrency: 5}},
			},
			providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
		}
		for i := range repo.providers {
			repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
		}

		cache := &mockGatewayCacheForPlatform{}

		cfg := testConfig()
		cfg.Gateway.Scheduling.LoadBatchEnabled = true

		concurrencyCache := &mockConcurrencyCache{
			loadMap: map[int64]*scheduler.ProviderLoadInfo{
				1: {ProviderID: 1, LoadRate: 50},
			},
			skipDefaultLoad: true,
		}

		svc := newGenericSelectionForTest(GenericDependencies{
			Reads: Reads{Providers: repo},
			Shared: Shared{
				Cache: cache,
				Concurrency: scheduler.NewConcurrencyService(concurrencyCache,

					scheduler.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}),
			},
		}, cfg)

		result, err := svc.SelectProviderWithLoadAwareness(ctx, selectionFixtureGroupID(ctx), "missing-load", "claude-3-5-sonnet-20241022", nil, "", int64(0))
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, result.Provider)
		require.Equal(t, int64(2), result.Provider.Record.ID)
	})
}

func TestGatewayService_GroupResolution_ReusesContextGroup(t *testing.T) {
	ctx := context.Background()
	groupID := int64(42)
	group := &routing.Group{
		ID: groupID,

		Status:   billing.StatusActive,
		Hydrated: true,
	}
	ctx = requeststate.WithGroup(ctx, group)

	repo := &mockProviderRepoForPlatform{
		providers: []gatewayprovider.ExecutionProvider{
			{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true}},
		},
		providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
	}
	for i := range repo.providers {
		repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
	}

	groupRepo := &mockGroupRepoForGateway{
		groups: map[int64]*routing.Group{groupID: group},
	}

	svc := newGenericSelectionForTest(GenericDependencies{Reads: Reads{Providers: repo, Groups: groupRepo}, Shared: Shared{}}, testConfig())

	provider, err := svc.SelectProviderForModelWithExclusions(ctx, &groupID, "", "claude-3-5-sonnet-20241022", nil)
	require.NoError(t, err)
	require.NotNil(t, provider)
	require.Equal(t, 1, groupRepo.getByIDCalls) // +1 for require_privacy_set check
	require.Equal(t, 0, groupRepo.getByIDLiteCalls)
}

func TestGatewayService_GroupResolution_IgnoresInvalidContextGroup(t *testing.T) {
	ctx := context.Background()
	groupID := int64(42)
	ctxGroup := &routing.Group{
		ID: groupID,

		Status: billing.StatusActive,
	}
	ctx = requeststate.WithGroup(ctx, ctxGroup)

	repo := &mockProviderRepoForPlatform{
		providers: []gatewayprovider.ExecutionProvider{
			{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true}},
		},
		providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
	}
	for i := range repo.providers {
		repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
	}

	group := &routing.Group{
		ID: groupID,

		Status:   billing.StatusActive,
		Hydrated: true,
	}
	groupRepo := &mockGroupRepoForGateway{
		groups: map[int64]*routing.Group{groupID: group},
	}

	svc := newGenericSelectionForTest(GenericDependencies{Reads: Reads{Providers: repo, Groups: groupRepo}, Shared: Shared{}}, testConfig())

	provider, err := svc.SelectProviderForModelWithExclusions(ctx, &groupID, "", "claude-3-5-sonnet-20241022", nil)
	require.NoError(t, err)
	require.NotNil(t, provider)
	require.Equal(t, 1, groupRepo.getByIDCalls) // +1 for require_privacy_set check
	require.Equal(t, 1, groupRepo.getByIDLiteCalls)
}

func TestGatewayService_GroupContext_OverwritesInvalidContextGroup(t *testing.T) {
	groupID := int64(42)
	invalidGroup := &routing.Group{
		ID: groupID,

		Status: billing.StatusActive,
	}
	hydratedGroup := &routing.Group{
		ID: groupID,

		Status:   billing.StatusActive,
		Hydrated: true,
	}

	ctx := requeststate.WithGroup(context.Background(), invalidGroup)
	svc := newGenericSelectionForTest(GenericDependencies{Reads: Reads{}, Shared: Shared{}}, nil)

	ctx = svc.withGroupContext(ctx, hydratedGroup)

	got, ok := requeststate.GroupFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, hydratedGroup, got)
	require.NotSame(t, hydratedGroup, got, "分组状态保存独立快照")
}

func TestGatewayService_GroupResolution_RejectsImplicitFallback(t *testing.T) {
	ctx := context.Background()
	groupID := int64(10)
	fallbackID := int64(11)
	group := &routing.Group{
		ID: groupID,

		Status:          billing.StatusActive,
		ClaudeCodeOnly:  true,
		FallbackGroupID: &fallbackID,
		Hydrated:        true,
	}
	fallbackGroup := &routing.Group{
		ID: fallbackID,

		Status:   billing.StatusActive,
		Hydrated: true,
	}
	ctx = requeststate.WithGroup(ctx, group)

	repo := &mockProviderRepoForPlatform{
		providers: []gatewayprovider.ExecutionProvider{
			{Record: providercore.Record{Credentials: map[string]any{"model_whitelist": []string{"*"}}, LoadLocation: time.LoadLocation, ID: 1, Platform: capability.PlatformAnthropic, Priority: 1, Status: billing.StatusActive, Schedulable: true}},
		},
		providersByID: map[int64]*gatewayprovider.ExecutionProvider{},
	}
	for i := range repo.providers {
		repo.providersByID[repo.providers[i].Record.ID] = &repo.providers[i]
	}

	groupRepo := &mockGroupRepoForGateway{
		groups: map[int64]*routing.Group{fallbackID: fallbackGroup},
	}

	svc := newGenericSelectionForTest(GenericDependencies{Reads: Reads{Providers: repo, Groups: groupRepo}, Shared: Shared{}}, testConfig())

	provider, err := svc.SelectProviderForModelWithExclusions(ctx, &groupID, "", "claude-3-5-sonnet-20241022", nil)
	require.ErrorIs(t, err, routing.ErrClaudeCodeOnly)
	require.Nil(t, provider)
	// 回退目标还未准入，不读取它的策略或提供商池。
	require.Zero(t, groupRepo.getByIDCalls)
	require.Zero(t, groupRepo.getByIDLiteCalls)
}
