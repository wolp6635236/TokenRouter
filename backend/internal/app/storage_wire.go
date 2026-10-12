//go:build wireinject

package app

import (
	"github.com/google/wire"

	"github.com/TokenFlux/TokenRouter/internal/infra/timingwheel"
	"github.com/TokenFlux/TokenRouter/internal/settings"
)

// storageProviders 将通用设置读取接口绑定到共享 Store。
var storageProviders = wire.NewSet(
	provideSQLDB,
	provideSettingsStore,
	wire.Bind(new(settings.Repository), new(*settings.Store)),
	provideIdempotencyRepository,
	provideIdempotencyCoordinator,
	provideIdempotencyHTTP,
	provideIdempotencyCleanupService,
	timingwheel.New,
)
