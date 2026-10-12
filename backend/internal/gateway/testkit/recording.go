package testkit

import (
	"context"
	"log/slog"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	billingtestkit "github.com/TokenFlux/TokenRouter/internal/billing/testkit"
	"github.com/TokenFlux/TokenRouter/internal/gateway/completion"
	gatewayadapter "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	gatewaytelemetry "github.com/TokenFlux/TokenRouter/internal/gateway/telemetry"
	"github.com/TokenFlux/TokenRouter/internal/infra/telemetry/logging"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/usage"
)

// Recording 保存用量记录测试配置的依赖。
type Recording struct {
	Dependencies   completion.Dependencies
	Options        completion.RecorderOptions
	GroupPolicies  *routing.PricingConfigService
	Effects        completion.CommitEffects
	ProviderLookup func(context.Context, int64) (*provider.Record, error)
}

type recordProviders struct {
	lookup func(context.Context, int64) (*provider.Record, error)
}

// NewRecording 配置记录夹具的默认倍率和缓存期限，并构造计费计算器。
func NewRecording(logs usage.UsageLogRepository, funds completion.Store, rates billing.UserGroupRateRepository, withResolver bool) *Recording {
	calculator := billingtestkit.Calculator(nil, nil)
	fixture := &Recording{
		Options: completion.RecorderOptions{DefaultMultiplier: 1.1},
		Dependencies: completion.Dependencies{
			Calculator: calculator, Funds: funds, Logs: completion.SnapshotLogWriter(logs),
			Rates:  billing.NewGroupRateResolver(rates, nil, 30*time.Second, nil, "service.openai_gateway.test", logging.LegacyPrintf),
			Models: gatewayadapter.CompletionModels{}, Emit: gatewaytelemetry.CompletionBillingEvent, Observe: gatewaytelemetry.ObserveCompletion,
		},
		Effects: completion.CommitEffects{
			Activity: &provider.DeferredService{}, Observe: gatewaytelemetry.ObserveCompletion,
			Funds: billing.SettlementEffects{
				Background: func(_ string, fn func()) bool { go fn(); return true },
				Observe:    logging.LegacyPrintf,
				BalanceWarning: func(id int64, balance float64, err error) {
					slog.Warn("invalidate balance cache after exhausted deduction failed", "user_id", id, "new_balance", balance, "error", err)
				},
			},
		},
	}
	if withResolver {
		fixture.Dependencies.Prices = billingtestkit.PriceResolver(nil, calculator)
	}
	return fixture
}

// Core 绑定测试输入并使用 Dependencies 中的共享实例构造记录器。
func (f *Recording) Core(updater gatewayadapter.QuotaUpdater, openAI bool) *completion.Recorder {
	deps := f.Dependencies
	deps.Subscriptions, _ = deps.Funds.(completion.SubscriptionReader)
	var stats billing.ProviderStatsSource
	if f.GroupPolicies != nil {
		stats = gatewayadapter.ProviderStatsSource{Service: f.GroupPolicies}
	}
	deps.ProviderStats = billing.NewPriceResolver(nil, deps.Calculator, nil, nil, stats)
	effects := f.Effects
	if auth, ok := updater.(completion.AuthInvalidator); ok {
		effects.Auth = auth
	}
	deps.Effects = &effects
	if openAI {
		deps.Providers = recordProviders{lookup: f.ProviderLookup}
	}
	return completion.NewRecorder(deps, f.Options)
}

// RecordOpenAI 测试通过生产捕获和完成入口验证观测与资金结果。
func (f *Recording) RecordOpenAI(ctx context.Context, in *gatewayadapter.OpenAICapture) error {
	var updater gatewayadapter.QuotaUpdater
	if in != nil {
		updater = in.APIKeyService
	}
	return f.Core(updater, true).Record(ctx, gatewayadapter.CaptureOpenAI(ctx, in), true)
}

// RecordMessages 与普通 Messages 完成链共享相同捕获与记录实现。
func (f *Recording) RecordMessages(ctx context.Context, in *gatewayadapter.MessagesCapture) error {
	return f.Core(in.APIKeyService, false).Record(ctx, gatewayadapter.CaptureMessages(ctx, in), false)
}

func (p recordProviders) CredentialProvider(ctx context.Context, in completion.ProviderSnapshot) (*completion.ProviderSnapshot, error) {
	source := &provider.Record{ID: in.ID, Platform: in.Platform, Type: in.Type, ParentProviderID: in.CredentialProviderID}
	record, err := provider.ResolveCredentialRecord(ctx, p.lookup, source)
	if err != nil {
		return nil, err
	}
	if record == source {
		return &in, nil
	}
	return gatewayadapter.ProjectCompletionProvider(record), nil
}
