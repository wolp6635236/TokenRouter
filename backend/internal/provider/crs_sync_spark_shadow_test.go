package provider_test

import (
	"context"
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

// TestPropagateProviderProxyToShadows CRS/管理端 改母提供商 proxy 后,
// 影子 proxy 必须跟随(影子 proxy 恒继承母提供商,否则出站漂移)。
func TestPropagateProviderProxyToShadows(t *testing.T) {
	ctx := context.Background()
	repo := newCRSShadowStore()

	oldProxy := int64(11)
	mother := &provider.Record{Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, ProxyID: &oldProxy}
	require.NoError(t, repo.Create(ctx, mother))
	parentID := mother.ID

	shadow := &provider.Record{
		Platform:         capability.PlatformOpenAI,
		Type:             capability.ProviderTypeOAuth,
		ParentProviderID: &parentID,
		QuotaDimension:   provider.QuotaDimensionSpark,
		ProxyID:          &oldProxy,
	}
	require.NoError(t, repo.Create(ctx, shadow))

	newProxy := int64(22)
	require.NoError(t, provider.PropagateProviderProxyToShadows(ctx, repo, parentID, &newProxy))

	got, err := repo.GetByID(ctx, shadow.ID)
	require.NoError(t, err)
	require.NotNil(t, got.ProxyID)
	require.Equal(t, newProxy, *got.ProxyID, "shadow proxy must follow the parent's new proxy")

	// 清空母 proxy 也应传播为 nil。
	require.NoError(t, provider.PropagateProviderProxyToShadows(ctx, repo, parentID, nil))
	got, err = repo.GetByID(ctx, shadow.ID)
	require.NoError(t, err)
	require.Nil(t, got.ProxyID, "clearing parent proxy must clear the shadow proxy too")
}

// TestGuardCRSShadowParentInvariant 有 spark 影子的母提供商经 CRS 任意分支更新后,目标结果
// 必须仍是 OpenAI OAuth;否则(改 api_key 或跨平台 Anthropic/Gemini)影子读透母凭据失败、spark 全崩。
func TestGuardCRSShadowParentInvariant(t *testing.T) {
	ctx := context.Background()
	repo := newCRSShadowStore()

	mother := &provider.Record{Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth}
	require.NoError(t, repo.Create(ctx, mother))
	parentID := mother.ID

	// 无影子:任何目标都放行(含改离 OpenAI OAuth)。
	require.NoError(t, provider.GuardCRSShadowParentInvariant(ctx, repo, mother, capability.PlatformOpenAI, capability.ProviderTypeAPIKey))
	require.NoError(t, provider.GuardCRSShadowParentInvariant(ctx, repo, mother, capability.PlatformAnthropic, capability.ProviderTypeOAuth))

	// 建一个影子后:
	shadow := &provider.Record{
		Platform:         capability.PlatformOpenAI,
		Type:             capability.ProviderTypeOAuth,
		ParentProviderID: &parentID,
		QuotaDimension:   provider.QuotaDimensionSpark,
	}
	require.NoError(t, repo.Create(ctx, shadow))

	// 翻成 OpenAI api_key 被拒。
	err := provider.GuardCRSShadowParentInvariant(ctx, repo, mother, capability.PlatformOpenAI, capability.ProviderTypeAPIKey)
	require.Error(t, err, "must reject converting a shadow parent to openai api_key")
	require.Contains(t, err.Error(), "spark-shadow parent")

	// 跨平台改成 Anthropic OAuth(Type 仍 OAuth、仅 Platform 变)也被拒，验证平台和类型必须同时满足约束。
	require.Error(t, provider.GuardCRSShadowParentInvariant(ctx, repo, mother, capability.PlatformAnthropic, capability.ProviderTypeOAuth),
		"must reject moving a shadow parent to a non-OpenAI platform even if type stays oauth")

	// 改成 Gemini api_key 被拒。
	require.Error(t, provider.GuardCRSShadowParentInvariant(ctx, repo, mother, capability.PlatformGemini, capability.ProviderTypeAPIKey))

	// 保持 OpenAI OAuth(重新同步母提供商)放行,即便仍有影子。
	require.NoError(t, provider.GuardCRSShadowParentInvariant(ctx, repo, mother, capability.PlatformOpenAI, capability.ProviderTypeOAuth))
}
