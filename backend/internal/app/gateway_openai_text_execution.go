package app

import (
	"context"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/app/lifecycle"
	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/config"
	"github.com/TokenFlux/TokenRouter/internal/egress"
	egressprovider "github.com/TokenFlux/TokenRouter/internal/egress/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway"
	"github.com/TokenFlux/TokenRouter/internal/gateway/admission"
	gatewayhttp "github.com/TokenFlux/TokenRouter/internal/gateway/httpapi"
	"github.com/TokenFlux/TokenRouter/internal/gateway/promptpolicy"
	gatewayadapter "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/provider/selection"
	"github.com/TokenFlux/TokenRouter/internal/gateway/session"
	"github.com/TokenFlux/TokenRouter/internal/gateway/ws"
	wshttp "github.com/TokenFlux/TokenRouter/internal/gateway/ws/httpapi"
	"github.com/TokenFlux/TokenRouter/internal/infra/httpclient"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"
	"github.com/TokenFlux/TokenRouter/internal/scheduler"
	openaiws "github.com/TokenFlux/TokenRouter/internal/upstream/openai/ws"
)

// provideCompactExecutor 为上下文恢复执行器提供所需的静态参数。
func provideCompactExecutor(cfg *config.Config) *gatewayhttp.CompactExecutor {
	value := &gatewayhttp.CompactExecutor{}
	if cfg != nil {
		value.Models = gatewayadapter.CompactModels{Default: cfg.Gateway.OpenAICompactModel}
		value.LogBody = cfg.Gateway.LogUpstreamErrorBody
		value.LogBodyMaxBytes = cfg.Gateway.LogUpstreamErrorBodyMaxBytes
	}
	return value
}

// provideExecutionAgentIdentity 与所有提供商查询共用协调器和连接失效拥有者。
func provideExecutionAgentIdentity(tasks *provider.OpenAITaskCoordinator, store gatewayadapter.ExecutionProviderStore, connections *openaiws.OpenAIWSConnections) *gatewayadapter.ExecutionAgentIdentity {
	return gatewayadapter.NewExecutionAgentIdentity(tasks, store, func(ctx context.Context, value *provider.Record) (string, error) {
		return provideradapter.RegisterAgentIdentityTask(ctx, value, "https://auth.openai.com/api/accounts")
	}, connections.InvalidateProvider)
}

func provideAnthropicPromptCache() *session.AnthropicPromptCache {
	return session.NewAnthropicPromptCache(time.Now)
}

// provideOpenAITextExecutor 构造独立的文本能力，共用请求、输出和后台观察实例。
func provideOpenAITextExecutor(cfg *config.Config, store gatewayadapter.ExecutionProviderStore, identity *gatewayadapter.ExecutionAgentIdentity, credentials *provider.OpenAIExecutionCredentials, transport httpclient.UpstreamTransport, profiles *egressprovider.TLSProfiles, routers *egress.TLSFingerprintRouterService, readers *gatewayadapter.RuntimeReaders, grok *gatewayhttp.GrokExecutor, output *gatewayhttp.OpenAIResponseOutput, cache *session.AnthropicPromptCache, choices *selection.Compatible, compact *gatewayhttp.CompactExecutor, tasks *lifecycle.Tasks) *gatewayhttp.OpenAITextExecutor {
	text := openAITextExecution(cfg, store, identity, credentials, transport, profiles, routers, readers, grok, output, cache, choices.OpenAIHTTPResponseStickyTTL, compact)
	text.CodexUsage.Go = tasks.Go
	return text
}

func provideOpenAIImageBridgePolicy(cfg *config.Config) *gatewayadapter.ResponseImagePolicy {
	policy := &gatewayadapter.ResponseImagePolicy{}
	if cfg != nil {
		policy.DefaultEnabled = cfg.Gateway.CodexImageGenerationBridgeEnabled
	}
	return policy
}

func provideOpenAIEncryptedLineage(state session.OpenAIWSStateStore, choices *selection.Compatible) *gatewayhttp.OpenAIEncryptedLineage {
	return &gatewayhttp.OpenAIEncryptedLineage{Store: state, TTL: choices.SessionStickyTTL}
}

func provideOpenAIWebSockets(runtime *ws.Runtime, cfg *config.Config, connections *openaiws.OpenAIWSConnections, text *gatewayhttp.OpenAITextExecutor, prompts *promptpolicy.Service, choices *selection.Compatible, lineage *gatewayhttp.OpenAIEncryptedLineage, imagePolicy *gatewayadapter.ResponseImagePolicy, cache session.GatewayCache) *wshttp.OpenAIWebSocketExecutor {
	return wshttp.NewOpenAIWebSocketExecutor(wshttp.OpenAIWSDependencies{Runtime: runtime, Options: openAIWSExecutionOptions(cfg), Connections: connections, Requests: text.Requests, Output: text.Output, Grok: text.Grok, FastPolicy: text.FastPolicy, Prompts: prompts, Selection: choices, State: lineage.Store, Lineage: lineage, ImageBridge: imagePolicy, Cache: cache})
}

func provideOpenAIResponses(text *gatewayhttp.OpenAITextExecutor, choices *selection.Compatible, lineage *gatewayhttp.OpenAIEncryptedLineage, imagePolicy *gatewayadapter.ResponseImagePolicy) *gatewayhttp.OpenAIResponsesExecutor {
	return &gatewayhttp.OpenAIResponsesExecutor{Requests: text.Requests, Output: text.Output, Text: text, Grok: text.Grok, Lineage: lineage, ImageBridge: imagePolicy}
}

