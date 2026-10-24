//go:build wireinject

package app

import (
	"github.com/google/wire"
)

// assembly_qualityprobe_wire.go registers quality-probe construction with Wire.
// The app adapters own storage, test-service, mail, HTTP, and lifecycle binding
// while qualityprobe retains its scheduling and decision rules.

// qualityprobeAssemblyProviders supplies the quality-probe engine, HTTP handler,
// and periodic runner to the application dependency graph.
var qualityprobeAssemblyProviders = wire.NewSet(
	provideQualityProbeEngine,
	provideQualityProbeHTTP,
	provideQualityProbeRunner,
)
