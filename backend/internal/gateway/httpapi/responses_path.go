package httpapi

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/TokenFlux/TokenRouter/internal/upstream"
)

// OpenAIResponsesRequestPathSuffix 提取 Responses 路径后缀并检查协议白名单。
func OpenAIResponsesRequestPathSuffix(c *gin.Context) string {
	suffix, ok := upstream.SanitizedUpstreamPathSuffix(RawOpenAIResponsesRequestPathSuffix(c))
	if !ok {
		return ""
	}
	return suffix
}

// IsForwardableOpenAIResponsesRequestPath 判断入站请求携带的 /responses 子路径
// 是否可以安全转发。路由层用它在鉴权后、调度前直接拒绝畸形子路径。
func IsForwardableOpenAIResponsesRequestPath(c *gin.Context) bool {
	_, ok := upstream.SanitizedUpstreamPathSuffix(RawOpenAIResponsesRequestPathSuffix(c))
	return ok
}

// IsOpenAIResponsesInputTokensRequestPath 判断请求是否指向原生 Responses 输入 token 预检端点。
func IsOpenAIResponsesInputTokensRequestPath(c *gin.Context) bool {
	return OpenAIResponsesRequestPathSuffix(c) == "/input_tokens"
}

// RawOpenAIResponsesRequestPathSuffix 提取路径后缀，调用方负责校验路径。
func RawOpenAIResponsesRequestPathSuffix(c *gin.Context) string {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return ""
	}
	normalizedPath := strings.TrimRight(strings.TrimSpace(c.Request.URL.Path), "/")
	if normalizedPath == "" {
		return ""
	}
	_, after, ok := strings.CutLast(normalizedPath, "/responses")
	if !ok {
		return ""
	}
	suffix := after
	if suffix == "" || suffix == "/" {
		return ""
	}
	if !strings.HasPrefix(suffix, "/") {
		return ""
	}
	return suffix
}
