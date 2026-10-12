package messageforward

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"

	"github.com/TokenFlux/TokenRouter/internal/gateway"
	"github.com/TokenFlux/TokenRouter/internal/gateway/errorpolicy"
	forwardcore "github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/infra/httpclient"
	"github.com/TokenFlux/TokenRouter/internal/settings"
	"github.com/TokenFlux/TokenRouter/internal/upstream"
	"github.com/TokenFlux/TokenRouter/internal/upstream/anthropic"
)

// privateHTTPBoundary 记录输出和活动写入，完整 HTTP 响应由公开入口测试覆盖。
type privateHTTPBoundary struct {
	HTTPBoundary
	Request *http.Request
	Writer  *privateOutputSink
}

type privateOutputSink struct {
	recorder *httptest.ResponseRecorder
	written  bool
}

// 请求构造测试提供 Header。调用响应方法时，嵌入的 nil 接口会使测试失败。
type requestBoundaryFixture struct {
	HTTPBoundary
	Request *http.Request
}

type betaSettingsFixture struct {
	gateway.RuntimeSettingsStore
	values map[string]string
}

func (s *privateOutputSink) Begin(head upstream.OutputHead) error {
	for name, values := range head.Header {
		s.recorder.Header()[name] = append([]string(nil), values...)
	}
	s.recorder.WriteHeader(head.Status)
	s.written = true
	return nil
}

func (s *privateOutputSink) Emit(event upstream.OutputEvent) error {
	if len(event.Data) > 0 {
		s.written = true
		if _, err := s.recorder.Write(event.Data); err != nil {
			return err
		}
	}
	if event.Flush {
		s.recorder.Flush()
	}
	return nil
}

func (s *privateOutputSink) Written() bool { return s.written }

func newPrivateHTTPFixture(rec *httptest.ResponseRecorder) (*privateHTTPBoundary, error) {
	return &privateHTTPBoundary{Writer: &privateOutputSink{recorder: rec}}, nil
}

func (b *privateHTTPBoundary) Present() bool { return true }

func (b *privateHTTPBoundary) RequestPresent() bool { return b.Request != nil }

func (b *privateHTTPBoundary) RequestHeaders() http.Header {
	if b.Request == nil {
		return http.Header{}
	}
	return b.Request.Header
}

func (b *privateHTTPBoundary) HasHeaderFilter() bool { return false }

func (b *privateHTTPBoundary) Sink() upstream.OutputSink { return b.Writer }

func (b *privateHTTPBoundary) Written() bool { return b.Writer.Written() }

func (b *privateHTTPBoundary) Size() int {
	if !b.Written() {
		return -1
	}
	return b.Writer.recorder.Body.Len()
}

func (b *privateHTTPBoundary) ServiceTier() string { return "" }

func (b *privateHTTPBoundary) MarkPassthrough() {}

func (b *privateHTTPBoundary) Observe(forwardcore.Notice) {}

func (b *privateHTTPBoundary) SetError(int, string, string) {}

func (b *privateHTTPBoundary) MatchRule(string, int, []byte) *errorpolicy.ErrorPassthroughRule {
	return nil
}

func (b *privateHTTPBoundary) Commit() { b.Writer.written = true }

func (b *privateHTTPBoundary) MessageError(status int, _, _ string) {
	b.Writer.recorder.WriteHeader(status)
	b.Writer.written = true
}

func (b *privateHTTPBoundary) RawError(status int, body []byte) {
	b.Writer.recorder.WriteHeader(status)
	_, _ = b.Writer.recorder.Write(body)
	b.Writer.written = true
}

func (b *privateHTTPBoundary) WriteHeaders(dst, src http.Header, _ bool) {
	for key, values := range src {
		dst[key] = append([]string(nil), values...)
	}
}

func (b *privateHTTPBoundary) ReadResponseBody(reader io.Reader, limit int64, _ BodyKind) ([]byte, error) {
	return httpclient.ReadResponseBodyLimited(reader, limit)
}

func (b *requestBoundaryFixture) Present() bool { return b != nil }

func (b *requestBoundaryFixture) RequestPresent() bool { return b != nil && b.Request != nil }

func (b *requestBoundaryFixture) RequestHeaders() http.Header {
	if !b.RequestPresent() {
		return http.Header{}
	}
	return b.Request.Header
}

func (s betaSettingsFixture) GetValue(_ context.Context, key string) (string, error) {
	if value, ok := s.values[key]; ok {
		return value, nil
	}
	return "", settings.ErrSettingNotFound
}

func newBetaRuntime(values map[string]string) *gateway.RuntimeSettings {
	return gateway.NewRuntimeSettings(betaSettingsFixture{values: values}, settings.ErrSettingNotFound, func() *gateway.BetaPolicySettings {
		return gatewayprovider.GatewayBetaPolicy(anthropic.DefaultBetaPolicySettings())
	})
}

// GetMultiple 每次读取返回独立映射，设置用例可以在主动失效后观察新值。
func (s betaSettingsFixture) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	out := make(map[string]string)
	for _, key := range keys {
		if value, ok := s.values[key]; ok {
			out[key] = value
		}
	}
	return out, nil
}
