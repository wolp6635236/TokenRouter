//go:build wireinject

package app

import (
	"github.com/google/wire"

	"github.com/TokenFlux/TokenRouter/internal/scheduler"
	schedulerpostgres "github.com/TokenFlux/TokenRouter/internal/scheduler/postgres"
	schedulerredis "github.com/TokenFlux/TokenRouter/internal/scheduler/rediscache"
)

// schedulerProviders 构造共享调度实例，生命周期管理器调用其 Start 和 Stop。
var schedulerProviders = wire.NewSet(provideSelectionSnapshots, provideSelectionReads, provideSelectionShared, provideSelectionFreeQuota, provideSelectionModelTransient, provideSelectionProxyCircuit, provideGenericSelection, provideCompatibleSelection, provideGeminiSelection, provideSchedulerSharedState, provideUpstreamHealth, provideSchedulerDiagnosticsHTTP, provideSchedulerCache,
	wire.Bind(new(scheduler.SnapshotCache), new(*schedulerredis.SnapshotCache)),
	provideSchedulerSnapshot,
	provideConcurrencyCache, provideConcurrency, provideSessionCache, provideMessageQueue,
	schedulerredis.NewRPMCache, schedulerredis.NewUserRPMCache, schedulerredis.NewUserMsgQueueCache,
	schedulerpostgres.NewSchedulerOutboxRepository)
