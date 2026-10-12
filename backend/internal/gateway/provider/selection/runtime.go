package selection

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/egress"
	gatewayadapter "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/session"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	schedulercore "github.com/TokenFlux/TokenRouter/internal/scheduler"
)

// Generic 包含通用提供商选择所需的读取接口、资格规则和并发资源。
type Generic struct {
	options                 Options
	providerRepo            Providers
	groupRepo               Groups
	schedulerSnapshot       Snapshots
	cache                   schedulercore.StickyCache
	concurrencyService      *schedulercore.ConcurrencyService
	healthObserver          *provideradapter.UpstreamHealth
	groupPolicies           *routing.PricingConfigService
	schedulerParameters     *schedulercore.Parameters
	advancedProviderStats   *schedulercore.RuntimeStats
	freeQuotaGate           *provider.FreeQuotaGate
	window                  *billing.WindowCostGuard
	windowPrefetchAvailable bool
	rpmCache                schedulercore.RPMCache
	sessionLimitCache       schedulercore.SessionLimitCache
	setProviderError        func(context.Context, int64, string) error
}

// Compatible 为调度器提供 OpenAI/Grok 资格规则和共享状态。
type Compatible struct {
	generic             *Generic
	gemini              *Gemini
	options             Options
	providerRepo        Providers
	schedulerSnapshot   Snapshots
	schedulingGroups    func(context.Context, int64) (*routing.Group, error)
	cache               schedulercore.StickyCache
	concurrencyService  *schedulercore.ConcurrencyService
	healthObserver      *provideradapter.UpstreamHealth
	groupPolicies       *routing.PricingConfigService
	schedulerParameters *schedulercore.Parameters
	openaiProviderStats *schedulercore.RuntimeStats
	quotaSettings       *provider.QuotaSettingsCache
	freeQuotaGate       *provider.FreeQuotaGate
	newFreeQuotaGate    func() *provider.FreeQuotaGate
	runtime             *provider.RuntimeBlockState
	modelTransient      *provider.ModelTransientState
	proxyCircuit        *egress.ProxyStreamCircuit
	proxyFailOpenLogAt  atomic.Int64
	responseState       session.OpenAIWSStateStore
	stickyMetrics       *schedulercore.StickyStats
	pickerOnce          sync.Once
	picker              pickerEngine
}

// Gemini 包含 Gemini 和混合池的无槽选择依赖。
type Gemini struct {
	options               Options
	providerRepo          Providers
	groupRepo             Groups
	schedulerSnapshot     Snapshots
	cache                 schedulercore.StickyCache
	schedulerParameters   *schedulercore.Parameters
	advancedProviderStats *schedulercore.RuntimeStats
	quotaPrecheck         *provider.GeminiPrecheck
}

// DiagnosticSource 定义诊断所需的提供商读取方法，scheduler 构造诊断结果。
type DiagnosticSource interface {
	GetProvider(context.Context, int64) (*gatewayadapter.ExecutionProvider, error)
	GetGroup(context.Context, int64) (*routing.Group, error)
	ListProvidersForSchedulerScoreFilter(context.Context, string, string, string, string, int64, string) ([]gatewayadapter.ExecutionProvider, error)
	ListSchedulableProvidersForAdvancedSchedulerScore(context.Context, *int64, string) ([]gatewayadapter.ExecutionProvider, error)
}

// Diagnostics 与请求选择共用参数、反馈和资格规则，用于只读诊断。
type Diagnostics struct {
	source              DiagnosticSource
	concurrencyService  *schedulercore.ConcurrencyService
	feedback            *schedulercore.RuntimeStats
	schedulerParameters *schedulercore.Parameters
	gatewayService      *Generic
	openAIGateway       *Compatible
}

// NewGeneric 绑定共享调度状态，为每次选择建立执行目标。
// @project-doc docs/architecture/provider_scheduling_and_cache.md#advanced_scheduler_selection
func NewGeneric(deps GenericDependencies, options Options) *Generic {
	if deps.Window == nil {
		deps.Window = defaultWindowCostGuard()
	}
	return &Generic{
		options:           options,
		providerRepo:      deps.Providers,
		groupRepo:         deps.Groups,
		schedulerSnapshot: deps.Snapshot,
		cache:             deps.Cache,

		concurrencyService:  deps.Concurrency,
		healthObserver:      deps.Health,
		groupPolicies:       deps.GroupPolicies,
		schedulerParameters: deps.Parameters,

		advancedProviderStats:   deps.Feedback,
		freeQuotaGate:           deps.FreeQuota,
		window:                  deps.Window,
		windowPrefetchAvailable: deps.WindowPrefetchAvailable,

		rpmCache:          deps.RPM,
		sessionLimitCache: deps.Sessions,
		setProviderError:  deps.SetProviderError,
	}
}

func NewCompatible(deps CompatibleDependencies, options Options) *Compatible {
	if deps.RuntimeBlocks == nil {
		deps.RuntimeBlocks = provider.NewRuntimeBlockState(time.Now)
	}
	if deps.ModelTransient == nil {
		deps.ModelTransient = provider.NewModelTransientState(0)
	}
	if deps.ProxyCircuit == nil {
		deps.ProxyCircuit = egress.NewProxyStreamCircuit(egress.DefaultProxyStreamCircuitSettings())
	}
	if deps.StickyStats == nil {
		deps.StickyStats = &schedulercore.StickyStats{}
	}
	var groups func(context.Context, int64) (*routing.Group, error)
	if deps.Groups != nil {
		groups = deps.Groups.GetByID
	}
	return &Compatible{
		generic: deps.Generic, gemini: deps.Gemini,
		options:           options,
		providerRepo:      deps.Providers,
		schedulerSnapshot: deps.Snapshot,
		schedulingGroups:  groups,
		cache:             deps.Cache,

		concurrencyService:  deps.Concurrency,
		healthObserver:      deps.Health,
		groupPolicies:       deps.GroupPolicies,
		schedulerParameters: deps.Parameters,

		openaiProviderStats: deps.Feedback,
		quotaSettings:       deps.QuotaSettings,
		freeQuotaGate:       deps.FreeQuota,
		newFreeQuotaGate:    deps.NewAdvancedFreeQuota,

		runtime:        deps.RuntimeBlocks,
		modelTransient: deps.ModelTransient,
		proxyCircuit:   deps.ProxyCircuit,
		responseState:  deps.Responses,
		stickyMetrics:  deps.StickyStats,
	}
}

func NewGemini(deps GeminiDependencies, options Options) *Gemini {
	return &Gemini{
		options:           options,
		providerRepo:      deps.Providers,
		groupRepo:         deps.Groups,
		schedulerSnapshot: deps.Snapshot,
		cache:             deps.Cache,

		schedulerParameters:   deps.Parameters,
		advancedProviderStats: deps.Feedback,
		quotaPrecheck:         deps.QuotaPrecheck,
	}
}

func NewDiagnostics(source DiagnosticSource, shared Shared, generic *Generic, compatible *Compatible) *Diagnostics {
	return &Diagnostics{
		source:              source,
		concurrencyService:  shared.Concurrency,
		feedback:            shared.Feedback,
		schedulerParameters: shared.Parameters,

		gatewayService: generic,
		openAIGateway:  compatible,
	}
}
