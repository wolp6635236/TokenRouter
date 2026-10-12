package antigravity

import (
	"github.com/TokenFlux/TokenRouter/internal/protocol/gemini"
)

// DefaultStopSequences 默认停止序列。
var DefaultStopSequences = []string{
	"<|user|>",
	"<|endoftext|>",
	"<|end_of_turn|>",
	"\n\nHuman:",
}

// 通用 wire 类型只保留别名；默认模型、安全配置及 v1internal 包装仍由平台拥有。

// V1InternalRequest v1internal 请求包装。
type V1InternalRequest struct {
	Project     string        `json:"project"`
	RequestID   string        `json:"requestId"`
	UserAgent   string        `json:"userAgent"`
	RequestType string        `json:"requestType,omitempty"`
	Model       string        `json:"model"`
	Request     GeminiRequest `json:"request"`
}

type GeminiRequest = gemini.GeminiRequest

type GeminiContent = gemini.GeminiContent

type GeminiPart = gemini.GeminiPart

type GeminiGenerationConfig = gemini.GeminiGenerationConfig

type GeminiToolDeclaration = gemini.GeminiToolDeclaration

type GeminiToolConfig = gemini.GeminiToolConfig

type GeminiFunctionCallingConfig = gemini.GeminiFunctionCallingConfig

// V1InternalResponse v1internal 响应包装。
type V1InternalResponse struct {
	Response     GeminiResponse `json:"response"`
	ResponseID   string         `json:"responseId,omitempty"`
	ModelVersion string         `json:"modelVersion,omitempty"`
}

type GeminiResponse = gemini.GeminiResponse
