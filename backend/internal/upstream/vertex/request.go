package vertex

import (
	"bytes"
	"context"
	"net/http"
	"strconv"
	"strings"

	wire "github.com/TokenFlux/TokenRouter/internal/protocol/anthropic"
)

// vertexSupportedBetaTokens 是 Vertex AI 的 Anthropic 端点接受的 anthropic-beta
// 白名单。Vertex 对未知 token 返回 HTTP 400，此处与 Bedrock 的
// bedrockSupportedBetaTokens 一样过滤清单外的 token。Vertex 支持新的 beta 时需要补充清单。
//
// 明确排除（issue #3358 中 Vertex 报 400 的 token）：advisor-tool-2026-03-01、
// prompt-caching-scope-2026-01-05、redact-thinking-2026-02-12、
// thinking-token-count-2026-05-13；以及 claude-code-20250219 / oauth-2025-04-20 等
// 客户端身份 beta。Vertex service_account 使用 Bearer 鉴权。
var vertexSupportedBetaTokens = map[string]bool{
	"context-1m-2025-08-07":                  true,
	"context-management-2025-06-27":          true,
	"fine-grained-tool-streaming-2025-05-14": true,
	"interleaved-thinking-2025-05-14":        true,
}

// AnthropicRequestOptions 配置请求头、项目、区域和 beta 策略。
type AnthropicRequestOptions struct {
	ClientBeta           string
	ClientHeaders        http.Header
	AllowedHeaders       map[string]bool
	Project              func() string
	Location             func(string) string
	Policy               func(context.Context, string) (map[string]struct{}, error)
	SanitizeBody         func([]byte, string) ([]byte, bool)
	WireCasing           func(string) string
	AddHeader, SetHeader func(http.Header, string, string)
	DeleteHeader         func(http.Header, string)
	Debug                func(http.Header, []byte, map[string]string)
}

// FilterBetaTokens 解析 client 的 anthropic-beta header，先剔除 drop 集合中的
// token（BetaPolicy filter + 默认 drop），再只保留 Vertex 支持的 token，去重后逗号拼接。
// 返回最终 header（可能为空字符串）。
func FilterBetaTokens(header string, drop map[string]struct{}) string {
	tokens := wire.ParseAnthropicBetaHeader(header)
	if len(tokens) == 0 {
		return ""
	}
	out := make([]string, 0, len(tokens))
	seen := make(map[string]bool, len(tokens))
	for _, t := range tokens {
		if _, dropped := drop[t]; dropped {
			continue
		}
		if !vertexSupportedBetaTokens[t] {
			continue
		}
		if seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return strings.Join(out, ",")
}

func BuildAnthropicRequest(ctx context.Context, body []byte, token, modelID string, reqStream bool, options AnthropicRequestOptions) (*http.Request, error) {
	vertexBody, err := BuildVertexAnthropicRequestBody(body)
	if err != nil {
		return nil, err
	}

	// 计算最终 outgoing anthropic-beta。Vertex AI 的 Anthropic 端点只接受一小撮
	// beta token，未知 token 返回 HTTP 400。Claude Code CLI 透传的
	// advisor-tool-2026-03-01 / prompt-caching-scope-2026-01-05 /
	// redact-thinking-2026-02-12 / thinking-token-count-2026-05-13 都不被 Vertex 接受
	// （issue #3358）。这里复用 BetaPolicy 的 block 检查（与 Bedrock 的
	// resolveBedrockBetaTokensForRequest 对称），再按 vertexSupportedBetaTokens 白名单
	// 剥离其余 token，使该路径与 Anthropic 直连 / Bedrock 路径行为一致。
	clientBeta := options.ClientBeta
	drop, err := options.Policy(ctx, clientBeta)
	if err != nil {
		return nil, err
	}
	finalBeta := FilterBetaTokens(clientBeta, drop)

	// 根据过滤后的 beta 决定是否保留 body 中的
	// context_management，与 Anthropic 直连 / Bedrock 路径对称。
	if sanitized, changed := options.SanitizeBody(vertexBody, finalBeta); changed {
		vertexBody = sanitized
	}
	fullURL, err := BuildVertexAnthropicURL(options.Project(), options.Location(modelID), modelID, reqStream)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(vertexBody))
	if err != nil {
		return nil, err
	}

	if options.ClientHeaders != nil {
		for key, values := range options.ClientHeaders {
			lowerKey := strings.ToLower(strings.TrimSpace(key))
			if !options.AllowedHeaders[lowerKey] || lowerKey == "anthropic-version" {
				continue
			}
			wireKey := options.WireCasing(key)
			for _, v := range values {
				options.AddHeader(req.Header, wireKey, v)
			}
		}
	}

	req.Header.Del("authorization")
	req.Header.Del("x-api-key")
	req.Header.Del("x-goog-api-key")
	req.Header.Del("cookie")
	req.Header.Del("anthropic-version")
	options.SetHeader(req.Header, "authorization", "Bearer "+token)
	options.SetHeader(req.Header, "content-type", "application/json")

	// 覆盖上面白名单 loop 写入的原始 client anthropic-beta，使用过滤后的最终值。
	// finalBeta 为空（全部被剥离）时不下发该 header，与 Vertex 无 beta 请求一致。
	options.DeleteHeader(req.Header, "anthropic-beta")
	if finalBeta != "" {
		options.SetHeader(req.Header, "anthropic-beta", finalBeta)
	}

	options.Debug(req.Header, vertexBody, map[string]string{
		"url":        req.URL.String(),
		"token_type": "service_account",
		"model":      modelID,
		"stream":     strconv.FormatBool(reqStream),
	})

	return req, nil
}
