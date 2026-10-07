package provider

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestOpenAIRuntimeBlock_DoesNotShortenExistingBlock 验证原停调延长与清理断言迁至状态所有者，直接检查私有缓存。
func TestOpenAIRuntimeBlock_DoesNotShortenExistingBlock(t *testing.T) {
	svc := NewRuntimeBlockState(time.Now)
	provider := &Record{ID: 46, Platform: PlatformOpenAI, Type: ProviderTypeOAuth}
	longUntil := time.Now().Add(10 * time.Minute)

	svc.Block(provider.ID, longUntil, "oauth_401")
	svc.Block(provider.ID, time.Time{}, "upstream_disable")

	value, ok := svc.until.Load(provider.ID)
	require.True(t, ok)
	actualUntil, ok := value.(time.Time)
	require.True(t, ok)
	require.WithinDuration(t, longUntil, actualUntil, time.Second)
}

func TestOpenAIRuntimeBlock_ClearProviderSchedulingBlock(t *testing.T) {
	svc := NewRuntimeBlockState(time.Now)
	provider := &Record{ID: 47, Platform: PlatformOpenAI, Type: ProviderTypeOAuth}

	svc.Block(provider.ID, time.Now().Add(time.Minute), "429")
	require.True(t, svc.Blocked(provider.ID, func() string { return RefreshCredentialIdentity(provider) }))

	svc.ClearProviderSchedulingBlock(provider.ID)
	require.False(t, svc.Blocked(provider.ID, func() string { return RefreshCredentialIdentity(provider) }))
}
