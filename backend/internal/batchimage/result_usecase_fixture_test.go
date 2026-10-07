package batchimage_test

import (
	"context"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/billing"

	"github.com/TokenFlux/TokenRouter/internal/batchimage"
	batchprovider "github.com/TokenFlux/TokenRouter/internal/batchimage/provider"
	"github.com/TokenFlux/TokenRouter/internal/config"
	"github.com/TokenFlux/TokenRouter/internal/provider"
)

// resultProviderFixture 返回下载/清理已绑定提供商，不参与重新选号。
type resultProviderFixture struct{ provider *provider.Record }

func (r *resultProviderFixture) GetByID(context.Context, int64) (*provider.Record, error) {
	return r.provider, nil
}

func newBatchDownloadFixture(repo batchimage.BatchImageRepository, registry *batchimage.Registry[batchprovider.BatchImageProvider], providers batchprovider.ResultProviders, limiter batchimage.BatchImageDownloadLimiter, cfg *config.Config) *batchimage.Download {
	return &batchimage.Download{Repo: repo, Limiter: limiter, ResolveProvider: (batchprovider.ResultAccess{Registry: registry, Providers: providers}).Download, Options: batchimage.DownloadOptions{MaxItems: cfg.BatchImage.MaxDownloadItemsZip, MaxBytes: cfg.BatchImage.MaxDownloadBytesPerRequest, Duration: time.Duration(cfg.BatchImage.MaxDownloadDurationSeconds) * time.Second}}
}

func newBatchCleanupFixture(repo batchimage.BatchImageRepository, registry *batchimage.Registry[batchprovider.BatchImageProvider], providers batchprovider.ResultProviders, cfg *config.Config) *batchimage.Cleanup {
	return &batchimage.Cleanup{Repo: repo, ResolveProvider: (batchprovider.ResultAccess{Registry: registry, Providers: providers}).Cleanup, Options: batchimage.CleanupOptions{InputRetention: time.Duration(cfg.BatchImage.InputRetentionAfterTerminalHours) * time.Hour, Interval: time.Duration(cfg.BatchImage.CleanupIntervalMinutes) * time.Minute, BatchSize: cfg.BatchImage.CleanupBatchSize}}
}

// cloneResultAllocations 只复制仓储替身保存的数据，使用资金模块既有复制入口。
func cloneResultAllocations(values []billing.BillingAllocation) []billing.BillingAllocation {
	if len(values) == 0 {
		return nil
	}
	out := make([]billing.BillingAllocation, len(values))
	for i, value := range values {
		out[i] = billing.CloneBillingAllocation(value, value.AmountUSD)
	}
	return out
}
