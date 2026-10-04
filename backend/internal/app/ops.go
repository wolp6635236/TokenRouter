package app

import (
	"context"
	"database/sql"
	"errors"
	"os"

	"github.com/TokenFlux/TokenRouter/internal/notification"
	"github.com/TokenFlux/TokenRouter/internal/provider"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	"github.com/TokenFlux/TokenRouter/internal/config"
	egresspostgres "github.com/TokenFlux/TokenRouter/internal/egress/postgres"
	identitypostgres "github.com/TokenFlux/TokenRouter/internal/identity/postgres"
	logger "github.com/TokenFlux/TokenRouter/internal/infra/telemetry/logging"
	"github.com/TokenFlux/TokenRouter/internal/ops"
	"github.com/TokenFlux/TokenRouter/internal/ops/maintenance"
	opspostgres "github.com/TokenFlux/TokenRouter/internal/ops/postgres"
	opsadapter "github.com/TokenFlux/TokenRouter/internal/ops/provider"
	opsredis "github.com/TokenFlux/TokenRouter/internal/ops/rediscache"
	providerpostgres "github.com/TokenFlux/TokenRouter/internal/provider/postgres"
	"github.com/TokenFlux/TokenRouter/internal/scheduler"
	"github.com/TokenFlux/TokenRouter/internal/settings"
	"github.com/TokenFlux/TokenRouter/internal/settings/preaggregation"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

func provideOpsRepository(db *sql.DB) ops.OpsRepository { return opspostgres.NewOpsRepository(db) }
func provideOpsService(repo ops.OpsRepository, settings *settings.Store, options *ops.Options, providers *providerpostgres.ProviderStore, users *identitypostgres.UserStore, c *scheduler.ConcurrencyService, sink *ops.OpsSystemLogSink, quota *provider.QuotaSettingsCache, worker *apikey.AuthCacheInvalidationWorker, keys *apikey.APIKeyService, pre *preaggregation.PreAggregationSettingsService) *ops.OpsService {
	s := ops.NewOpsService(repo, settings, options, opsProviderReader{providers}, opsUsers{users}, c, sink, opsadapter.LogControl{})
	s.SetPreAggregationSettings(pre)
	s.SetOpenAIQuotaAutoPauseSettingsSink(quota.SetOpenAIQuotaAutoPauseSettings)
	quota.WarmOpenAIQuotaAutoPauseSettings(context.Background())
	s.SetAuthObservers(worker, keys)
	return s
}

func provideOpsCollector(repo ops.OpsRepository, settings *settings.Store, providers *providerpostgres.ProviderStore, c *scheduler.ConcurrencyService, db *sql.DB, r *redis.Client, options *ops.Options) *ops.OpsMetricsCollector {
	return ops.NewOpsMetricsCollector(repo, settings, opsProviderReader{providers}, c, opspostgres.NewMetricsQueries(db), opsredis.NewRuntime(r), options, opsadapter.NewHostObserver(db, r))
}

func provideOpsAggregation(repo ops.OpsRepository, settings *settings.Store, db *sql.DB, r *redis.Client, options *ops.Options, pre *preaggregation.PreAggregationSettingsService) *ops.OpsAggregationService {
	return ops.NewOpsAggregationService(repo, settings, opspostgres.NewAdvisory(db), opsredis.NewRuntime(r), options, pre)
}

func provideOpsEvaluator(s *ops.OpsService, repo ops.OpsRepository, email *notification.Mailer, notifications *notification.NotificationEmailService, r *redis.Client, options *ops.Options, proxies *egresspostgres.ProxyStore) *ops.OpsAlertEvaluatorService {
	return ops.NewOpsAlertEvaluatorService(s, repo, notificationOpsDelivery(email, notifications), opsredis.NewRuntime(r), options, proxies)
}

func provideOpsCleanup(repo ops.OpsRepository, settings *settings.Store, s *ops.OpsService, db *sql.DB, r *redis.Client, options *ops.Options) *ops.OpsCleanupService {
	c := ops.NewOpsCleanupService(repo, opspostgres.NewCleanupStore(db), opsredis.NewRuntime(r), options, settings)
	s.SetCleanupReloader(c)
	return c
}

func provideOpsReports(s *ops.OpsService, users *identitypostgres.UserStore, email *notification.Mailer, notifications *notification.NotificationEmailService, r *redis.Client, options *ops.Options) *ops.OpsScheduledReportService {
	return ops.NewOpsScheduledReportService(s, opsUsers{users}, notificationOpsDelivery(email, notifications), opsredis.NewRuntime(r), options)
}

func provideOpsIngress(repo ops.OpsRepository, s *ops.OpsService) *ops.OpsIngressRejectAggregator {
	r, ok := repo.(ops.OpsIngressRejectRepository)
	if !ok {
		return nil
	}
	a := ops.NewOpsIngressRejectAggregator(r)
	s.SetIngressRejectAggregator(a)
	return a
}

func provideReleaseClient(cfg *config.Config) opsadapter.ReleaseClient {
	return opsadapter.NewReleaseClient(opsadapter.ReleaseOptions{ProxyURL: cfg.Update.ProxyURL, AllowDirectOnProxyError: cfg.Security.ProxyFallback.AllowDirectOnError, GitHubToken: os.Getenv("UPDATE_GITHUB_TOKEN")})
}

func provideReleaseQuery(cache ops.UpdateCache, client opsadapter.ReleaseClient, info BuildInfo, cfg *config.Config) *ops.ReleaseQuery {
	repo := ""
	if cfg != nil {
		repo = cfg.Update.GitHubRepo
	}
	return ops.NewReleaseQuery(cache, client, info.Version, info.BuildType, repo)
}

func provideUpdateMaintenance(query *ops.ReleaseQuery, client opsadapter.ReleaseClient) *maintenance.UpdateService {
	return maintenance.NewUpdateService(query, opsadapter.NewBinaryInstaller(client, nil))
}

func provideOpsOptions(cfg *config.Config) *ops.Options {
	if cfg == nil {
		return nil
	}
	o := &ops.Options{Timezone: cfg.Timezone, IsNotFound: func(e error) bool { return errors.Is(e, sql.ErrNoRows) || errors.Is(e, ops.ErrRowNotFound) }, Logf: func(f string, a ...any) { logger.LegacyPrintf("service.ops", f, a...) }}
	o.CleanupCompleted = func(counts string) {
		logger.L().Info("[OpsCleanup] cleanup complete", zap.String("component", "service.ops_cleanup"), zap.String("deleted_counts", counts))
	}
	o.Ops.Enabled = cfg.Ops.Enabled
	o.Ops.Aggregation.Enabled = cfg.Ops.Aggregation.Enabled
	o.Ops.Cleanup = ops.CleanupOptions(cfg.Ops.Cleanup)
	o.Ops.MetricsCollectorCache = ops.MetricsCollectorCacheOptions(cfg.Ops.MetricsCollectorCache)
	o.Database.MaxOpenConns = cfg.Database.MaxOpenConns
	o.Redis.PoolSize = cfg.Redis.PoolSize
	o.Log = ops.LogOptions{Level: cfg.Log.Level, Caller: cfg.Log.Caller, StacktraceLevel: cfg.Log.StacktraceLevel, Sampling: ops.SamplingOptions(cfg.Log.Sampling)}
	return o
}
