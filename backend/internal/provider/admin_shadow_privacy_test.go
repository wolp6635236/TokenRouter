package provider_test

import (
	"context"
	"testing"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"

	"github.com/stretchr/testify/require"
)

// TestForceOpenAIPrivacy_SkipsShadow 验证影子隐私设置跳过(由母提供商管理),
// 早返不触碰任何依赖(svc 无 deps,若未守卫会 nil panic)。
func TestForceOpenAIPrivacy_SkipsShadow(t *testing.T) {
	svc := providercore.NewPrivacyService(nil, nil, providercore.PrivacyOptions{})
	pid := int64(1)
	shadow := &providercore.Record{ID: 2, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, ParentProviderID: &pid}
	require.Equal(t, "", svc.ForceOpenAIPrivacy(context.Background(), shadow), "影子隐私设置应跳过")
}
