//go:build wireinject

package app

import (
	"github.com/google/wire"

	billinghttp "github.com/TokenFlux/TokenRouter/internal/billing/httpapi"
	opshttp "github.com/TokenFlux/TokenRouter/internal/ops/httpapi"
	sitehttp "github.com/TokenFlux/TokenRouter/internal/site/httpapi"
)

// nativeHTTPProviders 汇总 HTTP 构造器，各入口共用已装配的用例实例。
var nativeHTTPProviders = wire.NewSet(
	billinghttp.NewPlanHandler,
	billinghttp.NewRedeemHandler,
	billinghttp.NewSubscriptionHandler,
	billinghttp.NewAdminRedeemHandler,
	billinghttp.NewAdminSubscriptionHandler,
	sitehttp.NewAnnouncementHandler,
	sitehttp.NewAdminAnnouncementHandler,
	opshttp.NewOpsHandler,
)
