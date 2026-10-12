package openaiforward

import (
	"context"
	"net/http"
	"net/url"

	"github.com/TokenFlux/TokenRouter/internal/infra/httpclient"
	protocolopenai "github.com/TokenFlux/TokenRouter/internal/protocol/openai"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/upstream/openai"
)

// RequestTargetOptions 包含本次凭据类别和目标地址读取函数。
type RequestTargetOptions struct {
	OAuthTarget, APIKey  bool
	DefaultURL, CodexURL string
	BaseURL              func() string
	Validate             func(string) (string, error)
	FromBase             func(string) string
	AppendSuffix         func(string) string
}

func (o RequestTargetOptions) Resolve() (string, error) {
	target := o.DefaultURL
	if o.OAuthTarget {
		target = o.CodexURL
	} else if o.APIKey {
		if base := o.BaseURL(); base != "" {
			validated, err := o.Validate(base)
			if err != nil {
				return "", err
			}
			target = o.FromBase(validated)
		}
	}
	return o.AppendSuffix(target), nil
}

// BuildResponsesRequest 观察请求端点，归一化报文并构造 Responses 请求。
func BuildResponsesRequest(ctx context.Context, body []byte, key string, target RequestTargetOptions, observe func(string), normalize func([]byte) []byte, options func(string) openai.ResponsesRequestOptions) (*http.Request, error) {
	address, err := target.Resolve()
	if err != nil {
		return nil, err
	}
	if parsed, e := url.Parse(address); e == nil {
		observe(parsed.Path)
	}
	body = normalize(body)
	return openai.BuildResponsesRequest(ctx, body, key, options(address))
}

// BuildPassthroughRequest 按目标地址和报文归一化配置构造透传请求。
func BuildPassthroughRequest(ctx context.Context, body []byte, target RequestTargetOptions, normalize func([]byte) []byte, options func(string) openai.PassthroughRequestOptions) (*http.Request, error) {
	address, err := target.Resolve()
	if err != nil {
		return nil, err
	}
	body = normalize(body)
	return openai.BuildPassthroughRequest(ctx, body, options(address))
}

// ResponsesEndpoint 按平台规则处理 DeepSeek 和其他兼容平台的 URL 版本段。
func ResponsesEndpoint(platform, base string) string {
	if platform == capability.PlatformDeepseek {
		return httpclient.BuildOpenAIEndpointURL(base, "/responses")
	}
	return httpclient.BuildOpenAIEndpointURL(base, "/v1/responses")
}

// NormalizeCNResponsesBody 清理 CN Responses 请求中的服务端状态字段。
func NormalizeCNResponsesBody(native bool, body []byte) []byte {
	if !native {
		return body
	}
	return protocolopenai.StatelessResponsesRequest(body)
}
