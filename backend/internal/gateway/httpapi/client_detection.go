package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/TokenFlux/TokenRouter/internal/gateway/clientmeta"
	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
)

var claudeCodeValidator = clientmeta.NewClaudeCodeValidator()

// ClientDetection 只携带识别结果；请求许可与平台默认值仍由相应用例裁决。
type ClientDetection struct {
	ClaudeCode bool
	Version    string
}

// SetClaudeCodeClientContext 将客户端识别结果写入 HTTP 请求上下文，供后续处理读取。
func SetClaudeCodeClientContext(c *gin.Context, body []byte, parsed *requeststate.ParsedRequest) {
	if c == nil || c.Request == nil {
		return
	}
	probe, _ := requeststate.IsMaxTokensOneHaikuRequestFromContext(c.Request.Context())
	result := DetectClaudeCodeRequest(c, body, parsed, probe)
	ctx := requeststate.SetClaudeCodeClient(c.Request.Context(), result.ClaudeCode)
	if result.ClaudeCode && result.Version != "" {
		ctx = requeststate.SetClaudeCodeVersion(ctx, result.Version)
	}
	c.Request = c.Request.WithContext(ctx)
}

// DetectClaudeCodeRequest 返回客户端识别结果，兼容入口将结果写入 context。
func DetectClaudeCodeRequest(c *gin.Context, body []byte, parsedReq *requeststate.ParsedRequest, probe bool) ClientDetection {
	if c == nil || c.Request == nil {
		return ClientDetection{}
	}
	ua := c.GetHeader("User-Agent")
	// 非 Claude CLI UA 直接返回 false，省去 JSON 反序列化。
	if !claudeCodeValidator.ValidateUserAgent(ua) {
		return ClientDetection{}
	}

	isClaudeCode := false
	if !strings.Contains(c.Request.URL.Path, "messages") {
		// 与 Validate 行为一致：非 messages 路径 UA 命中即可视为 Claude Code 客户端。
		isClaudeCode = true
	} else {
		// 仅在确认为 Claude CLI 且 messages 路径时再做 body 解析。
		bodyMap := ClaudeCodeBodyMapFromParsedRequest(parsedReq)
		if bodyMap == nil && len(body) > 0 {
			_ = json.Unmarshal(body, &bodyMap)
		}
		isClaudeCode = claudeCodeValidator.Validate(clientmeta.ClaudeCodeValidationInput{Path: c.Request.URL.Path, UserAgent: ua, XApp: c.GetHeader("X-App"), AnthropicBeta: c.GetHeader("anthropic-beta"), AnthropicVersion: c.GetHeader("anthropic-version"), MaxTokensOneHaiku: probe}, bodyMap)
	}

	result := ClientDetection{ClaudeCode: isClaudeCode}
	if isClaudeCode {
		result.Version = claudeCodeValidator.ExtractVersion(ua)
	}
	return result
}

func ClaudeCodeBodyMapFromParsedRequest(parsedReq *requeststate.ParsedRequest) map[string]any {
	if parsedReq == nil {
		return nil
	}
	bodyMap := map[string]any{
		"model": parsedReq.Model,
	}
	if parsedReq.HasSystem {
		if system, ok := parsedReq.SystemValue(); ok {
			bodyMap["system"] = system
		} else {
			bodyMap["system"] = nil
		}
	}
	if parsedReq.MetadataUserID != "" {
		bodyMap["metadata"] = map[string]any{"user_id": parsedReq.MetadataUserID}
	}
	return bodyMap
}

// PrepareMessageClientContext 在路由与提供商选择前解析客户端、探针和 thinking 的可信请求内状态。
func PrepareMessageClientContext(c *gin.Context, body []byte, bounds func(context.Context) (string, string)) error {
	// 身份探测宽松读取 stream，规范化解析副本，出站使用原报文。
	identityBody := body
	if value := gjson.GetBytes(body, "stream"); value.Exists() && value.Type != gjson.True && value.Type != gjson.False {
		var err error
		identityBody, err = sjson.SetBytes(body, "stream", value.Bool())
		if err != nil {
			return err
		}
	}
	parsed, err := requeststate.ParseGatewayRequest(requeststate.NewRequestBodyRef(identityBody), "anthropic")
	if err != nil {
		return err
	}
	probe := clientmeta.IsHaikuProbe(parsed.Model, parsed.MaxTokens)
	ctx := requeststate.WithIsMaxTokensOneHaikuRequest(c.Request.Context(), probe)
	c.Request = c.Request.WithContext(ctx)
	detected := DetectClaudeCodeRequest(c, body, parsed, probe)
	ctx = requeststate.SetClaudeCodeClient(ctx, detected.ClaudeCode)
	ctx = requeststate.SetClaudeCodeVersion(ctx, detected.Version)
	ctx = requeststate.WithThinkingEnabled(ctx, parsed.ThinkingEnabled)
	c.Request = c.Request.WithContext(ctx)
	if detected.ClaudeCode && bounds != nil {
		minimum, maximum := bounds(ctx)
		if message := clientmeta.ClaudeVersionRejection(detected.Version, minimum, maximum); message != "" {
			return errors.New(message)
		}
	}
	return nil
}
