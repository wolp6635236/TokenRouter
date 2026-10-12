package ops

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/scheduler"
)

type opsMetricsProjectionRepo struct {
	ProviderLoadSource
	providers       []ProviderObservation
	providerLoads   []ProviderWithConcurrency
	listCalls       int
	projectionCalls int
}

type opsMetricsFallbackRepo struct {
	ProviderLoadSource
	providers []ProviderObservation
	listCalls int
}

type opsMetricsLoadCache struct {
	scheduler.ConcurrencyCache
	loads map[int64]*ProviderLoadInfo
	got   []ProviderWithConcurrency
}

func (r *opsMetricsProjectionRepo) ListSchedulable(context.Context) ([]ProviderObservation, error) {
	r.listCalls++
	return r.providers, nil
}

func (r *opsMetricsProjectionRepo) ListSchedulableProviderLoads(context.Context) ([]ProviderWithConcurrency, error) {
	r.projectionCalls++
	return r.providerLoads, nil
}

func (r *opsMetricsFallbackRepo) ListSchedulable(context.Context) ([]ProviderObservation, error) {
	r.listCalls++
	return r.providers, nil
}

func (c *opsMetricsLoadCache) GetProvidersLoadBatch(_ context.Context, providers []ProviderWithConcurrency) (map[int64]*ProviderLoadInfo, error) {
	c.got = providers
	return c.loads, nil
}

func TestCollectConcurrencyQueueDepthUsesProjectionAndPreservesFallbackResult(t *testing.T) {
	loadFactor := 7
	providers := []ProviderObservation{
		{ID: 11, Concurrency: 2, LoadFactor: loadFactor},
		{ID: 12, Concurrency: 3, LoadFactor: 3},
		{ID: 13, LoadFactor: 1},
	}
	providerLoads := []ProviderWithConcurrency{
		{ID: 11, MaxConcurrency: 7},
		{ID: 12, MaxConcurrency: 3},
		{ID: 13, MaxConcurrency: 1},
	}
	loads := map[int64]*ProviderLoadInfo{
		11: {ProviderID: 11, WaitingCount: 2},
		12: {ProviderID: 12, WaitingCount: 3},
		13: {ProviderID: 13, WaitingCount: 0},
	}

	projectionRepo := &opsMetricsProjectionRepo{providers: providers, providerLoads: providerLoads}
	projectionCache := &opsMetricsLoadCache{loads: loads}
	projectionConcurrency := scheduler.NewConcurrencyService(projectionCache)
	projectionConcurrency.SetProviderLoadBatchCacheTTL(0)
	projectionCollector := &OpsMetricsCollector{
		providerRepo:       projectionRepo,
		concurrencyService: projectionConcurrency,
	}

	fallbackRepo := &opsMetricsFallbackRepo{providers: providers}
	fallbackCache := &opsMetricsLoadCache{loads: loads}
	fallbackConcurrency := scheduler.NewConcurrencyService(fallbackCache)
	fallbackConcurrency.SetProviderLoadBatchCacheTTL(0)
	fallbackCollector := &OpsMetricsCollector{
		providerRepo:       fallbackRepo,
		concurrencyService: fallbackConcurrency,
	}

	projectionDepth := projectionCollector.collectConcurrencyQueueDepth(context.Background())
	fallbackDepth := fallbackCollector.collectConcurrencyQueueDepth(context.Background())

	require.NotNil(t, projectionDepth)
	require.NotNil(t, fallbackDepth)
	require.Equal(t, 5, *projectionDepth)
	require.Equal(t, *fallbackDepth, *projectionDepth)
	require.Equal(t, 1, projectionRepo.projectionCalls)
	require.Zero(t, projectionRepo.listCalls)
	require.Equal(t, 1, fallbackRepo.listCalls)
	require.Equal(t, providerLoads, projectionCache.got)
	require.Equal(t, fallbackCache.got, projectionCache.got)
}

func BenchmarkOpsMetricsCollectorCollectConcurrencyQueueDepth(b *testing.B) {
	const providerCount = 1000
	loadFactor := 8
	providers := make([]ProviderObservation, providerCount)
	providerLoads := make([]ProviderWithConcurrency, providerCount)
	for i := range providerCount {
		id := int64(i + 1)
		providers[i] = ProviderObservation{
			ID:          id,
			Concurrency: 4,
			LoadFactor:  loadFactor,
		}
		providerLoads[i] = ProviderWithConcurrency{ID: id, MaxConcurrency: loadFactor}
	}

	repo := &opsMetricsProjectionRepo{providers: providers, providerLoads: providerLoads}
	cache := &opsMetricsLoadCache{loads: map[int64]*ProviderLoadInfo{}}
	concurrency := scheduler.NewConcurrencyService(cache)
	concurrency.SetProviderLoadBatchCacheTTL(0)
	collector := &OpsMetricsCollector{providerRepo: repo, concurrencyService: concurrency}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if depth := collector.collectConcurrencyQueueDepth(context.Background()); depth == nil || *depth != 0 {
			b.Fatalf("unexpected queue depth: %v", depth)
		}
	}
}
