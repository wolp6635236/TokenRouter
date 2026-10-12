package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/upstream"
	"github.com/TokenFlux/TokenRouter/internal/upstream/openai"
)

// AlphaSearchOptions 保存已准备的搜索请求和执行回调。请求含授权信息，仅供执行使用；日志和序列化需要使用脱敏数据。
type AlphaSearchOptions struct {
	ProviderID        int64
	Request           *http.Request `json:"-"`
	ResponsesFallback bool
	Model             string
	Enter             func() (func(), error)
	Do                func(*http.Request) (*http.Response, error)
	Latency           func(time.Duration)
	TransportError    func(error) error
	ReadBody          func(io.Reader) ([]byte, error)
	HTTPError         func(*http.Response, []byte) error
	UpdateQuota       func(http.Header)
	Headers           func(http.Header, http.Header)
}

type AlphaSearch struct{ Options AlphaSearchOptions }

// String 防止技术参数中的令牌或请求被默认日志展开。
func (o AlphaSearchOptions) String() string {
	return fmt.Sprintf("media AlphaSearch provider=%d", o.ProviderID)
}
func (o AlphaSearchOptions) GoString() string { return o.String() }

func (e AlphaSearch) Execute(ctx context.Context, input upstream.AttemptInput, sink upstream.OutputSink) (upstream.AttemptResult, error) {
	o := e.Options
	target := &openai.AlphaSearchTarget{
		ProviderID:        o.ProviderID,
		Request:           o.Request,
		ResponsesFallback: o.ResponsesFallback,
		Model:             o.Model,
		Enter:             o.Enter,
		Do:                o.Do,
		Latency:           o.Latency,
		TransportError:    o.TransportError,
		ReadBody:          o.ReadBody,
		HTTPError:         o.HTTPError,
		UpdateQuota:       o.UpdateQuota,
		Headers:           o.Headers,
	}
	input.Target = target
	return (openai.AlphaSearchExecutor{}).Execute(ctx, input, sink)
}
