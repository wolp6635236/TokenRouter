package provider

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/gateway/completion"
	forwardcore "github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	"github.com/TokenFlux/TokenRouter/internal/identity"
	"github.com/TokenFlux/TokenRouter/internal/infra/telemetry"
	"github.com/TokenFlux/TokenRouter/internal/protocol/openai"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/scheduler"
)

// MessagesCapture 仅供同步捕获使用；CaptureMessages 返回的快照才可以提交给异步 worker。
type MessagesCapture struct {
	Result             *forwardcore.MessagesResult
	APIKey             *apikey.APIKey
	User               *identity.User
	Provider           *provider.Record
	Subscription       *billing.UserSubscription // 可选：订阅信息
	InboundEndpoint    string                    // 入站端点（客户端请求路径）
	UpstreamEndpoint   string                    // 上游端点（标准化后的上游路径）
	UserAgent          string                    // 请求的 User-Agent
	IPAddress          string                    // 请求的客户端 IP 地址
	ClientSessionID    string                    // 客户端显式会话标识（session_id / X-Session-Id 等请求头），仅用于用量行会话关联
	RequestPayloadHash string                    // 请求体语义哈希，用于降低 request_id 误复用时的静默误去重风险
	RequestBody        []byte                    // 原始请求体，用于解析客户端请求的计费推理档位
	ForceCacheBilling  bool                      // 强制缓存计费：将 input_tokens 转为 cache_read 计费（用于粘性会话切换）
	APIKeyService      QuotaUpdater              // 可选：用于更新API Key配额

	routing.PricingUsageFields // 分组映射信息（由 handler 在 Forward 前解析）
}

// QuotaUpdater 保留调用方已提供额度更新能力的判定，捕获过程不会调用它。
type QuotaUpdater interface {
	UpdateQuotaUsed(ctx context.Context, apiKeyID int64, cost float64) error
	UpdateRateLimitUsage(ctx context.Context, apiKeyID int64, cost float64) error
}

// OpenAICapture 包含同步捕获的输入和 HTTP 请求或 WS turn 的计费时刻。
type OpenAICapture struct {
	Result             *forwardcore.OpenAIResult
	APIKey             *apikey.APIKey
	User               *identity.User
	Provider           *provider.Record
	Subscription       *billing.UserSubscription
	InboundEndpoint    string
	UpstreamEndpoint   string
	UserAgent          string // 请求的 User-Agent
	IPAddress          string // 请求的客户端 IP 地址
	ClientSessionID    string // 客户端显式会话标识（session_id / X-Session-Id 等请求头），仅用于用量行会话关联
	RequestPayloadHash string
	RequestBody        []byte // 原始请求体，用于解析客户端请求的计费推理档位
	// PricingAt 是 WS turn 开始时刻；普通 HTTP 调用留空并在记录时取当前时间。
	PricingAt     time.Time
	APIKeyService QuotaUpdater
	CyberBlocked  bool
	// NativeCompactionV2 表示请求体运行时被识别为原生远程 compaction v2。
	NativeCompactionV2 bool
	routing.PricingUsageFields
}

// CyberCapture 是 forward 错误路径中 cyber_policy 命中的补记用量入参。
type CyberCapture struct {
	APIKey             *apikey.APIKey
	Provider           *provider.Record
	Subscription       *billing.UserSubscription
	RequestID          string
	Model              string
	Stream             bool
	InputTokens        int
	OutputTokens       int
	InboundEndpoint    string
	UpstreamEndpoint   string
	UserAgent          string
	IPAddress          string
	ClientSessionID    string
	RequestPayloadHash string
	APIKeyService      QuotaUpdater

	// NativeCompactionV2 保留错误路径中原生 compaction 标记。
	NativeCompactionV2 bool
	routing.PricingUsageFields
}

func CompletionBillingKey(ctx context.Context, upstreamRequestID string) string {
	return completion.ResolveBillingKey(RequestIdentity(ctx, upstreamRequestID, ""), scheduler.GenerateRequestID)
}

// StableAudioBillingRequestID 为单次 TTS/STT HTTP 调用生成持久用量去重键，优先沿用上游请求 ID。
func StableAudioBillingRequestID(upstreamRequestID string) string {
	return completion.StableAudioRequestID(upstreamRequestID, scheduler.GenerateRequestID)
}

