package provider

import (
	"context"
	"testing"
	"time"

	provider "github.com/TokenFlux/TokenRouter/internal/provider"
	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

// TestSparkShadowIntegration 检查 Spark 影子的令牌读取、模型资格和母提供商健康限制。
// 母提供商轮换令牌后，影子读取新值。模型资格由 model_mapping 决定。
// 母提供商 Status=error 时影子不可用，手动暂停或全局限流时影子仍可使用其凭据。
func TestSparkShadowIntegration(t *testing.T) {
	ctx := context.Background()
	pid := int64(100)

	// 共享母提供商：Credentials 为 map（引用型），可原地轮换而无需重建 stub。
	parent := &ExecutionProvider{
		Record: provider.Record{
			LoadLocation: time.LoadLocation, ID: 100,
			Platform:    capability.PlatformOpenAI,
			Type:        capability.ProviderTypeOAuth,
			Status:      billing.StatusActive,
			Schedulable: true,
			Credentials: map[string]any{
				"access_token": "T1",
			},
		},
	}
	// 影子提供商通过母提供商读取凭据，QuotaDimensionSpark 标记 spark 维度。
	shadow := &ExecutionProvider{
		Record: provider.Record{
			LoadLocation: time.LoadLocation, ID: 200,
			Platform:         capability.PlatformOpenAI,
			Type:             capability.ProviderTypeOAuth,
			ParentProviderID: &pid,
			QuotaDimension:   provider.QuotaDimensionSpark,
			Status:           billing.StatusActive,
			Schedulable:      true,
		},
	}

	// repo：stubCredRepo（credential_shadow_test.go）保存执行目标指针，
	// Credentials map 变更直接可见，无需重建 stub。
	repo := newStubCredRepo(parent)
	credentials := &provider.OpenAIExecutionCredentials{Parent: func(ctx context.Context, id int64) (*provider.Record, error) {
		value, err := repo.GetByID(ctx, id)
		return ExecutionRecord(value), err
	}}

	// ──────────────────────────────────────────────────────────────────────
	// 属性 1：凭据轮换读透（脱钩命门）
	// ──────────────────────────────────────────────────────────────────────

	t.Run("credential_readthrough_initial_T1", func(t *testing.T) {
		// 影子无凭据，resolveCredentialProvider 必须透传到母提供商。
		got, err := CredentialProvider(ctx, repo, shadow)
		require.NoError(t, err)
		require.Equal(t, int64(100), got.Record.ID, "解析结果应为母提供商")
		require.Equal(t, "T1", got.View().GetOpenAIAccessToken(),
			"初始应读到 T1")
	})

	t.Run("credential_readthrough_after_rotation_T2", func(t *testing.T) {
		// 模拟 refresh_token 轮换：原地更新母提供商凭据。
		// 影子不持凭据、无本地缓存，下次解析必须见到新值。
		parent.Record.Credentials["access_token"] = "T2"

		got, err := CredentialProvider(ctx, repo, shadow)
		require.NoError(t, err)
		require.Equal(t, "T2", got.View().GetOpenAIAccessToken(),
			"轮换后影子必须立即反映母提供商新 token（零脱钩）")
	})

	t.Run("get_access_token_e2e_reads_through_T3", func(t *testing.T) {
		// 经凭据源 Resolve 检查影子读取到的轮换后令牌。
		// openAITokenProvider=nil → 降级到直接读 provider.GetOpenAIAccessToken()。
		parent.Record.Credentials["access_token"] = "T3"
		token, tokenType, err := credentials.Resolve(ctx, ExecutionRecord(shadow))
		require.NoError(t, err)
		require.Equal(t, "T3", token,
			"Resolve(影子) 必须返回母提供商当前 token")
		require.Equal(t, "oauth", tokenType)
	})

	t.Run("normal_provider_returns_its_own_token", func(t *testing.T) {
		// 对照组：普通提供商（非影子）直接返回自身凭据，不经 resolveCredentialProvider。
		ordinary := &ExecutionProvider{
			Record: provider.Record{
				LoadLocation: time.LoadLocation, ID: 300,
				Platform:    capability.PlatformOpenAI,
				Type:        capability.ProviderTypeOAuth,
				Status:      billing.StatusActive,
				Schedulable: true,
				Credentials: map[string]any{
					"access_token": "ordinary-token",
				},
			},
		}
		token, _, err := credentials.Resolve(ctx, ExecutionRecord(ordinary))
		require.NoError(t, err)
		require.Equal(t, "ordinary-token", token)
	})

	// ──────────────────────────────────────────────────────────────────────
	// 属性 2：路由不变量（路由资格由 IsModelSupported 决定）
	// ──────────────────────────────────────────────────────────────────────

	t.Run("routing_invariant", func(t *testing.T) {
		// 路由资格已从「按提供商类型」改为「按提供商支持模型」(model_mapping / IsModelSupported)。
		sparkModel := "gpt-5.3-codex-spark"
		normalModel := "gpt-5.3-codex"
		sparkCreds := map[string]any{"model_mapping": provideradapter.DefaultSparkShadowModels()}

		pid := int64(1)
		sparkShadow := &ExecutionProvider{Record: provider.Record{LoadLocation: time.LoadLocation, ID: 2, ParentProviderID: &pid, Platform: capability.PlatformOpenAI, Credentials: sparkCreds}}
		require.True(t, ExecutionProtocolRecord(sparkShadow).IsModelSupported(sparkModel, provideradapter.ModelDefaults(), provideradapter.ModelRules(ExecutionProtocolRecord(sparkShadow))), "影子配 spark → 接 spark")
		require.False(t, ExecutionProtocolRecord(sparkShadow).IsModelSupported(normalModel, provideradapter.ModelDefaults(), provideradapter.ModelRules(ExecutionProtocolRecord(sparkShadow))), "影子（仅 spark mapping）→ 拒非 spark")

		normalWithSpark := &ExecutionProvider{Record: provider.Record{LoadLocation: time.LoadLocation, ID: 3, Platform: capability.PlatformOpenAI, Credentials: sparkCreds}}
		require.True(t, ExecutionProtocolRecord(normalWithSpark).IsModelSupported(sparkModel, provideradapter.ModelDefaults(), provideradapter.ModelRules(ExecutionProtocolRecord(normalWithSpark))), "普通提供商配 spark → 接 spark（不再按类型排除）")

		normalNoSpark := &ExecutionProvider{Record: provider.Record{
			LoadLocation: time.LoadLocation, ID: 4, Platform: capability.PlatformOpenAI,
			Credentials: map[string]any{"model_whitelist": []string{normalModel}, "model_mapping": map[string]any{normalModel: normalModel}},
		}}
		require.False(t, ExecutionProtocolRecord(normalNoSpark).IsModelSupported(sparkModel, provideradapter.ModelDefaults(), provideradapter.ModelRules(ExecutionProtocolRecord(normalNoSpark))), "普通提供商显式白名单不含 Spark 时拒绝")
	})

	// ──────────────────────────────────────────────────────────────────────
	// 属性 3：母提供商健康度联动（parentHealthyForShadow）
	// ──────────────────────────────────────────────────────────────────────

	t.Run("parent_health_propagated_to_shadow", func(t *testing.T) {
		// 恢复母提供商健康状态（属性 1/2 测试可能改过）
		parent.Record.Status = billing.StatusActive
		parent.Record.Schedulable = true

		lookup := func(id int64) *ExecutionProvider {
			if id == parent.Record.ID {
				return parent
			}
			return nil
		}

		// 母健康 → 影子健康
		require.True(t, provider.ParentHealthyForShadow(ExecutionRecord(shadow), func(id int64) *provider.Record {
			return ExecutionRecord(lookup(id))
		}), "健康母提供商时影子应健康")

		// 母 Status=error(凭据不可用)→ 影子不健康
		parent.Record.Status = provider.StatusError
		require.False(t, provider.ParentHealthyForShadow(ExecutionRecord(shadow), func(id int64) *provider.Record {
			return ExecutionRecord(lookup(id))
		}), "Status=error 母提供商时影子应不健康")

		// F1 决策 A:母 Schedulable=false (Status=active) 是手动调度暂停,不连坐影子(凭据仍可用)
		parent.Record.Status = billing.StatusActive
		parent.Record.Schedulable = false
		require.True(t, provider.ParentHealthyForShadow(ExecutionRecord(shadow), func(id int64) *provider.Record {
			return ExecutionRecord(lookup(id))
		}), "母提供商手动暂停不应连坐影子(凭据仍可用)")

		// F1 核心:母 global 限流(RateLimitResetAt 未来)不连坐 spark 影子
		parent.Record.Schedulable = true
		resetAt := time.Now().Add(1 * time.Hour)
		parent.Record.RateLimitResetAt = &resetAt
		require.True(t, provider.ParentHealthyForShadow(ExecutionRecord(shadow), func(id int64) *provider.Record {
			return ExecutionRecord(lookup(id))
		}), "母提供商 global 限流不应连坐 spark 影子")
		parent.Record.RateLimitResetAt = nil

		// 对照组：非影子提供商 parentHealthyForShadow 始终 true，不调用 lookup
		parent.Record.Schedulable = true
		lookupNotCalled := func(_ int64) *ExecutionProvider {
			t.Error("非影子提供商不应调用 lookup")
			return nil
		}
		require.True(t, provider.ParentHealthyForShadow(ExecutionRecord(parent), func(id int64) *provider.Record {
			return ExecutionRecord(lookupNotCalled(id))
		}), "普通提供商应直接返回 true")
	})
}
