package completion

import (
	"context"
	"fmt"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/billing/pricing"
	"github.com/TokenFlux/TokenRouter/internal/infra/telemetry"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/usage"
)

const (
	BillingModeToken              = pricing.BillingModeToken
	BillingModeImage              = pricing.BillingModeImage
	BillingModeVideo              = pricing.BillingModeVideo
	BillingModePerRequest         = pricing.BillingModePerRequest
	BillingTypeBalance            = usage.BillingTypeBalance
	BillingTypeSubscription       = usage.BillingTypeSubscription
	RequestTypeCyberBlocked       = usage.RequestTypeCyberBlocked
	BillingModelSourceRequested   = routing.BillingModelSourceRequested
	BillingModelSourceUpstream    = routing.BillingModelSourceUpstream
	BillingModelSourceGroupMapped = routing.BillingModelSourceGroupMapped
)

var ErrModelPricingUnavailable = pricing.ErrModelPricingUnavailable

// Result 仅保留完成处理需要的已观测结果，不携带响应体、HTTP Header 或平台执行器。
type Result struct {
	// UpstreamResponseModel 是协议转换前的上游模型声明；空值表示未声明。
	UpstreamResponseModel string
	// NativeUsage 表示执行器已将输入与缓存用量拆成独立分桶。
	NativeUsage                                                                  bool
	RequestID, ResponseID, Model, BillingModel, UpstreamModel                    string
	UpstreamRequestID                                                            *string
	Usage                                                                        TokenUsage
	ServiceTier, ReasoningEffort, RequestedReasoningEffort                       *string
	UpstreamResponseServiceTier                                                  string
	Stream, OpenAIWSMode                                                         bool
	Duration                                                                     time.Duration
	FirstTokenMs                                                                 *int
	ImageCount, VideoCount, VideoDurationSeconds, WebSearchCalls, SearchCount    int
	ImageSize, ImageInputSize, ImageOutputSize, ImageSizeSource, VideoResolution string
	ImageOutputSizes                                                             []string
	ImageSizeBreakdown                                                           map[string]int
	AudioUsage                                                                   *AudioUsage
}

// TokenUsage 区分未缓存输入和供应商返回的总输入，各入口负责转换计量分桶。
type TokenUsage struct {
	InputTokens, OutputTokens, CacheCreationInputTokens, CacheReadInputTokens         int
	CacheCreation5mTokens, CacheCreation1hTokens, ImageInputTokens, ImageOutputTokens int
	Speed                                                                             string
}
type AudioUsage struct {
	Mode            string
	DurationOrUnits float64
}

// PayerSnapshot 分别记录资金主体和行为主体。
type PayerSnapshot struct {
	ID           int64
	Balance      float64
	Notification *billing.UserSummary
}

// ProviderSnapshot 不携带凭据或健康可变字段。
type ProviderSnapshot struct {
	CacheTTLOverrideEnabled                                     bool
	CacheTTLOverrideTarget                                      string
	AnthropicOAuthOrSetupToken                                  bool
	Platform                                                    string
	ID                                                          int64
	Type                                                        string
	RateMultiplier                                              float64
	OpenAI, CNProvider, OAuthLike, QuotaEligible, HasQuotaLimit bool
	// CredentialProviderID 用于读取影子的母提供商。
	CredentialProviderID *int64
	Notification         *billing.QuotaNotifyProvider
}

// KeySnapshot 冻结请求取得的资金绑定；行为用户和付款用户不能互相替代。
type KeySnapshot struct {
	ActorUserPresent                         bool
	ID                                       int64
	Key                                      string
	ActorUserID                              int64
	GroupID, TeamID, PreferredSubscriptionID *int64
	BillingMode                              string
	Quota                                    float64
	HasRateLimits                            bool
	Group                                    *GroupSnapshot
}

// GroupSnapshot 保存完成计费所需的分组字段。
type GroupSnapshot struct {
	ID                                      int64
	RateMultiplier                          float64
	PeakRateEnabled                         bool
	PeakStart, PeakEnd                      string
	PeakRateMultiplier                      float64
	Location                                *time.Location
	FreeOpenAIFast, SupportsOpenAIFast      bool
	WebSearchPricePerCall, SearchPricePer1k *float64
	AudioPrice                              *pricing.AudioPriceConfig
}

// Input 保存异步完成所需的数据，调用方先调用 Snapshot 再提交队列。
type Input struct {
	// BillingKey 是资金操作的去重键，使用记录按 RequestID 关联请求。
	BillingKey                                                               string
	Result                                                                   *Result
	APIKey                                                                   *KeySnapshot
	User                                                                     *PayerSnapshot
	Provider                                                                 *ProviderSnapshot
	Subscription                                                             *billing.UserSubscription
	InboundEndpoint, UpstreamEndpoint, UserAgent, IPAddress, ClientSessionID string
	RequestID, RequestPayloadHash, CacheOverrideTarget                       string
	RequestedReasoningEffort                                                 *string
	ForceCacheBilling, QuotaUpdates, CyberBlocked, NativeCompactionV2        bool
	PricingAt                                                                time.Time
	PricingUsageFields
}

type (
	PricingUsageFields = routing.PricingUsageFields
	UsageLog           = usage.UsageLog
	UsageTokens        = pricing.UsageTokens
	CostBreakdown      = pricing.CostBreakdown
	ResolvedPricing    = pricing.ResolvedPricing
	CostInput          = billing.CostInput
	PricingInput       = billing.PricingInput
)

