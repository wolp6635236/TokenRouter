package openaiattempt

import (
	"context"

	"github.com/gin-gonic/gin"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	gatewayhttp "github.com/TokenFlux/TokenRouter/internal/gateway/httpapi"
	"github.com/TokenFlux/TokenRouter/internal/routing"
)

// 兼容错误结构保持同一 HTTP 类型。
type noProviderErrorClassification = gatewayhttp.SelectionErrorResponse

func classifySelectionFailureError(err error, fallback noProviderErrorClassification) noProviderErrorClassification {
	return gatewayhttp.RefineSelectionError(err, fallback)
}

// classifyNoProviderError 从 Key 取得最终分组，调用诊断和 HTTP 错误映射。
func classifyNoProviderError(
	ctx context.Context,
	diag routing.ModelAvailabilityDiagnoser,
	apiKey *apikey.APIKey,
	routingModel string,
	displayModel string,
	platform string,
) noProviderErrorClassification {
	var groupID *int64
	if apiKey != nil {
		groupID = apiKey.GroupID
	}
	return gatewayhttp.ClassifySelectionError(ctx, diag, groupID, routingModel, displayModel, platform)
}

// ClassifyNoProviderErrorFromGin 复用 gin.Context 上的 request context，简化 handler 调用点。
func ClassifyNoProviderErrorFromGin(
	c *gin.Context,
	diag routing.ModelAvailabilityDiagnoser,
	apiKey *apikey.APIKey,
	routingModel string,
	displayModel string,
	platform string,
) noProviderErrorClassification {
	ctx := context.Background()
	if c != nil && c.Request != nil {
		ctx = c.Request.Context()
	}
	classification := classifyNoProviderError(ctx, diag, apiKey, routingModel, displayModel, platform)
	if classification.ModelNotFound {
		gatewayhttp.MarkOpsClientBusinessLimited(c, gatewayhttp.OpsClientBusinessLimitedReasonLocalModelConfiguration)
	}
	return classification
}

// ClassifyOpenAICompatibleNoProviderErrorFromGin 按分组候选及可选强制平台诊断兼容请求。
func ClassifyOpenAICompatibleNoProviderErrorFromGin(
	c *gin.Context,
	diag routing.ModelAvailabilityDiagnoser,
	apiKey *apikey.APIKey,
	routingModel string,
	displayModel string,
) noProviderErrorClassification {
	return ClassifyNoProviderErrorFromGin(
		c,
		diag,
		apiKey,
		routingModel,
		displayModel,
		gatewayhttp.OpenAICompatibleRequestPlatform(apiKey),
	)
}