// StableRealtimeBillingRequestID 为单个 Realtime WebSocket 会话生成持久用量去重键。
func StableRealtimeBillingRequestID(sessionID string) string {
	return completion.StableRealtimeRequestID(sessionID, scheduler.GenerateRequestID)
}

func CompletionPayloadFingerprint(ctx context.Context, requestPayloadHash string) string {
	return completion.PayloadFingerprint(RequestIdentity(ctx, "", requestPayloadHash))
}

// CaptureMessages 在提交完成记录时保存用量和付款主体快照。
func CaptureMessages(ctx context.Context, in *MessagesCapture) *completion.Input {
	if in == nil {
		return nil
	}
	out := &completion.Input{
		Result:             ProjectMessagesCompletionResult(in.Result, in.Provider),
		APIKey:             ProjectCompletionKey(in.APIKey),
		User:               ProjectCompletionPayer(in.User),
		Provider:           ProjectCompletionProvider(in.Provider),
		Subscription:       in.Subscription,
		InboundEndpoint:    in.InboundEndpoint,
		UpstreamEndpoint:   in.UpstreamEndpoint,
		UserAgent:          in.UserAgent,
		IPAddress:          in.IPAddress,
		ClientSessionID:    in.ClientSessionID,
		RequestPayloadHash: CompletionPayloadFingerprint(ctx, in.RequestPayloadHash),

		ForceCacheBilling:  in.ForceCacheBilling,
		QuotaUpdates:       in.APIKeyService != nil,
		PricingUsageFields: in.PricingUsageFields,
	}
	if in.Result != nil {
		captureRequestIdentity(ctx, out, in.Result.RequestID)
		out.RequestedReasoningEffort = requeststate.CanonicalRequestedReasoningEffort(in.RequestBody)
	}
	return completion.Snapshot(out)
}

// CaptureOpenAI 保留 WS turn 固定时刻及独立账单模型链。
func CaptureOpenAI(ctx context.Context, in *OpenAICapture) *completion.Input {
	if in == nil {
		return nil
	}
	out := &completion.Input{
		ForceCacheBilling:  in.Result != nil && in.Result.NativeUsage != nil && requeststate.IsForceCacheBilling(ctx),
		Result:             ProjectOpenAICompletionResult(in.Result, in.Provider),
		APIKey:             ProjectCompletionKey(in.APIKey),
		User:               ProjectCompletionPayer(in.User),
		Provider:           ProjectCompletionProvider(in.Provider),
		Subscription:       in.Subscription,
		InboundEndpoint:    in.InboundEndpoint,
		UpstreamEndpoint:   in.UpstreamEndpoint,
		UserAgent:          in.UserAgent,
		IPAddress:          in.IPAddress,
		ClientSessionID:    in.ClientSessionID,
		RequestPayloadHash: CompletionPayloadFingerprint(ctx, in.RequestPayloadHash),

		QuotaUpdates:             in.APIKeyService != nil,
		CyberBlocked:             in.CyberBlocked,
		NativeCompactionV2:       in.NativeCompactionV2,
		PricingAt:                in.PricingAt,
		PricingUsageFields:       in.PricingUsageFields,
		RequestedReasoningEffort: requeststate.CanonicalRequestedReasoningEffort(in.RequestBody),
	}
	if in.Result != nil {
		captureRequestIdentity(ctx, out, in.Result.RequestID)
	}
	return completion.Snapshot(out)
}

func RequestIdentity(ctx context.Context, upstream, payload string) completion.RequestIdentity {
	out := completion.RequestIdentity{Upstream: upstream, PayloadHash: payload}
	if ctx != nil {
		out.Client, _ = ctx.Value(telemetry.ClientRequestID).(string)
		out.Local, _ = ctx.Value(telemetry.RequestID).(string)
	}
	return out
}

