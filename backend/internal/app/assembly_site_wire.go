//go:build wireinject

package app

import (
	"github.com/google/wire"

	"github.com/TokenFlux/TokenRouter/internal/site"
	sitepostgres "github.com/TokenFlux/TokenRouter/internal/site/postgres"
)

// siteAssemblyProviders 汇总 site 模块的 Wire provider。
var siteAssemblyProviders = wire.NewSet(
	provideSiteDisplay,
	provideSitePublicHTTP,
	provideSitePublic,
	provideSitePages,
	site.NewAnnouncementService,
	sitepostgres.NewAnnouncementRepository,
	sitepostgres.NewAnnouncementReadRepository,
	provideAnnouncementUsers,
	provideAnnouncementSubscriptions,
	provideAnnouncementExpiry,
)
