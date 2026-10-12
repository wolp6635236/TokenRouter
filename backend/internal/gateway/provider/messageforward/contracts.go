package messageforward

import (
	"io"
	"net/http"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/egress"
	"github.com/TokenFlux/TokenRouter/internal/gateway/errorpolicy"
	"github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	"github.com/TokenFlux/TokenRouter/internal/gateway/searchtools"
	"github.com/TokenFlux/TokenRouter/internal/upstream"
	"github.com/TokenFlux/TokenRouter/internal/upstream/anthropic"
)

const (
	MessagesBody BodyKind = iota
	CountBody
)

// Options 保存启动时传入的传输参数，动态策略在请求处理中读取。
type Options struct {
	Configured           bool
	InjectAPIKeyBeta     bool
	LogErrorBody         bool
	LogErrorBodyMaxBytes int
	FailoverOn400        bool
	GeminiDebugHeaders   bool
	MaxLineSize          int
	StreamInterval       time.Duration
	StreamKeepalive      time.Duration
	ResponseReadLimit    int64
	PreserveContentType  bool
	URLAllowlistEnabled  bool
	AllowInsecureHTTP    bool
	URLValidation        egress.ValidationOptions
}

// AttemptState 保存一次请求准备和响应转换的状态。
// BetaEvaluated 区分尚未查询和已经得到空过滤集，保持 Messages 与 count 的读取差异。
type AttemptState struct {
	BetaEvaluated bool
	BetaFilters   map[string]struct{}
	ToolNames     *anthropic.ToolNameRewrite
	MimicDebug    string
}

// BodyKind 固定不同入口的超限错误形状，不让 provider 决定 HTTP envelope。
type BodyKind uint8

// HTTPBoundary 定义当前 HTTP 请求的交换与观测操作。
// 实现由 HTTP Adapter 持有，provider 不获取 Gin Context 或通用 Get/Set 容器。
type HTTPBoundary interface {
	Present() bool
	RequestPresent() bool
	HasHeaderFilter() bool
	RequestHeaders() http.Header
	Sink() upstream.OutputSink
	SearchOutput() searchtools.Output
	ConversionOutput(responses bool, state *AttemptState) forward.Output
	ReadResponseBody(io.Reader, int64, BodyKind) ([]byte, error)
	Size() int
	Written() bool
	Commit()
	GenericError()
	MessageError(int, string, string)
	RawError(int, []byte)
	CountError(int, string, string)
	CountSuccess(int, map[string][]string, []byte, bool)
	MarkPassthrough()
	ServiceTier() string
	SetError(int, string, string)
	Observe(forward.Notice)
	MatchRule(string, int, []byte) *errorpolicy.ErrorPassthroughRule
	SkipMonitoring()
	WriteHeaders(http.Header, http.Header, bool)
}
