package catalogue_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/provider"

	"github.com/TokenFlux/TokenRouter/internal/routing"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"

	gocache "github.com/patrickmn/go-cache"
	"github.com/stretchr/testify/require"
)

func TestGetAvailableModels_UsesShortCacheAndSupportsInvalidation(t *testing.T) {
	resetModelListMetrics()

	groupID := int64(9)
	repo := &modelsListProviderRepoStub{
		byGroup: map[int64][]provider.Record{
			groupID: {
				{
					ID:       1,
					Platform: capability.PlatformAnthropic,
					Credentials: map[string]any{
						"model_whitelist": []string{"claude-3-5-sonnet", "claude-3-5-haiku"},
						"model_mapping": map[string]any{
							"claude-3-5-sonnet": "claude-3-5-sonnet",
							"claude-3-5-haiku":  "claude-3-5-haiku",
						},
					},
				},
				{
					ID:       2,
					Platform: capability.PlatformGemini,
					Credentials: map[string]any{
						"model_whitelist": []string{"gemini-2.5-pro"},
						"model_mapping": map[string]any{
							"gemini-2.5-pro": "gemini-2.5-pro",
						},
					},
				},
			},
		},
	}

	svc := newModelListFixture(repo)

	models1 := svc.Available(context.Background(), &groupID, capability.PlatformAnthropic)
	require.Equal(t, []string{"claude-3-5-haiku", "claude-3-5-sonnet"}, models1)
	require.Equal(t, int64(1), repo.listByGroupCalls.Load())

	// TTL 内再次请求应命中缓存，不回源。
	models2 := svc.Available(context.Background(), &groupID, capability.PlatformAnthropic)
	require.Equal(t, models1, models2)
	require.Equal(t, int64(1), repo.listByGroupCalls.Load())

	// 更新仓储数据，但缓存未失效前应继续返回旧值。
	repo.byGroup[groupID] = []provider.Record{
		{
			ID:       3,
			Platform: capability.PlatformAnthropic,
			Credentials: map[string]any{
				"model_whitelist": []string{"claude-3-7-sonnet"},
				"model_mapping": map[string]any{
					"claude-3-7-sonnet": "claude-3-7-sonnet",
				},
			},
		},
	}
	models3 := svc.Available(context.Background(), &groupID, capability.PlatformAnthropic)
	require.Equal(t, []string{"claude-3-5-haiku", "claude-3-5-sonnet"}, models3)
	require.Equal(t, int64(1), repo.listByGroupCalls.Load())

	svc.Invalidate(&groupID, capability.PlatformAnthropic)
	models4 := svc.Available(context.Background(), &groupID, capability.PlatformAnthropic)
	require.Equal(t, []string{"claude-3-7-sonnet"}, models4)
	require.Equal(t, int64(2), repo.listByGroupCalls.Load())

	metrics := routing.SharedModelListMetrics()
	hit, miss, store := metrics.Hit.Load(), metrics.Miss.Load(), metrics.Store.Load()
	require.Equal(t, int64(2), hit)
	require.Equal(t, int64(2), miss)
	require.Equal(t, int64(2), store)
}

func TestGetAvailableModels_ErrorAndGlobalListBranches(t *testing.T) {
	resetModelListMetrics()

	errRepo := &modelsListProviderRepoStub{
		err: errors.New("db error"),
	}
	svcErr := newModelListFixture(errRepo)
	require.Nil(t, svcErr.Available(context.Background(), nil, ""))

	okRepo := &modelsListProviderRepoStub{
		all: []provider.Record{
			{
				ID:       1,
				Platform: capability.PlatformAnthropic,
				Credentials: map[string]any{
					"model_whitelist": []string{"claude-3-5-sonnet"},
					"model_mapping": map[string]any{
						"claude-3-5-sonnet": "claude-3-5-sonnet",
					},
				},
			},
			{
				ID:       2,
				Platform: capability.PlatformGemini,
				Credentials: map[string]any{
					"model_whitelist": []string{"gemini-2.5-pro"},
					"model_mapping": map[string]any{
						"gemini-2.5-pro": "gemini-2.5-pro",
					},
				},
			},
		},
	}
	svcOK := newModelListFixture(okRepo)
	models := svcOK.Available(context.Background(), nil, "")
	require.Equal(t, []string{"claude-3-5-sonnet", "gemini-2.5-pro"}, models)
	require.Equal(t, int64(1), okRepo.listAllCalls.Load())
}

