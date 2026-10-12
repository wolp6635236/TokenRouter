package selection

import (
	"context"
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

// Providers 限定原候选查询和完整目标读取，不向选择器开放凭据刷新、CRUD 或资金写入。
type Providers interface {
	GetByID(context.Context, int64) (*gatewayadapter.ExecutionProvider, error)
	ListSchedulableByPlatform(context.Context, string) ([]gatewayadapter.ExecutionProvider, error)
	ListSchedulableByGroupIDAndPlatform(context.Context, int64, string) ([]gatewayadapter.ExecutionProvider, error)
	ListSchedulableUngroupedByPlatform(context.Context, string) ([]gatewayadapter.ExecutionProvider, error)
	ListSchedulableByPlatforms(context.Context, []string) ([]gatewayadapter.ExecutionProvider, error)
	ListSchedulableByGroupIDAndPlatforms(context.Context, int64, []string) ([]gatewayadapter.ExecutionProvider, error)
	ListSchedulableUngroupedByPlatforms(context.Context, []string) ([]gatewayadapter.ExecutionProvider, error)
}

// Groups 保留轻量分组与完整设置的不同读取时点。
type Groups interface {
	GetByID(context.Context, int64) (*routing.Group, error)
	GetByIDLite(context.Context, int64) (*routing.Group, error)
}

// Snapshots 定义提供商快照读取方法，实现方负责缓存解码。
type Snapshots interface {
	GetProvider(context.Context, int64) (*provider.Record, error)
	ListProviders(context.Context, *int64, string, bool) ([]provider.Record, bool, error)
}

// Reads 包含提供商、分组和快照的数据来源。
type Reads struct {
	Providers Providers
	Groups    Groups
	Snapshot  Snapshots
}

// Shared 包含 app 构造的反馈、计数和配置实例，基础与高级选择共用这些实例。
type Shared struct {
	Cache         schedulercore.StickyCache
	Concurrency   *schedulercore.ConcurrencyService
	Health        *provideradapter.UpstreamHealth
	GroupPolicies *routing.PricingConfigService
	Parameters    *schedulercore.Parameters
	Feedback      *schedulercore.RuntimeStats
}

// GenericDependencies 保留窗口费用、RPM 与会话各自的作用域。
type GenericDependencies struct {
	Reads
	Shared
	Window                  *billing.WindowCostGuard
	WindowPrefetchAvailable bool
	RPM                     schedulercore.RPMCache
	Sessions                schedulercore.SessionLimitCache
	FreeQuota               *provider.FreeQuotaGate
	SetProviderError        func(context.Context, int64, string) error
}

// CompatibleDependencies 包含兼容选择所需的响应归属、健康状态和配额观测依赖。
type CompatibleDependencies struct {
	Generic *Generic
	Gemini  *Gemini
	Reads
	Shared
	Responses            session.OpenAIWSStateStore
	QuotaSettings        *provider.QuotaSettingsCache
	RuntimeBlocks        *provider.RuntimeBlockState
	ModelTransient       *provider.ModelTransientState
	ProxyCircuit         *egress.ProxyStreamCircuit
	StickyStats          *schedulercore.StickyStats
	FreeQuota            *provider.FreeQuotaGate
	NewAdvancedFreeQuota func() *provider.FreeQuotaGate
}

// GeminiDependencies 复用既有配额批量预检，不合并普通与混合池的选择顺序。
type GeminiDependencies struct {
	Reads
	Shared
	QuotaPrecheck *provider.GeminiPrecheck
}

// Options 包含启动配置，app 负责区分未配置和零值。
type Options struct {
	Scheduling        schedulercore.FlowOptions
	DebugRouting      bool
	StickyTTL         time.Duration
	ResponseTTL       time.Duration
	ReadLegacySticky  bool
	WriteLegacySticky bool
}

// DefaultOptions 返回未提供进程配置时的默认值。
func DefaultOptions() Options {
	return Options{
		Scheduling: schedulercore.FlowOptions{StickySessionMaxWaiting: 3, StickySessionWaitTimeout: 45 * time.Second, FallbackWaitTimeout: 30 * time.Second, FallbackMaxWaiting: 100, LoadBatchEnabled: true},

		StickyTTL:         time.Hour,
		ResponseTTL:       time.Hour,
		ReadLegacySticky:  true,
		WriteLegacySticky: true,
	}
}
