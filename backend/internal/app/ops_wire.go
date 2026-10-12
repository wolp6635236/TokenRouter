//go:build wireinject

package app

import (
	"github.com/google/wire"

	opsredis "github.com/TokenFlux/TokenRouter/internal/ops/rediscache"
)

var opsProviders = wire.NewSet(provideOpsOptions, provideOpsRepository, provideOpsService, provideOpsCollector, provideOpsAggregation, provideOpsEvaluator, provideOpsCleanup, provideOpsReports, provideOpsIngress, provideReleaseClient, opsredis.NewUpdateCache, provideReleaseQuery, provideUpdateMaintenance)
