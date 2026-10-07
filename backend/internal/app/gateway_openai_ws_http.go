package app

import (
	"context"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"

	"github.com/TokenFlux/TokenRouter/internal/gateway/provider/selection"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/config"
	"github.com/TokenFlux/TokenRouter/internal/gateway/admission"
	gatewayhttp "github.com/TokenFlux/TokenRouter/internal/gateway/httpapi"
	"github.com/TokenFlux/TokenRouter/internal/gateway/httpapi/openaiattempt"
	"github.com/TokenFlux/TokenRouter/internal/gateway/httpapi/wsentry"
	"github.com/TokenFlux/TokenRouter/internal/gateway/promptpolicy"
	"github.com/TokenFlux/TokenRouter/internal/gateway/session"
	"github.com/TokenFlux/TokenRouter/internal/routing"
)

// provideResponsesWSHTTP 直接绑定 WS 用例，共享同一尝试支持、生命周期与动态数据读取。
func provideResponsesWSHTTP(
	source *gatewayhttp.OpenAIWebSocketExecutor, credentials *gatewayhttp.RequestCredentialExecutor,
	funding *admission.FundingAdmission,
	keys *apikey.APIKeyService,
	common openaiattempt.Bindings,
	prompt *promptpolicy.Service,
	blocks *session.CyberBlocks,
	cfg *config.Config,
	activity *gatewayRequestActivity,
	choices *selection.Compatible, planner *gatewayprovider.RoutePlanner,
	subscriptions *billing.SubscriptionService,
	prices *billing.PriceResolver,
) *gatewayhttp.ResponsesWSHandler {
	options := responsesWSOptions(cfg)
	b := responsesWSBindings(source, credentials, funding, keys, common, prompt, blocks, choices, planner, subscriptions)
	b.Pricing = &admission.ModelPricing{Resolver: prices}
	result := wsentry.New(options, b)
	result.BindRequestActivity(activity.Enter)
	return result
}

// responsesWSOptions 返回入站连接和首帧的静态配置及默认值。
func responsesWSOptions(cfg *config.Config) gatewayhttp.ResponsesWSOptions {
	options := gatewayhttp.ResponsesWSOptions{
		MaxProviderSwitches: 3,
		ReadLimit:           gatewayhttp.ResolveOpenAIWSClientReadLimitBytes(openAIWSExecutionOptions(cfg)),
		FirstMessageTimeout: gatewayhttp.ResolveOpenAIWSClientFirstMessageTimeout(openAIWSExecutionOptions(cfg)),
	}
	if cfg != nil {
		options.MaxIngressConnectionsPerAPIKey = cfg.Gateway.OpenAIWS.MaxIngressConnectionsPerAPIKey
		if cfg.Gateway.MaxProviderSwitches > 0 {
			options.MaxProviderSwitches = cfg.Gateway.MaxProviderSwitches
		}
	}
	return options
}

// responsesWSBindings 绑定共享状态和每轮单次执行函数。
func responsesWSBindings(source *gatewayhttp.OpenAIWebSocketExecutor, credentials *gatewayhttp.RequestCredentialExecutor, funding *admission.FundingAdmission, keys *apikey.APIKeyService, common openaiattempt.Bindings, prompt *promptpolicy.Service, blocks *session.CyberBlocks, choices *selection.Compatible, planner *gatewayprovider.RoutePlanner, subscriptions admission.SubscriptionReader) wsentry.Bindings {
	b := wsentry.Bindings{
		Common:        common,
		Subscriptions: subscriptions,
		Prompt:        prompt,
		Blocks:        blocks,
		Dependencies: gatewayhttp.OpenAIDependencies{
			Handler:     true,
			Gateway:     source != nil,
			Funding:     funding != nil,
			Keys:        keys != nil,
			Concurrency: common.Support.Concurrency != nil && common.Support.Concurrency.Service() != nil,
		},
	}
	if keys != nil {
		b.Keys = keys
	}
	if funding != nil {
		b.CheckFunding = funding.CheckKey
	}
	if source != nil {
		b.PlanRoute = func(ctx context.Context, key *apikey.APIKey, model string) routing.RoutePlan {
			return planner.PlanKey(ctx, key, model)
		}
		b.Isolate = source.EnsureSessionIsolation
		b.ReportSelection = common.Selection.ReportSelection
		b.Stop429 = stopOpenAI429
		b.Credential = credentials.Resolve
		b.ResolveRouting = choices.ResolveOpenAIWSRoutingModelForProvider
		b.BeginPreemption = source.BeginOpenAIWSIngressSessionPreemption
		b.Relay = source.ProxyResponsesWebSocketFromClient
	}
	return b
}
