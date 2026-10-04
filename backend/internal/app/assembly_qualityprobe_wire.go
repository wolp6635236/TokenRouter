//go:build wireinject

package app

import (
	"github.com/google/wire"
)

// qualityprobeAssemblyProviders 汇总降智探测的 Wire provider。
var qualityprobeAssemblyProviders = wire.NewSet(
	provideQualityProbeEngine,
	provideQualityProbeHTTP,
	provideQualityProbeRunner,
)
