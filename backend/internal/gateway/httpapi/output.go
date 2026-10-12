package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/TokenFlux/TokenRouter/internal/upstream"
)

// MessagesOutput 提供同步 HTTP 输出与请求观测字段。
// 执行器通过 execution.Request 读取身份、路由、报文和资金状态。
type MessagesOutput struct {
	Concurrency *ConcurrencyHelper
	ResponseSink
	HTTP          *gin.Context
	Log           *zap.Logger
	StreamStarted *bool
}

// ResponseSink 接收上游同步事件，负责下游写入和 Flush。
type ResponseSink struct{ Writer http.ResponseWriter }

func (s ResponseSink) Begin(head upstream.OutputHead) error {
	for key, values := range head.Header {
		s.Writer.Header()[key] = append([]string(nil), values...)
	}
	s.Writer.WriteHeader(head.Status)
	return nil
}

func (s ResponseSink) Emit(event upstream.OutputEvent) error {
	if len(event.Data) > 0 {
		if _, err := s.Writer.Write(event.Data); err != nil {
			return err
		}
	}
	if event.Flush {
		if flusher, ok := s.Writer.(http.Flusher); ok {
			flusher.Flush()
		}
	}
	return nil
}

// InitialOutput 只快照现有响应状态，保留等待心跳和先前中间件设置的 Header。
func (s ResponseSink) InitialOutput() upstream.OutputHead {
	// 先读取 Header 再读取状态，因为 Header 可能暂停等待心跳。
	headers := s.Writer.Header().Clone()
	h := s.OutputState()
	h.Header = headers
	return h
}

// OutputState 直接读取提交状态，等待中的 keepalive 继续运行。
func (s ResponseSink) OutputState() upstream.OutputHead {
	h := upstream.OutputHead{}
	if w, ok := s.Writer.(interface{ Written() bool }); ok {
		h.Committed = w.Written()
	}
	if w, ok := s.Writer.(interface{ Status() int }); ok {
		h.Status = w.Status()
	}
	return h
}
