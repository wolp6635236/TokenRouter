//go:build wireinject

package app

import (
	"github.com/google/wire"

	teamredis "github.com/TokenFlux/TokenRouter/internal/team/rediscache"
)

// teamAssemblyProviders 汇总 team 模块的 Wire provider。
var teamAssemblyProviders = wire.NewSet(
	teamHTTPProviders,
	provideTeam,
	provideTeamRepository,
	teamredis.NewTeamInvitationLimiter,
)
