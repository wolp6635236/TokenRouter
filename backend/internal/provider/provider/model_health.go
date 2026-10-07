package provider

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/upstream"
	"github.com/TokenFlux/TokenRouter/internal/upstream/grok"
	"github.com/TokenFlux/TokenRouter/internal/upstream/openai"
)

// ModelHealth 处理本次模型观测，app 传入别名规则。
type ModelHealth struct {
	Health *provider.HealthService

	IsImageModel func(string) bool
}

// LimitKey 保留最终上游模型的一跳规则，不二次应用 OpenAI/Grok 提供商映射。
func (s *ModelHealth) LimitKey(value *provider.Record, requested string, thinking *bool) string {
	key := strings.TrimSpace(requested)
	if value == nil || key == "" {
		return key
	}
	if value.Platform == provider.PlatformAntigravity {
		mapped := FinalAntigravityModel(value, key, thinking)
		if mapped = strings.TrimSpace(mapped); mapped != "" {
			return mapped
		}
		return key
	}
	if value.Platform == provider.PlatformOpenAI || value.Platform == provider.PlatformGrok {
		normalized := key
		if value.IsGrok() {
			normalized = grok.NormalizeModelID(key)
		}
		if normalized = strings.TrimSpace(normalized); normalized != "" {
			return normalized
		}
		return key
	}
	mapped, _ := provider.ResolveMappedModel(provider.ResolveModelMapping(value, ModelDefaults()), key)
	if mapped = strings.TrimSpace(mapped); mapped != "" {
		return mapped
	}
	return key
}

// Observe 把平台识别结果和当次端点意图交给健康核心，不写共享提供商缓存。
func (s *ModelHealth) Observe(ctx context.Context, value *provider.Record, model string, status int, body []byte, thinking *bool, imagesEndpoint bool) bool {
	key := s.LimitKey(value, model, thinking)
	image := s.IsImageModel != nil && (s.IsImageModel(model) || s.IsImageModel(key))
	return s.Health.ApplyModelUnavailable(ctx, value, status, provider.ModelFailureObservation{NotFound: upstream.IsModelNotFoundError(status, body), CodexPlanGated: openai.IsCodexPlanGatedModelError(status, body), ModelKey: key, ImageModel: image, ImagesEndpoint: imagesEndpoint})
}

// ObserveSparkRateLimit 接收本次请求的 thinking 参数。
func (s *ModelHealth) ObserveSparkRateLimit(ctx context.Context, value *provider.Record, model string, status int, headers http.Header, body []byte, thinking *bool) bool {
	key := s.LimitKey(value, model, thinking)
	return s.Health.ApplySparkRateLimit(ctx, value, key, status, openai.IsCodexSparkModel(model), func() (provider.OpenAI429Disposition, *time.Time) { return ClassifyOpenAI429(headers, body) })
}