// TestGetAvailableModelsPassthroughPreservesExplicitScope 验证透传只改变传输，显式白名单及映射在目录聚合时仍生效。
func TestGetAvailableModelsPassthroughPreservesExplicitScope(t *testing.T) {
	groupID := int64(10)
	first := provider.Record{ID: 1, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey, Credentials: map[string]any{
		"model_mapping": map[string]any{"custom-alias": "custom-upstream"}, "model_whitelist": []string{"custom-upstream"},
	}, Extra: map[string]any{"openai_passthrough": true}}
	second := provider.Record{ID: 2, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey, Credentials: map[string]any{
		"model_mapping": map[string]any{"other-alias": "other-upstream"}, "model_whitelist": []string{"other-upstream"},
	}}
	repo := &modelsListProviderRepoStub{byGroup: map[int64][]provider.Record{groupID: {first, second}}}
	models := newModelListFixture(repo).Available(context.Background(), &groupID, capability.PlatformOpenAI)
	require.ElementsMatch(t, []string{"custom-alias", "custom-upstream", "other-alias", "other-upstream"}, models)
}

func TestGetAvailableModels_GlobalListPreservesMappedModelsWithOpenAIPassthrough(t *testing.T) {
	groupID := int64(11)
	repo := &modelsListProviderRepoStub{
		byGroup: map[int64][]provider.Record{
			groupID: {
				{
					ID:       1,
					Platform: capability.PlatformOpenAI,
					Extra:    map[string]any{"openai_passthrough": true},
				},
				{
					ID:          2,
					Platform:    capability.PlatformAnthropic,
					Credentials: map[string]any{"model_mapping": map[string]any{"claude-mapped": "claude-mapped"}},
				},
			},
		},
	}
	svc := newModelListFixture(repo)

	models := svc.Available(context.Background(), &groupID, "")
	require.Contains(t, models, "claude-mapped")
	require.Contains(t, models, "gpt-5.6-sol")
	require.NotContains(t, models, "unknown-model")
}

func TestInvalidateAvailableModelsCache_ByDimensions(t *testing.T) {
	svc := &routing.ModelList{Cache: gocache.New(time.Minute, time.Minute)}
	group9 := int64(9)
	group10 := int64(10)
	svc.Cache.Set(routing.ModelListCacheKey(&group9, capability.PlatformAnthropic), []string{"a"}, time.Minute)
	svc.Cache.Set(routing.ModelListCacheKey(&group9, capability.PlatformGemini), []string{"b"}, time.Minute)
	svc.Cache.Set(routing.ModelListCacheKey(&group10, capability.PlatformAnthropic), []string{"c"}, time.Minute)
	svc.Cache.Set("invalid-key", []string{"d"}, time.Minute)

	t.Run("invalidate_group_and_platform", func(t *testing.T) {
		svc.Invalidate(&group9, capability.PlatformAnthropic)
		_, found := svc.Cache.Get(routing.ModelListCacheKey(&group9, capability.PlatformAnthropic))
		require.False(t, found)
		_, stillFound := svc.Cache.Get(routing.ModelListCacheKey(&group9, capability.PlatformGemini))
		require.True(t, stillFound)
	})

	t.Run("invalidate_group_only", func(t *testing.T) {
		svc.Invalidate(&group9, "")
		_, foundA := svc.Cache.Get(routing.ModelListCacheKey(&group9, capability.PlatformAnthropic))
		_, foundB := svc.Cache.Get(routing.ModelListCacheKey(&group9, capability.PlatformGemini))
		require.False(t, foundA)
		require.False(t, foundB)
		_, foundOtherGroup := svc.Cache.Get(routing.ModelListCacheKey(&group10, capability.PlatformAnthropic))
		require.True(t, foundOtherGroup)
	})

	t.Run("invalidate_platform_only", func(t *testing.T) {
		// 重建数据后仅按 platform 失效
		svc.Cache.Set(routing.ModelListCacheKey(&group9, capability.PlatformAnthropic), []string{"a"}, time.Minute)
		svc.Cache.Set(routing.ModelListCacheKey(&group9, capability.PlatformGemini), []string{"b"}, time.Minute)
		svc.Cache.Set(routing.ModelListCacheKey(&group10, capability.PlatformAnthropic), []string{"c"}, time.Minute)

		svc.Invalidate(nil, capability.PlatformAnthropic)
		_, found9Anthropic := svc.Cache.Get(routing.ModelListCacheKey(&group9, capability.PlatformAnthropic))
		_, found10Anthropic := svc.Cache.Get(routing.ModelListCacheKey(&group10, capability.PlatformAnthropic))
		_, found9Gemini := svc.Cache.Get(routing.ModelListCacheKey(&group9, capability.PlatformGemini))
		require.False(t, found9Anthropic)
		require.False(t, found10Anthropic)
		require.True(t, found9Gemini)
	})
}

// newModelListFixture 使用提供商查询夹具创建模型列表，缓存 TTL 为一分钟。
func newModelListFixture(rows catalogueRows) *routing.ModelList {
	catalogue := newCatalogueFixture(rows, nil, nil)
	return routing.NewModelList(catalogue.Read, time.Minute)
}

// resetModelListMetrics 重置进程级模型列表指标，避免用例之间相互影响。
func resetModelListMetrics() {
	metrics := routing.SharedModelListMetrics()
	metrics.Hit.Store(0)
	metrics.Miss.Store(0)
	metrics.Store.Store(0)
}
