package provider

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/upstream/grok"
)

// VideoContentOptions 保存视频状态查询、内容下载和响应处理函数。
type VideoContentOptions struct {
	StatusURL, RequestID, Range string
	Token                       string `json:"-"`
	Context                     func(context.Context) context.Context
	ContentURL                  func() (string, error)
	ApplyHeaders                func(http.Header, string)
	Do                          func(*http.Request) (*http.Response, error)
	ReadStatus                  func(io.Reader) ([]byte, error)
	Latency                     func(time.Duration)
	TransportError              func(error) error
	HTTPError                   func(*http.Response, string) error
	Enter                       func() (func(), error)
}

func (o VideoContentOptions) String() string   { return "media VideoContentOptions" }
func (o VideoContentOptions) GoString() string { return o.String() }
func (o VideoContentOptions) native() grok.VideoContentOptions {
	return grok.VideoContentOptions{StatusURL: o.StatusURL, RequestID: o.RequestID, Range: o.Range, Token: o.Token, Context: o.Context, ContentURL: o.ContentURL, ApplyHeaders: o.ApplyHeaders, Do: o.Do, ReadStatus: o.ReadStatus, Latency: o.Latency, TransportError: o.TransportError, HTTPError: o.HTTPError, Enter: o.Enter}
}

// OpenVideoContent 使用调用方的资源选项打开 Grok 视频内容流。
func OpenVideoContent(ctx context.Context, options VideoContentOptions) (*grok.VideoContent, error) {
	return grok.OpenVideoContent(ctx, options.native())
}
