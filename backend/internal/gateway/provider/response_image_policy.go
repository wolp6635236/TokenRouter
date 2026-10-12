package provider

import (
	"context"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	protocolcore "github.com/TokenFlux/TokenRouter/internal/protocol"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

// ProviderExtraImagesURLToB64JSON 保留既有提供商设置键。
const ProviderExtraImagesURLToB64JSON = "images_url_to_b64_json"

// ResponseImagePolicy 按分组协议策略、提供商覆盖和全局默认值的优先级读取图片桥接设置。
type ResponseImagePolicy struct {
	DefaultEnabled bool
}

// ImagesURLToB64JSONEnabled 返回提供商是否开启 URL 到 base64 的图片回填。
func ImagesURLToB64JSONEnabled(provider *ExecutionProvider) bool {
	return provider != nil && provider.Record.Platform == capability.PlatformOpenAI && provider.Record.Type == capability.ProviderTypeAPIKey && ExecutionProtocolRecord(provider).GetExtraBool(ProviderExtraImagesURLToB64JSON)
}

func GroupResponsesExplicitToolPolicy(group *routing.Group, inherited string) string {
	if group == nil {
		return inherited
	}
	switch group.ResponsesImagePolicy {
	case "block":
		return providercore.CodexImagePolicyStrip
	case "enabled", "disabled":
		return providercore.CodexImagePolicyAllow
	default:
		return inherited
	}
}

func ResponsesPolicyGroup(ctx context.Context, group *routing.Group) *routing.Group {
	source, _ := requeststate.ClientProtocolFromContext(ctx)
	if source != "" && source != protocolcore.ProtocolOpenAIResponses && source != protocolcore.ProtocolResponsesWebSocket {
		return nil
	}
	return group
}

func APIKeyGroup(apiKey *apikey.APIKey) *routing.Group {
	if apiKey == nil {
		return nil
	}
	return apiKey.Group
}

func (s *ResponseImagePolicy) Enabled(ctx context.Context, provider *ExecutionProvider, apiKey *apikey.APIKey) bool {
	if group := ResponsesPolicyGroup(ctx, APIKeyGroup(apiKey)); group != nil {
		switch group.ResponsesImagePolicy {
		case "enabled":
			return true
		case "disabled", "block":
			return false
		}
	}

	if override := ExecutionProtocolRecord(provider).CodexImageGenerationBridgeOverride(); override != nil {
		return *override
	}
	return s != nil && s.DefaultEnabled
}
