//go:build wireinject

package app

import (
	"github.com/google/wire"

	teamhttp "github.com/TokenFlux/TokenRouter/internal/team/httpapi"
)

// teamHTTPProviders 汇总团队用户端和管理端 HTTP 构造函数。
var teamHTTPProviders = wire.NewSet(teamhttp.NewUserHandler, teamhttp.NewAdminHandler)
