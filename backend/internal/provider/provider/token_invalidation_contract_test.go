package provider_test

import (
	"context"
	"testing"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	qoder "github.com/TokenFlux/TokenRouter/internal/upstream/qoder"
	"github.com/stretchr/testify/require"
)

func TestCompositeTokenCacheInvalidator_QoderCosy(t *testing.T) {
	cache := &qoderInvalidationCache{}
	tokenSource := provideradapter.NewQoderTokenProvider(qoder.SessionBuilder{})
	tokenSource.Core.Sessions[42] = providercore.QoderSessionCacheEntry[*qoder.SessionContext]{CredentialsHash: "old"}
	invalidator := providercore.NewCompositeTokenCacheInvalidator(cache, tokenSource, nil)
	provider := &providercore.Record{
		ID:       42,
		Platform: capability.PlatformQoder,
		Type:     capability.ProviderTypeCosy,
	}

	err := invalidator.InvalidateToken(context.Background(), provider)

	require.NoError(t, err)
	require.Contains(t, cache.deletedKeys, "qoder:provider:42")
	tokenSource.Core.Mu.Lock()
	_, cached := tokenSource.Core.Sessions[42]
	tokenSource.Core.Mu.Unlock()
	require.False(t, cached, "qoder provider session cache should be invalidated too")
}

// qoderInvalidationCache 记录测试调用的删除操作。
type qoderInvalidationCache struct {
	providercore.AccessTokenCache
	deletedKeys []string
}

func (c *qoderInvalidationCache) DeleteAccessToken(_ context.Context, key string) error {
	c.deletedKeys = append(c.deletedKeys, key)
	return nil
}

func TestCompositeTokenCacheInvalidator_QoderCosyInvalidatesProviderWithoutExternalCache(t *testing.T) {
	tokenSource := provideradapter.NewQoderTokenProvider(qoder.SessionBuilder{})
	tokenSource.Core.Sessions[43] = providercore.QoderSessionCacheEntry[*qoder.SessionContext]{CredentialsHash: "old"}
	invalidator := providercore.NewCompositeTokenCacheInvalidator(nil, tokenSource, nil)
	provider := &providercore.Record{
		ID:       43,
		Platform: capability.PlatformQoder,
		Type:     capability.ProviderTypeCosy,
	}

	err := invalidator.InvalidateToken(context.Background(), provider)

	require.NoError(t, err)
	tokenSource.Core.Mu.Lock()
	_, cached := tokenSource.Core.Sessions[43]
	tokenSource.Core.Mu.Unlock()
	require.False(t, cached)
}
