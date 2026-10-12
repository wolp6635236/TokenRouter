package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	"github.com/TokenFlux/TokenRouter/internal/app/lifecycle"
	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/config"
	"github.com/TokenFlux/TokenRouter/internal/gateway"
	"github.com/TokenFlux/TokenRouter/internal/gateway/completion"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	gatewaytelemetry "github.com/TokenFlux/TokenRouter/internal/gateway/telemetry"
	"github.com/TokenFlux/TokenRouter/internal/infra/telemetry/logging"
	"github.com/TokenFlux/TokenRouter/internal/pkg/timezone"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	providerpostgres "github.com/TokenFlux/TokenRouter/internal/provider/postgres"
	"github.com/TokenFlux/TokenRouter/internal/requestlog"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/usage"
)

// GatewayCompletionRecorders 包含两种完成记录器，各自使用独立的倍率缓存。
type GatewayCompletionRecorders struct{ Forward, OpenAI *completion.Recorder }

// completionProviders 在统计影子提供商用量时读取母提供商，并返回完成记录需要的统计字段。
type completionProviders struct {
	store *providerpostgres.ProviderStore
}

type completionHealth struct{ core *provider.HealthService }

// ProvideGatewayCompletionRecorders 为完成记录器组合价格、资金、统计和提交后操作。
func ProvideGatewayCompletionRecorders(
	requests *requestlog.Service,
	rates *gatewayBillingRates,
	calculator *billing.Calculator,
	prices *billing.PriceResolver,
	funds completion.Store,
	logs usage.UsageLogRepository,
	modelConfigs *routing.PricingConfigService,
	eligibility *billing.Eligibility,
	deferred *provider.DeferredService,
	notifications *billing.BalanceNotifyService,
	keys *apikey.APIKeyService,
	providers *providerpostgres.ProviderStore,
	health *providerHealthRuntime,
	settings *gateway.RuntimeSettings,
	tasks *lifecycle.Tasks,
	cfg *config.Config,
) GatewayCompletionRecorders {
	options := completion.RecorderOptions{DefaultMultiplier: 1, Now: timezone.NewCalendar(time.Local).Now}
	if cfg != nil {
		options.DefaultMultiplier = cfg.Default.RateMultiplier
	}
	subscriptions, _ := funds.(completion.SubscriptionReader)
	effects := func() *completion.CommitEffects {
		return gatewayCommitEffects(deferred, eligibility, notifications, keys, tasks, cfg)
	}
	stats := func() *billing.PriceResolver {
		var source billing.ProviderStatsSource
		if modelConfigs != nil {
			source = gatewayprovider.ProviderStatsSource{Service: modelConfigs}
		}
		return billing.NewPriceResolver(nil, calculator, nil, nil, source)
	}
	common := func(rate completion.RateReader) completion.Dependencies {
		return completion.Dependencies{
			RequestRecords: requests.Observe,
			Emit:           gatewaytelemetry.CompletionBillingEvent,
			Calculator:     calculator,
			Prices:         prices,
			ProviderStats:  stats(),
			Funds:          funds,
			Subscriptions:  subscriptions,
			Rates:          rate,
			Models:         gatewayprovider.CompletionModels{},
			Logs:           completion.SnapshotLogWriter(logs),
			Effects:        effects(),
			Observe:        gatewaytelemetry.ObserveCompletion,
		}
	}
	forward := common(rates.Forward)
	if settings != nil {
		forward.CacheInjection = settings
	}
	openai := common(rates.OpenAI)
	openai.Providers = completionProviders{store: providers}
	openai.Health = completionHealth{core: health.Health}
	return GatewayCompletionRecorders{Forward: completion.NewRecorder(forward, options), OpenAI: completion.NewRecorder(openai, options)}
}

func (p completionProviders) CredentialProvider(ctx context.Context, in completion.ProviderSnapshot) (*completion.ProviderSnapshot, error) {
	original := &provider.Record{ID: in.ID, Platform: in.Platform, Type: in.Type, ParentProviderID: in.CredentialProviderID}
	value, err := provider.ResolveCredentialRecord(ctx, p.store.GetByID, original)
	if err != nil {
		return nil, err
	}
	if value == original {
		return &in, nil
	}
	return gatewayprovider.ProjectCompletionProvider(value), nil
}

func (p completionHealth) ResetOpenAI403Counter(ctx context.Context, id int64) {
	p.core.ResetForbiddenCounter(ctx, id)
}

// gatewayCommitEffects 为两种完成流程分别创建提交后操作对象，后台任务由 app 统一跟踪。
func gatewayCommitEffects(deferred *provider.DeferredService, eligibility *billing.Eligibility, notifications *billing.BalanceNotifyService, keys *apikey.APIKeyService, tasks *lifecycle.Tasks, cfg *config.Config) *completion.CommitEffects {
	value := &completion.CommitEffects{
		Activity: deferred,
		Observe:  gatewaytelemetry.ObserveCompletion,
		Funds: billing.SettlementEffects{
			Cache:      eligibility,
			Background: tasks.Go,
			Observe:    logging.LegacyPrintf,
			BalanceWarning: func(id int64, balance float64, err error) {
				slog.Warn("invalidate balance cache after exhausted deduction failed", "user_id", id, "new_balance", balance, "error", err)
			},
		},
	}
	value.Auth = keys
	if notifications != nil {
		value.Notifications = notifications
	}
	return value
}
