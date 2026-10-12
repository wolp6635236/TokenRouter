package provider

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

// retryExhaustedCooldownRepoStub 记录同提供商重试耗尽后的本地冷却写入。
type retryExhaustedCooldownRepoStub struct {
	RetryCooldownStore

	provider  *Record
	tempCalls int
}

type capacityShedProviderRepoStub struct {
	RetryCooldownStore
	// 嵌入接口，未实现的方法会 panic（不应被调用）

	tempUnschedCalls int
}

func (r *retryExhaustedCooldownRepoStub) GetByID(context.Context, int64) (*Record, error) {
	return r.provider, nil
}

func (r *retryExhaustedCooldownRepoStub) SetTempUnschedulable(context.Context, int64, time.Time, string) error {
	r.tempCalls++
	return nil
}

// TestTempUnscheduleRetryableError_PoolModeSkipsLegacyCooldown 验证池模式的
// 同提供商重试耗尽后进入提供商切换。
func TestTempUnscheduleRetryableError_PoolModeSkipsLegacyCooldown(t *testing.T) {
	poolProvider := &Record{
		LoadLocation: time.LoadLocation, ID: 81,
		Type:     capability.ProviderTypeAPIKey,
		Platform: capability.PlatformAnthropic,
		Credentials: map[string]any{
			"pool_mode": true,
		},
	}
	repo := &retryExhaustedCooldownRepoStub{provider: poolProvider}
	svc := NewRetryCooldown(repo, RetryCooldownOptions{})

	svc.Apply(context.Background(), RetryCooldownInput{ProviderID: poolProvider.ID, Status: 502, Retryable: true})

	require.Zero(t, repo.tempCalls)

	// 非池模式提供商对特殊错误使用兼容冷却规则。
	repo.provider = &Record{LoadLocation: time.LoadLocation, ID: 82, Type: capability.ProviderTypeOAuth, Platform: capability.PlatformAntigravity}
	svc.Apply(context.Background(), RetryCooldownInput{ProviderID: repo.provider.ID, Status: 502, Retryable: true})
	require.Equal(t, 1, repo.tempCalls)
}

func (r *capacityShedProviderRepoStub) SetTempUnschedulable(_ context.Context, _ int64, _ time.Time, _ string) error {
	r.tempUnschedCalls++
	return nil
}

func (r *capacityShedProviderRepoStub) GetByID(_ context.Context, id int64) (*Record, error) {
	return &Record{LoadLocation: time.LoadLocation, ID: id, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth}, nil
}

func TestTempUnscheduleRetryableErrorSkipsRequestScopedTransient(t *testing.T) {
	t.Run("请求级瞬时故障不写提供商状态", func(t *testing.T) {
		repo := &capacityShedProviderRepoStub{}
		svc := NewRetryCooldown(repo, RetryCooldownOptions{})

		svc.Apply(context.Background(), RetryCooldownInput{ProviderID: 1, Status: 502, Retryable: true, RequestScopedTransient: true})

		require.Zero(t, repo.tempUnschedCalls)
	})

	// 同样的 502 未标记为请求级瞬时故障时，按提供商错误执行临时停调。
	t.Run("未标记时保持原有临时摘号语义", func(t *testing.T) {
		repo := &capacityShedProviderRepoStub{}
		svc := NewRetryCooldown(repo, RetryCooldownOptions{})

		svc.Apply(context.Background(), RetryCooldownInput{ProviderID: 1, Status: 502, Retryable: true})

		require.Equal(t, 1, repo.tempUnschedCalls)
	})
}
