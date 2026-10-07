package provider

import (
	"context"
	"errors"
	"testing"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/upstream/openai"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

// TestEnsureOpenAIPrivacySkipsShadow 验证影子提供商跳过隐私设置（不调用 privacyClientFactory）。
// 影子提供商透传母提供商凭据，但 Extra 通常为空，需给它一个 access_token 才能让
// 测试提供非空 token，使请求进入影子提供商检查。
func TestEnsureOpenAIPrivacySkipsShadow(t *testing.T) {
	pid := int64(100)
	shadow := &providercore.Record{
		ID:               200,
		Platform:         capability.PlatformOpenAI,
		Type:             capability.ProviderTypeOAuth,
		ParentProviderID: &pid,

		Credentials: map[string]any{"access_token": "shadow-passthrough-token"},
	}
	privacyCalled := false
	svc := providercore.NewPrivacyService(nil, nil, PrivacyOptions(func(proxyURL string) (*req.Client, error) {
		privacyCalled = true
		return nil, errors.New("should not reach factory for shadow provider")
	}, openai.PrivacyEndpoints{}))
	got := svc.EnsureOpenAIPrivacy(context.Background(), shadow)
	require.Equal(t, "", got)
	require.False(t, privacyCalled, "privacyClientFactory 不应被影子提供商触发")
}
