package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	"github.com/TokenFlux/TokenRouter/internal/gateway/admission"
	"github.com/TokenFlux/TokenRouter/internal/gateway/completion"
	"github.com/TokenFlux/TokenRouter/internal/gateway/media"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	"github.com/TokenFlux/TokenRouter/internal/gateway/ws"
	"github.com/TokenFlux/TokenRouter/internal/protocol"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// CheckTextModelPricing 按本次映射和计费来源检查价格，提供商切换后重新计算。
// compact 为 nil 时按普通请求查价，Compact 请求传入对应的执行器。
func CheckTextModelPricing(ctx context.Context, prices *admission.ModelPricing, key *apikey.APIKey, target *gatewayprovider.ExecutionProvider, requested, mapped string, compact *CompactExecutor, source protocol.ProtocolID) error {
	if prices == nil {
		return nil
	}
	// 图片型号由媒体计价处理，文本请求声明图片工具时仍需检查文本价格。
	if media.IsImageGenerationModel(mapped) {
		return nil
	}
	// 上游协议决定模型转换和透传的计费来源，预检与执行复用同一次请求的路由规则。
	target, err := gatewayprovider.ProviderForProtocolAttempt(ctx, target)
	if err != nil {
		return err
	}
	mapping := routing.GroupMappingResult{MappedModel: mapped}
	if plan, ok := requeststate.RoutePlanFromContext(ctx); ok {
		mapping = plan.Mapping()
		requested = plan.Models().RequestedModel
	}
	policy := gatewayprovider.ExecutionModelPolicy(target)
	result := &completion.Result{Model: mapped}
	if target.View().IsOpenAICompatible() {
		result.BillingModel, result.UpstreamModel = policy.ForwardMappedModels(mapped, compact != nil)
	} else if target.Record.Platform == provider.PlatformGemini {
		result.UpstreamModel = policy.Mapped(mapped)
	} else {
		result.UpstreamModel = policy.UpstreamModel(ctx, mapped)
	}
	switch source {
	case protocol.ProtocolResponsesWebSocket:
		result.BillingModel = ws.EntryBillingModel(nil, mapping, requested, result.UpstreamModel)
	case protocol.ProtocolOpenAIResponses:
		profile := openAIForwardProfile(target)
		if compact != nil && target.View().IsOpenAICompatible() && !profile.Grok && !profile.RawChat && !profile.Anthropic {
			// Compact 的提供商映射优先于全局模型，普通 Responses 使用常规模型映射。
			if model := compact.ResolveModel(target, mapped); model != "" {
				result.UpstreamModel = model
			}
		}
		if profile.Passthrough && !profile.RawChat && !profile.Anthropic {
			// HTTP 透传的完成结果以分组映射后的请求名保存 Model。
			result.BillingModel = ""
		}
	}
	model := completion.OpenAIUsageBillingModel(result, mapping.ToUsageFields(requested, result.UpstreamModel))
	var groupID *int64
	if key != nil {
		groupID = key.GroupID
	}
	return prices.Check(ctx, groupID, model)
}

// checkPricing 在文本执行前输出当前客户端协议的缺价错误。
func (e *UnifiedTextExecutor) checkPricing(c *gin.Context, target *gatewayprovider.ExecutionProvider, body []byte, source protocol.ProtocolID) error {
	if e.Pricing == nil {
		return nil
	}
	model := gjson.GetBytes(body, "model").String()
	key, _ := EffectiveAPIKey(c)
	var compact *CompactExecutor
	if source == protocol.ProtocolOpenAIResponses && IsOpenAIResponsesCompactPath(c) {
		compact = &CompactExecutor{}
		if e.OpenAI != nil && e.OpenAI.Text != nil && e.OpenAI.Text.Compact != nil {
			compact = e.OpenAI.Text.Compact
		}
	}
	err := CheckTextModelPricing(c.Request.Context(), e.Pricing, key, target, model, model, compact, source)
	if !errors.Is(err, admission.ErrModelPricingRejected) {
		return err
	}
	MarkOpsClientBusinessLimited(c, OpsClientBusinessLimitedReasonLocalPolicyDenied)
	if source == protocol.ProtocolAnthropicMessages {
		DefaultOpenAIErrorOutput().WriteAnthropicStreamingError(c, http.StatusBadRequest, "invalid_request_error", admission.ModelPricingUnavailableMessage, c.Writer.Written())
	} else {
		DefaultOpenAIErrorOutput().WriteStreamingErrorWithCode(c, http.StatusBadRequest, "invalid_request_error", "model_pricing_unavailable", admission.ModelPricingUnavailableMessage, c.Writer.Written(), false)
	}
	return err
}
