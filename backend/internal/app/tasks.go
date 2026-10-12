package app

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	keyhttp "github.com/TokenFlux/TokenRouter/internal/apikey/httpapi"
	keypostgres "github.com/TokenFlux/TokenRouter/internal/apikey/postgres"
	"github.com/TokenFlux/TokenRouter/internal/app/lifecycle"
	"github.com/TokenFlux/TokenRouter/internal/batchimage"
	batchhttp "github.com/TokenFlux/TokenRouter/internal/batchimage/httpapi"
	batchimageprovider "github.com/TokenFlux/TokenRouter/internal/batchimage/provider"
	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/config"
	"github.com/TokenFlux/TokenRouter/internal/creative"
	creativehttp "github.com/TokenFlux/TokenRouter/internal/creative/httpapi"
	gatewayhttp "github.com/TokenFlux/TokenRouter/internal/gateway/httpapi"
	providerpostgres "github.com/TokenFlux/TokenRouter/internal/provider/postgres"
)

// creativeManagedKeys 通过共享 KeyStore 为创作台任务提供 Key 操作。
type creativeManagedKeys struct {
	store *keypostgres.KeyStore
}

// batchCleanupRuntime 区分清理循环与任务消费循环的生命周期实例。
type batchCleanupRuntime struct{ *batchimage.Runtime }

// taskRequestActivity 等待提交、下载及管理请求结束，再停止 task worker 和共享存储。
type taskRequestActivity struct{ *lifecycle.Operations }

// batchImageAccessPorts 从认证上下文读取批量图片任务需要的 Key、订阅和会话 ID。
func batchImageAccessPorts() batchhttp.AccessPorts {
	return batchhttp.AccessPorts{Key: func(c *gin.Context) (*apikey.APIKey, bool) {
		k, ok := keyhttp.GetAPIKeyFromContext(c)
		return apikey.CopyAPIKey(k), ok
	}, PreferredSubscription: func(c *gin.Context) (*billing.UserSubscription, bool) {
		v, ok := gatewayhttp.GetAPIKeyBillingContext(c)
		if !ok || v == nil || v.Mode != apikey.APIKeyBillingModeSubscription || v.Subscription == nil {
			return nil, false
		}
		return v.Subscription, true
	}, SessionID: gatewayhttp.ExtractClientSessionID}
}

func (keys creativeManagedKeys) GetManagedKeyByUserAndGroup(ctx context.Context, userID, groupID int64, managedBy string) (*apikey.APIKey, error) {
	key, err := keys.store.GetManagedKeyByUserAndGroup(ctx, userID, groupID, managedBy)
	return apikey.CopyAPIKey(key), err
}

func (keys creativeManagedKeys) CreateManagedKey(ctx context.Context, key *apikey.APIKey) error {
	view := apikey.CopyAPIKey(key)
	err := keys.store.CreateManagedKey(ctx, view)
	if key != nil && view != nil {
		*key = *apikey.CopyAPIKey(view)
	}
	return err
}

func provideBatchRegistry(cfg *config.Config) *batchimage.Registry[batchimageprovider.BatchImageProvider] {
	return batchimage.NewRegistry[batchimageprovider.BatchImageProvider](batchimageprovider.NewGeminiAPIBatchImageProvider(nil), batchimageprovider.NewVertexBatchImageProvider(batchVertexOptions(cfg), nil, nil, nil))
}

func provideCreativeHTTP(s *creative.Public, activity *taskRequestActivity) *creativehttp.CreativeHandler {
	h := creativehttp.NewCreativeHandler(s)
	h.BindActivity(activity.Enter)
	return h
}

func provideBatchHTTP(s *batchimage.Public, d *batchimage.Download, c *batchimage.Cleanup, activity *taskRequestActivity) *batchhttp.BatchImageHandler {
	h := batchhttp.NewBatchImageHandler(s, d, c, batchImageAccessPorts())
	h.BindActivity(activity.Enter)
	return h
}

// provideBatchDownload 装配批量图片下载用例，复用提供商注册表。
func provideBatchDownload(repo batchimage.BatchImageRepository, providers *providerpostgres.ProviderStore, limiter batchimage.BatchImageDownloadLimiter, cfg *config.Config, registry *batchimage.Registry[batchimageprovider.BatchImageProvider]) *batchimage.Download {
	core := &batchimage.Download{Repo: repo, Limiter: limiter, ResolveProvider: (batchimageprovider.ResultAccess{Registry: registry, Providers: providers}).Download}
	if cfg != nil {
		core.Options = batchimage.DownloadOptions{MaxItems: cfg.BatchImage.MaxDownloadItemsZip, MaxBytes: cfg.BatchImage.MaxDownloadBytesPerRequest, Duration: time.Duration(cfg.BatchImage.MaxDownloadDurationSeconds) * time.Second}
	}
	return core
}

// provideBatchCleanup 注入批量图片清理配置和日志出口，运行循环由模块持有。
func provideBatchCleanup(repo batchimage.BatchImageRepository, providers *providerpostgres.ProviderStore, cfg *config.Config, registry *batchimage.Registry[batchimageprovider.BatchImageProvider]) *batchimage.Cleanup {
	core := &batchimage.Cleanup{Repo: repo, Now: time.Now, Observe: creativeObserve, ResolveProvider: (batchimageprovider.ResultAccess{Registry: registry, Providers: providers}).Cleanup}
	if cfg != nil {
		core.Options = batchimage.CleanupOptions{InputRetention: time.Duration(cfg.BatchImage.InputRetentionAfterTerminalHours) * time.Hour, Interval: time.Duration(cfg.BatchImage.CleanupIntervalMinutes) * time.Minute, BatchSize: cfg.BatchImage.CleanupBatchSize}
	}
	return core
}

func provideBatchCleanupRuntime(core *batchimage.Cleanup, cfg *config.Config) *batchCleanupRuntime {
	enabled := core != nil && core.Repo != nil && cfg != nil && cfg.BatchImage.Enabled && core.CleanupInterval() > 0
	return &batchCleanupRuntime{batchimage.NewRuntime("batch image cleanup", enabled, core.Run)}
}

func provideTaskActivity(manager *lifecycle.Manager) *taskRequestActivity {
	activity := &taskRequestActivity{lifecycle.NewOperations("TaskRequestsAndDownloads")}
	manager.Register(lifecycle.Hook{Name: "TaskRequestsAndDownloads", StopOrder: 16, Stop: activity.StopContext})
	return activity
}
