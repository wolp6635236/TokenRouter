package provider

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

// TestPropagateProviderProxyToShadows CRS/管理端 改母提供商 proxy 后,
// 影子 proxy 必须跟随(影子 proxy 恒继承母提供商,否则出站漂移)。
func TestPropagateProviderProxyToShadows(t *testing.T) {
	ctx := context.Background()
	repo := newCRSShadowStore()

	oldProxy := int64(11)
	mother := &Record{Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, ProxyID: &oldProxy}
	require.NoError(t, repo.Create(ctx, mother))
	parentID := mother.ID

	shadow := &Record{
		Platform:         capability.PlatformOpenAI,
		Type:             capability.ProviderTypeOAuth,
		ParentProviderID: &parentID,
		QuotaDimension:   QuotaDimensionSpark,
		ProxyID:          &oldProxy,
	}
	require.NoError(t, repo.Create(ctx, shadow))

	newProxy := int64(22)
	require.NoError(t, PropagateProviderProxyToShadows(ctx, repo, parentID, &newProxy))

	got, err := repo.GetByID(ctx, shadow.ID)
	require.NoError(t, err)
	require.NotNil(t, got.ProxyID)
	require.Equal(t, newProxy, *got.ProxyID, "shadow proxy must follow the parent's new proxy")

	// 清空母 proxy 也应传播为 nil。
	require.NoError(t, PropagateProviderProxyToShadows(ctx, repo, parentID, nil))
	got, err = repo.GetByID(ctx, shadow.ID)
	require.NoError(t, err)
	require.Nil(t, got.ProxyID, "clearing parent proxy must clear the shadow proxy too")
}

