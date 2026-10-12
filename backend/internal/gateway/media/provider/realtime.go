package provider

import (
	"context"
	"net/http"

	"github.com/TokenFlux/TokenRouter/internal/upstream"
	"github.com/TokenFlux/TokenRouter/internal/upstream/grok"
)

// RealtimeOptions 保存 Grok 实时连接的认证、请求头和拨号函数。
type RealtimeOptions struct {
	BaseURL, Model           string
	Token                    string `json:"-"`
	CLIHeaders, ApplyHeaders func(http.Header)
	Dial                     func(context.Context, string, http.Header) (upstream.FrameConn, int, error)
	Enter                    func() (func(), error)
}

func (o RealtimeOptions) String() string   { return "media RealtimeOptions" }
func (o RealtimeOptions) GoString() string { return o.String() }
func (o RealtimeOptions) native() grok.RealtimeDialOptions {
	return grok.RealtimeDialOptions{BaseURL: o.BaseURL, Model: o.Model, Token: o.Token, CLIHeaders: o.CLIHeaders, ApplyHeaders: o.ApplyHeaders, Dial: o.Dial, Enter: o.Enter}
}

// DialRealtime 使用调用方的资源选项建立 Grok 实时连接。
func DialRealtime(ctx context.Context, options RealtimeOptions) (*grok.RealtimeSession, error) {
	return grok.DialRealtime(ctx, options.native())
}
