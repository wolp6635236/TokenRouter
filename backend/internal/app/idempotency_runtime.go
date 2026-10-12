package app

import (
	"time"

	"github.com/TokenFlux/TokenRouter/internal/config"
	"github.com/TokenFlux/TokenRouter/internal/idempotency"
	"github.com/TokenFlux/TokenRouter/internal/infra/telemetry/logging"
)

// idempotencyOptions 用正数配置覆盖默认值，并读取 ObserveOnly 开关。
func idempotencyOptions(cfg *config.Config) idempotency.IdempotencyConfig {
	opts := idempotency.DefaultIdempotencyConfig()
	if cfg == nil {
		return opts
	}
	if cfg.Idempotency.DefaultTTLSeconds > 0 {
		opts.DefaultTTL = time.Duration(cfg.Idempotency.DefaultTTLSeconds) * time.Second
	}
	if cfg.Idempotency.SystemOperationTTLSeconds > 0 {
		opts.SystemOperationTTL = time.Duration(cfg.Idempotency.SystemOperationTTLSeconds) * time.Second
	}
	if cfg.Idempotency.ProcessingTimeoutSeconds > 0 {
		opts.ProcessingTimeout = time.Duration(cfg.Idempotency.ProcessingTimeoutSeconds) * time.Second
	}
	if cfg.Idempotency.FailedRetryBackoffSeconds > 0 {
		opts.FailedRetryBackoff = time.Duration(cfg.Idempotency.FailedRetryBackoffSeconds) * time.Second
	}
	if cfg.Idempotency.MaxStoredResponseLen > 0 {
		opts.MaxStoredResponseLen = cfg.Idempotency.MaxStoredResponseLen
	}
	opts.ObserveOnly = cfg.Idempotency.ObserveOnly
	return opts
}

// provideIdempotencyCoordinator 构造供各 HTTP 入口共享的幂等协调器。
func provideIdempotencyCoordinator(repo idempotency.IdempotencyRepository, cfg *config.Config) *idempotency.IdempotencyCoordinator {
	coordinator := idempotency.NewIdempotencyCoordinator(repo, idempotencyOptions(cfg), idempotencyObserver())
	return coordinator
}

// provideIdempotencyCleanupService 构造清理任务，应用生命周期管理器负责启停。
func provideIdempotencyCleanupService(repo idempotency.IdempotencyRepository, cfg *config.Config) *idempotency.IdempotencyCleanupService {
	opts := idempotency.CleanupOptions{Observer: idempotencyObserver()}
	if cfg != nil {
		opts.Interval = time.Duration(cfg.Idempotency.CleanupIntervalSeconds) * time.Second
		opts.Batch = cfg.Idempotency.CleanupBatchSize
	}
	return idempotency.NewIdempotencyCleanupService(repo, opts)
}

// idempotencyObserver 将幂等事件写入应用日志。
func idempotencyObserver() idempotency.Observer {
	return idempotency.ObserverFunc(func(component, message string) { logging.LegacyPrintf(component, "%s", message) })
}
