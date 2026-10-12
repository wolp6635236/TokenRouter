package httpapi

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
	"golang.org/x/net/http/httpguts"

	"github.com/TokenFlux/TokenRouter/internal/egress"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/tierpolicy"
	"github.com/TokenFlux/TokenRouter/internal/infra/telemetry/logging"
	protocolopenai "github.com/TokenFlux/TokenRouter/internal/protocol/openai"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/upstream/openai"
)

const (
	openCodeSessionHeader        = "X-OpenCode-Session"
	OpenAICodexRoutingHintHeader = "x-codex-routing-hint"
)

// ApplyOpenCodeSessionHeader forwards the caller-owned conversation identifier
// only to OpenCode's official API origin. The caller applies this after provider
// header overrides so a per-conversation value cannot be replaced by a fixed
// provider-wide override.
func ApplyOpenCodeSessionHeader(c *gin.Context, provider *gatewayprovider.ExecutionProvider, targetURL string, headers http.Header) {
	if c == nil || c.Request == nil || provider == nil || provider.Record.Type != capability.ProviderTypeAPIKey || headers == nil {
		return
	}

	parsed, err := url.Parse(targetURL)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || !strings.EqualFold(parsed.Hostname(), "opencode.ai") {
		return
	}

	sessionID := strings.TrimSpace(c.GetHeader(openCodeSessionHeader))
	if sessionID == "" {
		return
	}
	for key := range headers {
		if strings.EqualFold(key, openCodeSessionHeader) {
			delete(headers, key)
		}
	}
	headers.Set(openCodeSessionHeader, sessionID)
}

func ResolveOpenAIUpstreamOriginator(c *gin.Context, isOfficialClient bool, routerMatch ...egress.TLSFingerprintRouterMatchResult) string {
	return ResolveOpenAIUpstreamOriginatorForClient(func() string {
		if c == nil {
			return ""
		}
		return c.GetHeader("originator")
	}, isOfficialClient, routerMatch...)
}

// ResolveOpenAIUpstreamOriginatorForClient 将路由结果交给 upstream 解析 originator。
func ResolveOpenAIUpstreamOriginatorForClient(read func() string, official bool, matches ...egress.TLSFingerprintRouterMatchResult) string {
	var match egress.TLSFingerprintRouterMatchResult
	if len(matches) > 0 {
		match = matches[0]
	}
	return openai.ResolveUpstreamOriginator(read, official, match.Matched, match.UpstreamOriginator)
}

// SetOpenAICodexRoutingHint 为 OpenAI OAuth 请求生成 Codex 后端路由提示。
// 调用方传入最终上游模型名，以及应用本地改写和过滤策略后的 serviceTier。
func SetOpenAICodexRoutingHint(headers http.Header, provider *gatewayprovider.ExecutionProvider, model string, serviceTier string) {
	if headers == nil {
		return
	}

	// 生成路由提示前删除同名头的全部大小写变体，网关写入的提示使用最终值。
	// Header.Del 只删除规范化键，入站映射还可能包含小写键。
	DeleteOpenAIHeaderEqualFold(headers, OpenAICodexRoutingHintHeader)
	if provider == nil || !provider.View().IsOpenAIOAuthLike() {
		return
	}

	model = strings.TrimSpace(model)
	if model == "" || strings.ContainsAny(model, ";=") {
		return
	}

	// Codex 的 default 表示标准路由，发送时省略服务层级。fast 规范化为 priority，flex 和 ultrafast 保持原样。
	canonicalTier := protocolopenai.ServiceTierValue(serviceTier)
	// 当前回移不含 Codex 模型目录快照，无法校验任意层级 ID，因此只发送
	// Codex 实际选择的有效层级；default、空值和其他兼容 API 值仅保留模型。
	switch canonicalTier {
	case tierpolicy.OpenAIFastTierPriority, tierpolicy.OpenAIFastTierFlex, tierpolicy.OpenAIFastTierUltrafast:
	default:
		canonicalTier = ""
	}

	hint := "model=" + model
	if canonicalTier != "" {
		hint += ";tier=" + canonicalTier
	}
	if !httpguts.ValidHeaderFieldValue(hint) {
		return
	}
	headers.Set(OpenAICodexRoutingHintHeader, hint)
}

func DeleteOpenAIHeaderEqualFold(headers http.Header, name string) {
	if headers == nil {
		return
	}
	name = strings.TrimSpace(name)
	for key := range headers {
		if strings.EqualFold(strings.TrimSpace(key), name) {
			delete(headers, key)
		}
	}
}

func SetOpenAICodexRoutingHintFromBody(headers http.Header, provider *gatewayprovider.ExecutionProvider, body []byte) {
	fields := gjson.GetManyBytes(body, "model", "service_tier")
	SetOpenAICodexRoutingHint(headers, provider, fields[0].String(), fields[1].String())
}

// LogOpenAIRoutingDiagnostics 记录网关推导的路由状态，日志字段为路由诊断值。
func LogOpenAIRoutingDiagnostics(
	ctx context.Context,
	provider *gatewayprovider.ExecutionProvider,
	transport string,
	model string,
	serviceTier string,
	hintGenerated bool,
	wsAffinityDecision string,
) {
	if ctx == nil {
		ctx = context.Background()
	}
	providerID := int64(0)
	if provider != nil {
		providerID = provider.Record.ID
	}

	logging.FromContext(ctx).Debug("openai routing decision",
		zap.String("component", "service.openai_routing"),
		zap.String("transport", strings.TrimSpace(transport)),
		zap.Int64("provider_id", providerID),
		zap.String("final_model", strings.TrimSpace(model)),
		zap.String("final_service_tier", protocolopenai.ServiceTierValue(serviceTier)),
		zap.Bool("routing_hint_generated", hintGenerated),
		zap.String("ws_affinity_decision", strings.TrimSpace(wsAffinityDecision)),
	)
}

func LogOpenAIRoutingDiagnosticsFromBody(
	ctx context.Context,
	provider *gatewayprovider.ExecutionProvider,
	transport string,
	headers http.Header,
	body []byte,
	wsAffinityDecision string,
) {
	fields := gjson.GetManyBytes(body, "model", "service_tier")
	LogOpenAIRoutingDiagnostics(
		ctx,
		provider,
		transport,
		fields[0].String(),
		fields[1].String(),
		strings.TrimSpace(headers.Get(OpenAICodexRoutingHintHeader)) != "",
		wsAffinityDecision,
	)
}
