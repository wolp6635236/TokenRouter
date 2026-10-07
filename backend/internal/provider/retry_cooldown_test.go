package provider_test

import (
	"context"
	"testing"
	"time"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// TestCheckErrorPolicy 通过六组输入检查错误处理规则。
// ---------------------------------------------------------------------------

// TestGatewayFailoverSideEffects_BedrockUsesMappedModel 检查 Bedrock 临时停调规则
// 使用实际上游模型，并禁止池模式同提供商重试。

// ---------------------------------------------------------------------------
// TestApplyErrorPolicy 通过四组输入检查错误处理入口。
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// errorPolicyRepoStub 为错误处理测试提供存储替身。
// ---------------------------------------------------------------------------

// retryExhaustedCooldownRepoStub 记录同提供商重试耗尽后的本地冷却写入。
type retryExhaustedCooldownRepoStub struct {
	providercore.RetryCooldownStore

	provider  *providercore.Record
	tempCalls int
}

func (r *retryExhaustedCooldownRepoStub) GetByID(context.Context, int64) (*providercore.Record, error) {
	return r.provider, nil
}

func (r *retryExhaustedCooldownRepoStub) SetTempUnschedulable(context.Context, int64, time.Time, string) error {
	r.tempCalls++
	return nil
}

// TestTempUnscheduleRetryableError_PoolModeSkipsLegacyCooldown 验证池模式的
// 同提供商重试耗尽后进入提供商切换。
func TestTempUnscheduleRetryableError_PoolModeSkipsLegacyCooldown(t *testing.T) {
	poolProvider := &providercore.Record{
		LoadLocation: time.LoadLocation, ID: 81,
		Type:     capability.ProviderTypeAPIKey,
		Platform: capability.PlatformAnthropic,
		Credentials: map[string]any{
			"pool_mode": true,
		},
	}
	repo := &retryExhaustedCooldownRepoStub{provider: poolProvider}
	svc := providercore.NewRetryCooldown(repo, providercore.RetryCooldownOptions{})

	svc.Apply(context.Background(), providercore.RetryCooldownInput{ProviderID: poolProvider.ID, Status: 502, Retryable: true})

	require.Zero(t, repo.tempCalls)

	// 非池模式提供商对特殊错误使用兼容冷却规则。
	repo.provider = &providercore.Record{LoadLocation: time.LoadLocation, ID: 82, Type: capability.ProviderTypeOAuth, Platform: capability.PlatformAntigravity}
	svc.Apply(context.Background(), providercore.RetryCooldownInput{ProviderID: repo.provider.ID, Status: 502, Retryable: true})
	require.Equal(t, 1, repo.tempCalls)
}
