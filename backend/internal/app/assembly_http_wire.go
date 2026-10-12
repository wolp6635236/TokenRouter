//go:build wireinject

package app

import (
	"github.com/google/wire"

	"github.com/TokenFlux/TokenRouter/internal/server"
	serverhttp "github.com/TokenFlux/TokenRouter/internal/server/httpapi"
)

// httpAssemblyProviders 汇总 HTTP 入口的 Wire provider。
var httpAssemblyProviders = wire.NewSet(
	nativeHTTPProviders,
	server.ProviderSet,
	provideHTTPOptions,
	provideHTTPRouteMount,
	provideAuthRouteMount,
	provideUserRouteMount,
	provideAdminRouteMount,
	provideGatewayRouteMount,
	providePaymentRouteMount,
	provideForwardedSettings,
	providePanelSettings,
	serverhttp.NewPanelSettingsHandler,
	provideRouterRuntime,
	provideRequestLog,
)