// CaptureCyber 在提交请求时复制安全策略事件和身份信息，供异步任务使用。
func CaptureCyber(ctx context.Context, in CyberCapture) *completion.Input {
	if in.APIKey == nil || in.APIKey.User == nil || in.Provider == nil || strings.TrimSpace(in.Model) == "" {
		return nil
	}
	result := &forwardcore.OpenAIResult{
		RequestID: in.RequestID,
		Model:     strings.TrimSpace(in.Model),
		Usage: openai.ForwardUsage{
			InputTokens:  in.InputTokens,
			OutputTokens: in.OutputTokens,
		},
		Stream:   in.Stream,
		Duration: 0,
	}
	return CaptureOpenAI(ctx, &OpenAICapture{
		Result:             result,
		APIKey:             in.APIKey,
		User:               in.APIKey.User,
		Provider:           in.Provider,
		Subscription:       in.Subscription,
		InboundEndpoint:    in.InboundEndpoint,
		UpstreamEndpoint:   in.UpstreamEndpoint,
		UserAgent:          in.UserAgent,
		IPAddress:          in.IPAddress,
		ClientSessionID:    in.ClientSessionID,
		RequestPayloadHash: in.RequestPayloadHash,
		APIKeyService:      in.APIKeyService,

		PricingUsageFields: in.PricingUsageFields,
		CyberBlocked:       true,
		NativeCompactionV2: in.NativeCompactionV2,
	})
}

// captureRequestIdentity 在请求结束前拆分排障标识和计费去重键。
func captureRequestIdentity(ctx context.Context, out *completion.Input, upstreamID string) {
	out.BillingKey = CompletionBillingKey(ctx, upstreamID)
	out.BillingKey = out.SettlementKey()
	out.RequestID = telemetry.RequestIDValue(ctx)
	if out.RequestID == "" || out.Result.VideoCount > 0 {
		var keyID int64
		if out.APIKey != nil {
			keyID = out.APIKey.ID
		}
		out.RequestID = telemetry.StableRequestID(fmt.Sprintf("%d/%s", keyID, out.BillingKey))
	}
	childRequest := telemetry.IsChildRequest(ctx)
	if out.RequestID == telemetry.RequestIDValue(ctx) {
		telemetry.UpdateRequest(ctx, func(record *telemetry.RequestRecord) {
			record.Model = out.Result.Model
			if out.Provider != nil {
				record.ProviderID = out.Provider.ID
				record.Platform = out.Provider.Platform
				attempt := telemetry.RequestAttempt{Number: len(record.Attempts) + 1, ProviderID: out.Provider.ID, Outcome: "completed", DurationMs: out.Result.Duration.Milliseconds()}
				if out.Result.UpstreamRequestID != nil {
					attempt.RequestID = *out.Result.UpstreamRequestID
				}
				if !childRequest && len(record.Attempts) < 256 {
					record.Attempts = append(record.Attempts, attempt)
				}
			}
		})
		telemetry.AddRequestAlias(ctx, "billing", out.BillingKey)
		if !completion.ForcedRequestID(upstreamID) && !strings.HasPrefix(upstreamID, "x_search:") {
			telemetry.AddRequestAlias(ctx, "upstream", upstreamID)
		}
		telemetry.AddRequestAlias(ctx, "upstream", out.Result.ResponseID)
		if out.Result.UpstreamRequestID != nil {
			telemetry.AddRequestAlias(ctx, "upstream", *out.Result.UpstreamRequestID)
		}
	}
	if out.RequestID != telemetry.RequestIDValue(ctx) {
		record := telemetry.RequestRecord{RequestID: out.RequestID, State: "completed", Model: out.Result.Model, StartedAt: out.PricingAt}
		if record.StartedAt.IsZero() {
			record.StartedAt = time.Now().Add(-out.Result.Duration)
		}
		record.DurationMs = out.Result.Duration.Milliseconds()
		record.Aliases = []telemetry.RequestAlias{{Kind: "billing", Value: out.BillingKey}}
		if upstreamID != "" {
			record.Aliases = append(record.Aliases, telemetry.RequestAlias{Kind: "upstream", Value: upstreamID})
		}
		if out.APIKey != nil {
			record.UserID = out.APIKey.ActorUserID
			record.APIKeyID = out.APIKey.ID
			if out.APIKey.TeamID != nil {
				record.TeamID = *out.APIKey.TeamID
			}
		}
		if out.Provider != nil {
			record.ProviderID = out.Provider.ID
			record.Platform = out.Provider.Platform
		}
		telemetry.RecordRelatedRequest(ctx, record)
	}
}
