package provider

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/TokenFlux/TokenRouter/internal/gateway/provider/modelidentity"
	protocolanthropic "github.com/TokenFlux/TokenRouter/internal/protocol/anthropic"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

// OpenAICompatAnthropicReasoningEffort 在最终上游模型确定后重新裁定 Messages 桥接的推理强度。
// Anthropic 的 max 通常转换为 OpenAI xhigh，但 GPT-5.6 支持原生 max，不能因客户端别名而降级。
func OpenAICompatAnthropicReasoningEffort(req *protocolanthropic.AnthropicRequest, upstreamModel, convertedEffort string) string {
	if req == nil || req.OutputConfig == nil || !strings.EqualFold(strings.TrimSpace(req.OutputConfig.Effort), "max") {
		return convertedEffort
	}
	if normalized := capability.NormalizeRecordedOpenAIEffortForModel(req.OutputConfig.Effort, upstreamModel); normalized != "" {
		return normalized
	}
	if strings.EqualFold(strings.TrimSpace(convertedEffort), "max") {
		return "xhigh"
	}
	return convertedEffort
}

// isOfficialOpenAIModelsBaseURL 判断目标是否为官方 OpenAI 主机。
func isOfficialOpenAIModelsBaseURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && strings.EqualFold(u.Hostname(), "api.openai.com")
}

// ShouldPreserveOpenAIResponsesNoneReasoningEffort 判断请求是否仍需保留官方目录的 none 占位值。
func ShouldPreserveOpenAIResponsesNoneReasoningEffort(provider *providercore.Record) bool {
	if provider == nil {
		return false
	}
	if provider.IsOpenAIPassthroughEnabled() {
		return true
	}
	if provider.IsOpenAIOAuthLike() {
		return true
	}
	if !provider.IsOpenAIApiKey() {
		return false
	}
	baseURL := strings.TrimSpace(provider.GetCredential("base_url"))
	return baseURL == "" || isOfficialOpenAIModelsBaseURL(baseURL)
}

// FilterOpenAIResponsesNoneReasoningEffortForProvider 删除兼容上游不应接收的目录占位值。
// 官方 OpenAI 请求的 none 值原样发送。
func FilterOpenAIResponsesNoneReasoningEffortForProvider(provider *providercore.Record, body []byte) ([]byte, error) {
	if len(body) == 0 || ShouldPreserveOpenAIResponsesNoneReasoningEffort(provider) {
		return body, nil
	}

	out := body
	for _, path := range []string{"reasoning.effort", "reasoning_effort"} {
		effort := gjson.GetBytes(out, path)
		if effort.Type != gjson.String || !strings.EqualFold(strings.TrimSpace(effort.String()), "none") {
			continue
		}
		next, err := sjson.DeleteBytes(out, path)
		if err != nil {
			return body, fmt.Errorf("strip %s none placeholder: %w", path, err)
		}
		out = next
	}
	if reasoning := gjson.GetBytes(out, "reasoning"); reasoning.IsObject() && len(reasoning.Map()) == 0 {
		next, err := sjson.DeleteBytes(out, "reasoning")
		if err != nil {
			return body, fmt.Errorf("strip empty reasoning object: %w", err)
		}
		out = next
	}
	return out, nil
}

// DeleteOpenAIResponsesNoneReasoningEffortFromObject 删除 WS bridge 中的 none 占位字段。
func DeleteOpenAIResponsesNoneReasoningEffortFromObject(provider *providercore.Record, body map[string]any) {
	if body == nil || ShouldPreserveOpenAIResponsesNoneReasoningEffort(provider) {
		return
	}
	if effort, ok := body["reasoning_effort"].(string); ok && strings.EqualFold(strings.TrimSpace(effort), "none") {
		delete(body, "reasoning_effort")
	}
	reasoning, ok := body["reasoning"].(map[string]any)
	if !ok {
		return
	}
	if effort, ok := reasoning["effort"].(string); ok && strings.EqualFold(strings.TrimSpace(effort), "none") {
		delete(reasoning, "effort")
	}
	if len(reasoning) == 0 {
		delete(body, "reasoning")
	}
}

// NormalizeOpenAICodexCompactReasoningEffort 将 GPT-5.6 compact 暂不接受的
// max 档位降级为 xhigh，并保留 reasoning 下的其他字段。
func NormalizeOpenAICodexCompactReasoningEffort(body []byte, effectiveModel string) ([]byte, bool, error) {
	if !modelidentity.IsGPT56(effectiveModel) ||
		!strings.EqualFold(strings.TrimSpace(gjson.GetBytes(body, "reasoning.effort").String()), "max") {
		return body, false, nil
	}

	// Codex Ultra 在客户端编排层会下发 max；ChatGPT compact 端点目前只接受到
	// xhigh。这里只降级 OpenAI OAuth 的 GPT-5.6 compact 子请求，普通 Responses、
	// API Key 请求和其他平台的 OAuth 请求保留 max。
	normalized, err := sjson.SetBytes(body, "reasoning.effort", "xhigh")
	if err != nil {
		return body, false, fmt.Errorf("normalize codex compact reasoning effort: %w", err)
	}
	return normalized, true, nil
}
