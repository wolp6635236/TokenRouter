package provider

import (
	"slices"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

const (
	OpenAIEndpointCapabilityTextGeneration OpenAIEndpointCapability = "text_generation"
	OpenAIEndpointCapabilityEmbeddings     OpenAIEndpointCapability = "embeddings"
	OpenAIEndpointCapabilityAlphaSearch    OpenAIEndpointCapability = "alpha_search"
	// OpenAIEndpointCapabilityLive 表示仅 ChatGPT OAuth 提供商支持的 Frameless Live 能力。
	OpenAIEndpointCapabilityLive OpenAIEndpointCapability = "live"
	// OpenAIEndpointCapabilityGrokMediaGeneration 用于排除已禁用或计费资格
	// 探测遭拒的 Grok 提供商；视频状态查询不要求该能力，以便继续查询已提交的任务。
	OpenAIEndpointCapabilityGrokMediaGeneration OpenAIEndpointCapability = "grok_media_generation"
	// OpenAIEndpointCapabilityResponses 表示上游确实提供 /v1/responses 端点。
	// 根据已启用的协议集合和路由规则判断。
	OpenAIEndpointCapabilityResponses OpenAIEndpointCapability = "responses"
	// OpenAIEndpointCapabilityRemoteCompactionV2 表示提供商可承接原生 remote_compaction_v2。
	// 它仍要求普通 Responses 能力，额外受提供商级 V2 开关控制。
	OpenAIEndpointCapabilityRemoteCompactionV2 OpenAIEndpointCapability = "remote_compaction_v2"
)

type OpenAIEndpointCapability string

func (r *Record) SupportsOpenAIEndpointCapability(requested OpenAIEndpointCapability, grokMedia func() (bool, string)) bool {
	if r == nil {
		return false
	}
	if requested == "" {
		return true
	}
	if !r.IsOpenAICompatible() {
		return false
	}
	if _, unified := r.Credentials[UpstreamProtocolsKey]; unified && !r.IsGrok() {
		enabled := r.UpstreamProtocols()
		has := func(p capability.ProtocolID) bool { return slices.Contains(enabled, p) }
		switch requested {
		case OpenAIEndpointCapabilityTextGeneration:
			if !has(capability.ProtocolOpenAIResponses) && !has(capability.ProtocolOpenAIChatCompletions) && !has(capability.ProtocolAnthropicMessages) {
				return false
			}
		case OpenAIEndpointCapabilityResponses, OpenAIEndpointCapabilityRemoteCompactionV2:
			if !has(capability.ProtocolOpenAIResponses) {
				return false
			}
		case OpenAIEndpointCapabilityEmbeddings:
			if !has(capability.ProtocolEmbeddings) {
				return false
			}
		case OpenAIEndpointCapabilityLive:
			if !has(capability.ProtocolLive) {
				return false
			}
		case OpenAIEndpointCapabilityAlphaSearch:
			if !has(capability.ProtocolAlphaSearch) && (!r.IsOpenAIPersonalAccessToken() || !has(capability.ProtocolOpenAIResponses)) {
				return false
			}
		}
	}
	if r.IsGrok() {
		switch requested {
		case OpenAIEndpointCapabilityTextGeneration:
			return true
		case OpenAIEndpointCapabilityGrokMediaGeneration:
			if grokMedia == nil {
				return false
			}
			eligible, reason := grokMedia()
			// 尚无观测的 OAuth 提供商仍作为调度候选，供请求路径在转发前执行计费探测。
			// 如果探测不可用或无法提供明确的付费资格证据，转发门控会拒绝该提供商。
			return eligible || reason == "billing_unobserved"
		default:
			return false
		}
	}
	switch requested {
	case OpenAIEndpointCapabilityTextGeneration:
	case OpenAIEndpointCapabilityLive:
		return r.Platform == PlatformOpenAI &&
			r.Type == ProviderTypeOAuth &&
			!r.IsOpenAIPersonalAccessToken() &&
			!r.IsOpenAIAgentIdentity()
	case OpenAIEndpointCapabilityRemoteCompactionV2:
		if !r.AllowsOpenAINativeCompactionV2() {
			return false
		}
		fallthrough
	case OpenAIEndpointCapabilityResponses:
		// 生图等原生 Responses 路径不能降级；使用 Responses 首选协议解析后，
		// 管理员只启用 Chat 的 APIKey 提供商必须排除。
		if r.Type == ProviderTypeAPIKey && ResolveUpstreamTextProtocol(
			r.Extra,
			TextProtocolResponses,
		) != TextProtocolResponses {
			return false
		}
		// 支持 Responses 的上游同样需具备普通文本能力：复用下方 text_generation
		// 配置集校验。
		requested = OpenAIEndpointCapabilityTextGeneration
	case OpenAIEndpointCapabilityAlphaSearch:
		// alpha/search 的转发按提供商类型分流：OAuth/PAT 走
		// chatgpt.com/backend-api/codex/alpha/search，API key 走
		// {base_url}/v1/alpha/search（见 openAIAlphaSearchURL），两类提供商
		// 都可承接独立搜索请求，上游拒绝该端点时由转发层切换提供商。
		if r.Type != ProviderTypeOAuth && r.Type != ProviderTypeAPIKey {
			return false
		}
	case OpenAIEndpointCapabilityEmbeddings:
		if r.Type != ProviderTypeAPIKey {
			return false
		}
	default:
		return false
	}

	configured, found := r.OpenAIWorkloadCapabilitySet()
	if !found {
		return true
	}
	if requested == OpenAIEndpointCapabilityAlphaSearch && configured[string(OpenAIEndpointCapabilityTextGeneration)] {
		return true
	}
	return configured[string(requested)]
}