type PricingOptions struct{ PricingAt time.Time }

// Store 保持普通结算闭合事务，不把环境中的外层事务隐式传给资金参与者。
type Store interface {
	Apply(context.Context, *billing.UsageBillingCommand) (*billing.UsageBillingApplyResult, error)
}
type SubscriptionReader interface {
	ResolveUsableSubscriptionForGroup(context.Context, int64, int64) (*billing.UserSubscription, error)
}
type RateReader interface {
	Resolve(context.Context, int64, int64, float64) float64
}
type ProviderReader interface {
	CredentialProvider(context.Context, ProviderSnapshot) (*ProviderSnapshot, error)
}
type (
	HealthObserver  interface{ ResetOpenAI403Counter(context.Context, int64) }
	ModelCandidates interface {
		Candidates(string, ...string) []string
	}
)

type LogWriter interface {
	Create(context.Context, *usage.UsageLog) (bool, error)
}
type BestEffortLogWriter interface {
	CreateBestEffort(context.Context, *usage.UsageLog) error
}
type ProviderStats interface {
	ResolveProviderStats(context.Context, billing.ProviderStatsCostInput) *float64
}

// Effects 根据已提交的资金结果更新缓存并发送通知。
type Effects interface {
	ProviderUsed(int64)
	InvalidateAuth(context.Context, string)
	Settled(SettlementInput, *billing.UsageBillingApplyResult)
}

// CacheInjectionPolicy 读取 Anthropic 一小时缓存 TTL 注入开关。
type CacheInjectionPolicy interface{ IsAnthropicCacheTTL1hInjectionEnabled(context.Context) bool }

// BillingEvent 将计费日志字段交给日志适配器。
type BillingEvent struct {
	Kind, Component, RequestID, RequestedModel, MappedModel, UpstreamModel, Model, Platform string
	RequestedTier, ObservedTier, BilledTier                                                 string
	Models                                                                                  []string
	KeyID, ProviderID                                                                       int64
	GroupID                                                                                 *int64
	SearchCount                                                                             int
	Err                                                                                     error
}

// Dependencies 提供完成器所需的读写接口和 billing 计算器。
type Dependencies struct {
	RequestRecords func(telemetry.RequestRecord)
	Emit           func(BillingEvent)
	CacheInjection CacheInjectionPolicy
	Calculator     *billing.Calculator
	Prices         *billing.PriceResolver
	ProviderStats  ProviderStats
	Funds          Store
	Subscriptions  SubscriptionReader
	Rates          RateReader
	Providers      ProviderReader
	Health         HealthObserver
	Models         ModelCandidates
	Logs           LogWriter
	Effects        Effects
	Observe        func(string, string)
}
type RecorderOptions struct {
	DefaultMultiplier float64
	Now               func() time.Time
}
type Recorder struct {
	requestRecords    func(telemetry.RequestRecord)
	emit              func(BillingEvent)
	cacheInjection    CacheInjectionPolicy
	billingService    *billing.Calculator
	resolver          *billing.PriceResolver
	stats             ProviderStats
	store             Store
	subscriptions     SubscriptionReader
	rates             RateReader
	providers         ProviderReader
	health            HealthObserver
	models            ModelCandidates
	logs              LogWriter
	effects           Effects
	observe           func(string, string)
	defaultMultiplier float64
	now               func() time.Time
}

func (g *GroupSnapshot) PeakMultiplierAt(at time.Time) float64 {
	if g.Location != nil {
		at = at.In(g.Location)
	}
	return (&pricing.BillingSettings{PeakRateEnabled: g.PeakRateEnabled, PeakStart: g.PeakStart, PeakEnd: g.PeakEnd, PeakRateMultiplier: g.PeakRateMultiplier}).PeakMultiplierAt(at)
}

// NewRecorder 使用 app 传入的缓存、队列和服务实例构造记录器。
func NewRecorder(d Dependencies, o RecorderOptions) *Recorder {
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Recorder{
		requestRecords:    d.RequestRecords,
		emit:              d.Emit,
		cacheInjection:    d.CacheInjection,
		billingService:    d.Calculator,
		resolver:          d.Prices,
		stats:             d.ProviderStats,
		store:             d.Funds,
		subscriptions:     d.Subscriptions,
		rates:             d.Rates,
		providers:         d.Providers,
		health:            d.Health,
		models:            d.Models,
		logs:              d.Logs,
		effects:           d.Effects,
		observe:           d.Observe,
		defaultMultiplier: o.DefaultMultiplier,
		now:               o.Now,
	}
}

// Record 记录已满足提交条件的完成结果，openAI 指定总输入分桶和媒体计价方式。
func (s *Recorder) Record(ctx context.Context, input *Input, openAI bool) error {
	input = Snapshot(input)
	if openAI && (input == nil || input.Result == nil || !input.Result.NativeUsage) {
		return s.RecordOpenAI(ctx, input)
	}
	return s.RecordAnthropic(ctx, input, &PricingOptions{})
}

func (s *Recorder) printf(component, format string, args ...any) {
	if s.observe != nil {
		s.observe(component, fmt.Sprintf(format, args...))
	}
}
