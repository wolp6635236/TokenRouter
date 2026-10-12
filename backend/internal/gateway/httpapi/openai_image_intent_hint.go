package httpapi

import (
	"github.com/gin-gonic/gin"

	"github.com/TokenFlux/TokenRouter/internal/gateway/media"
	gatewayadapter "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	"github.com/TokenFlux/TokenRouter/internal/routing"
)

// 请求级 hint 仅限 HTTP：缺失表示 unknown，false/true 都表示已完成 canonical 判定。
const openAIImageIntentHintContextKey = "openai_image_intent_hint"

type ImageIntentClassifier func(endpoint string, requestedModel string, body []byte) bool

// GroupMappedImageIntent 先按分组映射改写模型和报文，再判断图片意图。
func GroupMappedImageIntent(endpoint, model string, body []byte, mapping routing.GroupMappingResult, platform string, replace requeststate.ModelBodyReplacer) ([]byte, string, bool) {
	target := requeststate.GroupMappedModel(model, mapping)
	rewritten := requeststate.ModelMappedBody(body, mapping.Mapped, target, replace)
	return rewritten, target, gatewayadapter.ImageIntentForPlatform(endpoint, target, rewritten, platform)
}

// SeedOpenAIForwardImageIntentHint 记录未改写请求的图片意图，分组映射后的请求等待重新判断。
func SeedOpenAIForwardImageIntentHint(c *gin.Context, mapped, image bool) {
	if mapped {
		return
	}
	SetOpenAIImageIntentHint(c, image)
}

// SetOpenAIImageIntentHint 保存请求级 canonical 生图判定。
func SetOpenAIImageIntentHint(c *gin.Context, imageIntent bool) {
	if c == nil || GetOpenAIClientTransport(c) != OpenAIClientTransportHTTP {
		return
	}
	c.Set(openAIImageIntentHintContextKey, imageIntent)
}

func GetOpenAIImageIntentHint(c *gin.Context) (imageIntent bool, known bool) {
	if c == nil || GetOpenAIClientTransport(c) != OpenAIClientTransportHTTP {
		return false, false
	}
	value, ok := c.Get(openAIImageIntentHintContextKey)
	if !ok {
		return false, false
	}
	imageIntent, ok = value.(bool)
	return imageIntent, ok
}

func ResolveOpenAIImageIntentHint(
	c *gin.Context,
	requestedModel string,
	canonicalBody []byte,
	classify ImageIntentClassifier,
) bool {
	if imageIntent, known := GetOpenAIImageIntentHint(c); known {
		return imageIntent
	}
	imageIntent := classify(media.OpenAIResponsesEndpoint, requestedModel, canonicalBody)
	SetOpenAIImageIntentHint(c, imageIntent)
	return imageIntent
}

func ResolveOpenAIPassthroughImageIntent(
	c *gin.Context,
	canonicalRequestedModel string,
	canonicalBody []byte,
	attemptRequestedModel string,
	attemptBody []byte,
	attemptInvalidated bool,
	classify ImageIntentClassifier,
) bool {
	imageIntent := ResolveOpenAIImageIntentHint(c, canonicalRequestedModel, canonicalBody, classify)
	if attemptInvalidated {
		// strip/compact 改写后重新计算当前尝试的意图，请求级 canonical hint 保持原样。
		imageIntent = classify(media.OpenAIResponsesEndpoint, attemptRequestedModel, attemptBody)
	}
	return imageIntent
}