// TestParentHealthyForShadow 检查调度、网关选择和 WebSocket 转发使用的母提供商健康判断。
func TestParentHealthyForShadow(t *testing.T) {
	pid := int64(100)

	// 母提供商夹具统一设为 OpenAI OAuth，让每个用例只触发自身要验证的健康条件。
	healthyParent := &Record{
		ID:          100,
		Platform:    capability.PlatformOpenAI,
		Type:        capability.ProviderTypeOAuth,
		Status:      billing.StatusActive,
		Schedulable: true,
	}
	unhealthyParent := &Record{
		ID:          100,
		Platform:    capability.PlatformOpenAI,
		Type:        capability.ProviderTypeOAuth,
		Status:      StatusError,
		Schedulable: true, // Schedulable flag is set, but Status=error → IsSchedulable()==false
	}
	shadow := &Record{
		ID:               200,
		ParentProviderID: &pid,
		QuotaDimension:   QuotaDimensionSpark,
		Platform:         capability.PlatformOpenAI,
		Status:           billing.StatusActive,
		Schedulable:      true,
	}
	normalProvider := &Record{
		ID:          300,
		Platform:    capability.PlatformOpenAI,
		Status:      billing.StatusActive,
		Schedulable: true,
	}

	t.Run("shadow_of_healthy_parent_is_healthy", func(t *testing.T) {
		lookup := func(id int64) *Record {
			if id == healthyParent.ID {
				return healthyParent
			}
			return nil
		}
		require.True(t, ParentHealthyForShadow(shadow, lookup))
	})

	t.Run("shadow_of_unhealthy_parent_is_not_healthy", func(t *testing.T) {
		// 母提供商状态为 error 时，IsActive 和 IsSchedulable 均返回 false。
		require.False(t, unhealthyParent.IsSchedulable(), "precondition: unhealthy parent must not be schedulable")
		lookup := func(id int64) *Record {
			if id == unhealthyParent.ID {
				return unhealthyParent
			}
			return nil
		}
		require.False(t, ParentHealthyForShadow(shadow, lookup))
	})

	t.Run("shadow_parent_not_found_is_not_healthy", func(t *testing.T) {
		lookup := func(_ int64) *Record { return nil }
		require.False(t, ParentHealthyForShadow(shadow, lookup))
	})

	t.Run("normal_provider_always_healthy", func(t *testing.T) {
		// 普通提供商无需查询母提供商。
		calledLookup := false
		lookup := func(_ int64) *Record {
			calledLookup = true
			return nil
		}
		require.True(t, ParentHealthyForShadow(normalProvider, lookup))
		require.False(t, calledLookup, "lookup must not be called for non-shadow providers")
	})

	t.Run("nil_provider_always_healthy", func(t *testing.T) {
		lookup := func(_ int64) *Record { return nil }
		require.True(t, ParentHealthyForShadow(nil, lookup))
	})

	t.Run("manual_schedulable_false_parent_does_not_block_shadow", func(t *testing.T) {
		// 母提供商 Schedulable=false 时，影子仍按自己的 Schedulable 判断调度资格。
		// 母提供商凭据处于 active 且未过期时，影子的凭据检查通过。
		manualPausedParent := &Record{
			ID:          100,
			Platform:    capability.PlatformOpenAI,
			Type:        capability.ProviderTypeOAuth,
			Status:      billing.StatusActive,
			Schedulable: false,
		}
		require.False(t, manualPausedParent.IsSchedulable(), "precondition: 母提供商被手动暂停不可调度")
		lookup := func(id int64) *Record {
			if id == manualPausedParent.ID {
				return manualPausedParent
			}
			return nil
		}
		require.True(t, ParentHealthyForShadow(shadow, lookup),
			"母提供商手动暂停不应连坐影子(凭据仍可用)")
	})

	t.Run("global_rate_limited_parent_does_not_block_shadow", func(t *testing.T) {
		// F1 核心修复:母提供商 global 429(RateLimitResetAt 未来)是 global 维度限流,
		// spark 有独立窗口 → 不得连坐影子,否则违背「global 枯竭后 spark 仍独立」目标。
		resetAt := time.Now().Add(1 * time.Hour)
		rateLimitedParent := &Record{
			ID:               100,
			Platform:         capability.PlatformOpenAI,
			Type:             capability.ProviderTypeOAuth,
			Status:           billing.StatusActive,
			Schedulable:      true,
			RateLimitResetAt: &resetAt,
		}
		require.False(t, rateLimitedParent.IsSchedulable(), "precondition: global 限流母提供商自身不可调度")
		lookup := func(id int64) *Record {
			if id == rateLimitedParent.ID {
				return rateLimitedParent
			}
			return nil
		}
		require.True(t, ParentHealthyForShadow(shadow, lookup),
			"母提供商 global 限流不应连坐 spark 影子")
	})

	t.Run("overloaded_parent_does_not_block_shadow", func(t *testing.T) {
		// 过载退避(OverloadUntil)同属 global 维度运行态,不连坐影子。
		until := time.Now().Add(30 * time.Minute)
		overloadedParent := &Record{
			ID:            100,
			Platform:      capability.PlatformOpenAI,
			Type:          capability.ProviderTypeOAuth,
			Status:        billing.StatusActive,
			Schedulable:   true,
			OverloadUntil: &until,
		}
		require.False(t, overloadedParent.IsSchedulable(), "precondition: 过载母提供商自身不可调度")
		lookup := func(id int64) *Record {
			if id == overloadedParent.ID {
				return overloadedParent
			}
			return nil
		}
		require.True(t, ParentHealthyForShadow(shadow, lookup),
			"母提供商过载退避不应连坐 spark 影子")
	})

	t.Run("temp_unschedulable_parent_blocks_shadow", func(t *testing.T) {
		// G2:TempUnschedulableUntil 对 OpenAI 提供商由 401/token 刷新耗尽/transport·proxy 写入,
		// 代表共享凭据/传输坏死 → 影子共享母 token+proxy,应被挡(与 global 限流 RateLimitResetAt 区分)。
		until := time.Now().Add(15 * time.Minute)
		tempUnschedParent := &Record{
			ID:                     100,
			Platform:               capability.PlatformOpenAI,
			Type:                   capability.ProviderTypeOAuth,
			Status:                 billing.StatusActive,
			Schedulable:            true,
			TempUnschedulableUntil: &until,
		}
		lookup := func(id int64) *Record {
			if id == tempUnschedParent.ID {
				return tempUnschedParent
			}
			return nil
		}
		require.False(t, ParentHealthyForShadow(shadow, lookup),
			"母提供商 TempUnschedulableUntil(凭据/传输坏死)冷却期内应挡住影子")
	})

	t.Run("expired_parent_credentials_block_shadow", func(t *testing.T) {
		// 凭据真正过期(AutoPauseOnExpired + ExpiresAt 已过)→ 透传 token 不可用 → 影子应被挡。
		expiredAt := time.Now().Add(-1 * time.Hour)
		expiredParent := &Record{
			ID:                 100,
			Platform:           capability.PlatformOpenAI,
			Type:               capability.ProviderTypeOAuth,
			Status:             billing.StatusActive,
			Schedulable:        true,
			AutoPauseOnExpired: true,
			ExpiresAt:          &expiredAt,
		}
		lookup := func(id int64) *Record {
			if id == expiredParent.ID {
				return expiredParent
			}
			return nil
		}
		require.False(t, ParentHealthyForShadow(shadow, lookup),
			"母提供商凭据过期时影子应被挡(透传 token 不可用)")
	})

	t.Run("non_oauth_parent_blocks_shadow", func(t *testing.T) {
		// 母提供商被改成非 OpenAI OAuth(如 apikey)后,透传凭据解析必失败,
		// 影子应 fail-closed 不进调度候选(即便提供商 active、凭据未过期)。
		apikeyParent := &Record{
			ID:          100,
			Platform:    capability.PlatformOpenAI,
			Type:        capability.ProviderTypeAPIKey,
			Status:      billing.StatusActive,
			Schedulable: true,
		}
		lookup := func(id int64) *Record {
			if id == apikeyParent.ID {
				return apikeyParent
			}
			return nil
		}
		require.False(t, ParentHealthyForShadow(shadow, lookup),
			"母提供商非 OpenAI OAuth 时影子应被挡(fail-closed)")
	})
}
