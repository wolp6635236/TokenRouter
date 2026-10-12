package provider

import (
	"context"
	"io"
	"strings"

	"github.com/TokenFlux/TokenRouter/internal/batchimage"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

const (
	PlatformGemini             = provider.PlatformGemini
	ProviderTypeAPIKey         = provider.ProviderTypeAPIKey
	ProviderTypeServiceAccount = provider.ProviderTypeServiceAccount
)

type (
	// Provider 是批量图片执行使用的提供商记录。
	Provider = provider.Record
	// GeminiTokenCache 缓存 Gemini 访问令牌。
	GeminiTokenCache = provider.AccessTokenCache
)

// BatchImageProvider 定义图片作业的提交、轮询、取消、结果读取和资源清理。
type BatchImageProvider interface {
	Name() string
	SupportsProvider(*Provider) bool
	Submit(context.Context, *batchimage.BatchImageJob, *Provider, batchimage.BatchImageInput) (*batchimage.BatchProviderJob, error)
	Get(context.Context, *batchimage.BatchImageJob, *Provider) (*batchimage.BatchProviderStatus, error)
	Cancel(context.Context, *batchimage.BatchImageJob, *Provider) error
	OpenResult(context.Context, *batchimage.BatchImageJob, *Provider) (io.ReadCloser, string, error)
	Cleanup(context.Context, *batchimage.BatchImageJob, *Provider, batchimage.CleanupTarget) error
}

// batchImageProviderAPIKey 返回去除首尾空白后的提供商 API Key。
func batchImageProviderAPIKey(value *Provider) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(value.GetCredential("api_key"))
}

// resolveBatchProtocol 返回提供商匹配的批量图片协议及匹配结果。
func resolveBatchProtocol(value *Provider) (capability.ProtocolID, bool) {
	plan := routing.Plan(routing.PlanInput{ClientProtocol: capability.ProtocolImageBatches})
	candidate, ok := plan.ResolveCandidate(value.RoutingSnapshot())
	return candidate.UpstreamProtocol, ok
}