// provideOpenAIAuxiliary 复用 app 已构造的请求、输出、授权和后台观察实例。
func provideOpenAIAuxiliary(text *gatewayhttp.OpenAITextExecutor, authorization *provider.OpenAIAuthorization, activity *gatewayRequestActivity) *gatewayhttp.OpenAIAuxiliary {
	executor := &gatewayhttp.OpenAIAuxiliary{Requests: text.Requests, Output: text.Output, CodexUsage: text.CodexUsage, Authorization: authorization, Enter: activity.Enter}
	return executor
}

// provideOpenAIImages 绑定共享的请求、输出和任务跟踪器，模型冷却由提供商模块管理。
func provideOpenAIImages(text *gatewayhttp.OpenAITextExecutor, activity *gatewayRequestActivity) *gatewayhttp.OpenAIImagesExecutor {
	cooldown := &provideradapter.ImageToolCooldown{Store: text.Requests.Providers}
	if text.Requests.Readers != nil {
		cooldown.Settings = text.Requests.Readers.Provider.GetOpenAIImagesOAuthUnavailableCooldownSettings
	}
	result := &gatewayhttp.OpenAIImagesExecutor{Requests: text.Requests, Output: text.Output, Cooldown: cooldown, Enter: activity.Enter}
	return result
}

// openAITextExecution 固定注入已构造的资源，动态策略仍按请求读取。
func openAITextExecution(cfg *config.Config, store gatewayadapter.ExecutionProviderStore, identity *gatewayadapter.ExecutionAgentIdentity, credentials *provider.OpenAIExecutionCredentials, transport httpclient.UpstreamTransport, profiles *egressprovider.TLSProfiles, routers *egress.TLSFingerprintRouterService, readers *gatewayadapter.RuntimeReaders, grok *gatewayhttp.GrokExecutor, output *gatewayhttp.OpenAIResponseOutput, prompts *session.AnthropicPromptCache, ttl func() time.Duration, compact *gatewayhttp.CompactExecutor) *gatewayhttp.OpenAITextExecutor {
	policy := &provideradapter.OpenAIProbePolicy{Available: true, DefaultBrowserUserAgent: gateway.DefaultOpenAICodexUserAgent, Profiles: profiles}
	requests := &gatewayhttp.OpenAIRequests{Providers: store, Identity: identity, Credentials: credentials, Transport: transport, Profiles: profiles, Routers: routers, Readers: readers, ClientPolicy: policy}
	if routers != nil {
		policy.Routers = routers
	}
	if readers != nil {
		policy.AllowClaudeCode = readers.Gateway.IsOpenAIAllowClaudeCodeCodexPluginEnabled
		policy.BrowserUserAgent = readers.Gateway.GetOpenAICodexUserAgent
	}
	executor := &gatewayhttp.OpenAITextExecutor{Compact: compact, Requests: requests, Output: output, Grok: grok, Continuation: &session.CompatResponses{TTL: ttl}, PromptCache: prompts, CodexUsage: &provideradapter.CodexUsageObserver{Store: store}, ResponseTTL: ttl}
	if grok != nil {
		requests.Failure = grok.Failure
		requests.GrokRoutes = grok.Routes
		executor.Credentials = grok.Credentials
		executor.FastPolicy = grok.FastPolicy
		if grok.Health != nil {
			executor.CodexUsage.Throttle = grok.Health.Throttle
		}
	}
	if output != nil {
		requests.Turns = output.Turns
	}

	if cfg != nil {
		v := cfg.Security.URLAllowlist
		requests.Options = gatewayhttp.OpenAIRequestOptions{ForceCLI: cfg.Gateway.ForceCodexCLI, AllowTimeoutHeaders: cfg.Gateway.OpenAIPassthroughAllowTimeoutHeaders, URLPolicy: egress.OperatorURLPolicy{Enabled: v.Enabled, AllowInsecureHTTP: v.AllowInsecureHTTP, AllowPrivateHosts: v.AllowPrivateHosts, UpstreamHosts: v.UpstreamHosts}}
		policy.ForceCLI = cfg.Gateway.ForceCodexCLI
		executor.ForcedTemplate = cfg.Gateway.ForcedCodexInstructionsTemplate
	}
	return executor
}

// provideUnifiedTextExecutor 注入共享的执行器，跨平台切换提供商时复用连接和资源池。
func provideUnifiedTextExecutor(openai *gatewayhttp.OpenAIResponsesExecutor, anthropic *gatewayhttp.MessagesExecutor, gemini *gatewayhttp.GeminiExecutor, antigravity *gatewayhttp.AntigravityExecutor, qoder *gatewayadapter.QoderRuntime, refresh *provideradapter.QoderRequestRefresh, queue *scheduler.UserMessageQueueService, cfg *config.Config, prices *billing.PriceResolver) *gatewayhttp.UnifiedTextExecutor {
	executor := &gatewayhttp.UnifiedTextExecutor{OpenAI: openai, Anthropic: anthropic, Gemini: gemini, Antigravity: antigravity, Qoder: qoder, QoderRefresh: refresh}
	executor.Pricing = &admission.ModelPricing{Resolver: prices}
	if cfg != nil {
		executor.MessageQueueMode = cfg.Gateway.UserMessageQueue.GetEffectiveMode()
		executor.MessageQueueWait = cfg.Gateway.UserMessageQueue.WaitTimeout()
		if queue != nil {
			executor.MessageQueue = gatewayhttp.NewUserMsgQueueHelper(queue, gatewayhttp.SSEPingFormatClaude, time.Duration(cfg.Concurrency.PingInterval)*time.Second)
		}
	}
	return executor
}
