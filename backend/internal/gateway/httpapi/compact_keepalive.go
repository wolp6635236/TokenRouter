package httpapi

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	// openAIStreamKeepaliveBytesContextKey 是 Responses SSE 心跳字节数的上下文键。
	openAIStreamKeepaliveBytesContextKey = "openai_stream_keepalive_bytes"

	// openAICompactSSEKeepaliveKey 存放 body-signal compact 请求的下游 SSE 心跳器。
	openAICompactSSEKeepaliveKey = "openai_compact_sse_keepalive"
)

// openAICompactSSEKeepalive 在等待 Compact 上游 JSON 时发送 SSE 注释心跳。
// 大上下文处理可能持续数分钟，Nginx 或 Cloudflare Tunnel 的空闲超时会中断静默连接，Codex 重连会重复消耗压缩配额（#3887）。
// SSE 注释由 eventsource 解析器忽略。首拍延迟一个 interval，此前的鉴权、参数或限流错误按 JSON 和 HTTP 状态返回。
// 首拍提交 200 后，后续错误通过 response.failed 返回。
type openAICompactSSEKeepalive struct {
	mu      sync.Mutex
	writer  gin.ResponseWriter
	started bool
	stopped bool
	// bytes 记录已写出的心跳注释字节数。判断 Forward 是否开始输出协议内容时，
	// OpenAICompactKeepaliveAdjustedWrittenSize 会扣除这些字节。
	bytes int
	stop  chan struct{}
}

// compactKeepaliveWriter 包装 gin.ResponseWriter，写方法在互斥锁下停止心跳，读方法加锁读取状态。
// Forward 前读取 Size 时心跳继续运行。心跳 goroutine 直接写入内层 k.writer。
type compactKeepaliveWriter struct {
	gin.ResponseWriter
	k *openAICompactSSEKeepalive
}

// StartOpenAICompactSSEKeepalive 为标记为 body-signal 客户端流式的 Compact 请求启动心跳，返回幂等停止函数。
// interval<=0 或缺少标记时返回空操作。c.Writer 包装为 compactKeepaliveWriter，
// 请求开始构造响应时在互斥锁下停止心跳，Forward 内部的本地拒绝也通过该包装器写入。
func StartOpenAICompactSSEKeepalive(c *gin.Context, interval time.Duration) func() {
	if !OpenAICompactClientWantsStream(c) {
		return func() {}
	}
	return StartOpenAISSEKeepalive(c, interval)
}

// StartOpenAISSEKeepalive 供已确认使用 SSE 的调用方启动心跳。
// 例如 /v1/responses 透传进入流式循环时，上游已返回 text/event-stream，SSE 响应头也已设置。
// OpenAICompactKeepaliveAdjustedWrittenSize 排除心跳字节后判断是否可以换号（#3887）。
func StartOpenAISSEKeepalive(c *gin.Context, interval time.Duration) func() {
	if c == nil || c.Writer == nil || interval <= 0 {
		return func() {}
	}
	originalWriter := c.Writer
	k := &openAICompactSSEKeepalive{
		writer: originalWriter,
		stop:   make(chan struct{}),
	}
	c.Set(openAICompactSSEKeepaliveKey, k)
	wrappedWriter := &compactKeepaliveWriter{ResponseWriter: originalWriter, k: k}
	c.Writer = wrappedWriter

	var reqDone <-chan struct{}
	if c.Request != nil {
		reqDone = c.Request.Context().Done()
	}
	go func() {
		timer := time.NewTimer(interval)
		defer timer.Stop()
		for {
			select {
			case <-k.stop:
				return
			case <-reqDone:
				return
			case <-timer.C:
			}
			if !k.beat() {
				return
			}
			timer.Reset(interval)
		}
	}()
	return func() {
		k.Stop()
		// 请求结束后恢复中间件 writer，compact 包装器随请求释放。
		if current, ok := c.Writer.(*compactKeepaliveWriter); ok && current == wrappedWriter {
			c.Writer = originalWriter
		}
	}
}

// beat 在锁内提交（首次）响应头并写出一条 SSE 注释行；返回 false 表示心跳已
// 停止或下游写入失败，goroutine 应退出。
func (k *openAICompactSSEKeepalive) beat() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.stopped {
		return false
	}
	if !k.started {
		header := k.writer.Header()
		header.Set("Content-Type", "text/event-stream")
		header.Set("Cache-Control", "no-cache")
		header.Set("Connection", "keep-alive")
		header.Set("X-Accel-Buffering", "no")
		k.writer.WriteHeader(http.StatusOK)
		k.started = true
	}
	n, err := k.writer.Write([]byte(": keepalive\n\n"))
	k.bytes += n
	if err != nil {
		k.stopped = true
		return false
	}
	k.writer.Flush()
	return true
}

// Stop 停止心跳；幂等，可与写回路径并发调用。
func (k *openAICompactSSEKeepalive) Stop() {
	k.mu.Lock()
	k.markStoppedLocked()
	k.mu.Unlock()
}

func (k *openAICompactSSEKeepalive) markStoppedLocked() {
	if k.stopped {
		return
	}
	k.stopped = true
	close(k.stop)
}

// StopOpenAICompactSSEKeepaliveCommitted 停止 Compact 心跳，并返回心跳是否已提交 200。
// 调用方据此选择 JSON 状态响应或 SSE 终止事件。函数通过互斥锁等待心跳写入完成，返回后由调用方接管 ResponseWriter。
func StopOpenAICompactSSEKeepaliveCommitted(c *gin.Context) bool {
	if c == nil {
		return false
	}
	value, ok := c.Get(openAICompactSSEKeepaliveKey)
	if !ok {
		return false
	}
	k, ok := value.(*openAICompactSSEKeepalive)
	if !ok || k == nil {
		return false
	}
	k.mu.Lock()
	k.markStoppedLocked()
	committed := k.started
	k.mu.Unlock()
	return committed
}

// OpenAICompactKeepaliveAdjustedWrittenSize 返回扣除 compact 心跳注释后的已写字节数，无心跳时等于 c.Writer.Size()。
// handler 比较 Forward 前后的结果判断是否开始输出协议内容，已输出时结束换号。
// 仅写出心跳时返回 gin 的“未写出”值 -1，上游 429/5xx 仍可进入换号处理（#3887）。
func OpenAICompactKeepaliveAdjustedWrittenSize(c *gin.Context) int {
	if c == nil || c.Writer == nil {
		return -1
	}
	streamKeepaliveBytes := 0
	if value, ok := c.Get(openAIStreamKeepaliveBytesContextKey); ok {
		streamKeepaliveBytes, _ = value.(int)
	}
	size := c.Writer.Size()
	compactKeepaliveBytes := 0
	if value, ok := c.Get(openAICompactSSEKeepaliveKey); ok {
		if k, valid := value.(*openAICompactSSEKeepalive); valid && k != nil {
			k.mu.Lock()
			size = k.writer.Size()
			compactKeepaliveBytes = k.bytes
			k.mu.Unlock()
		}
	}
	if size < 0 {
		return size
	}
	keepaliveBytes := compactKeepaliveBytes + streamKeepaliveBytes
	if keepaliveBytes <= 0 {
		return size
	}
	if real := size - keepaliveBytes; real > 0 {
		return real
	}
	return -1
}

// suspend 在请求开始构造响应时停止心跳，重复调用安全。Header 访问也会触发停止。
func (w *compactKeepaliveWriter) suspend() {
	if w.k == nil {
		return
	}
	w.k.Stop()
}

func (w *compactKeepaliveWriter) Header() http.Header {
	w.suspend()
	if w.ResponseWriter == nil {
		return http.Header{}
	}
	return w.ResponseWriter.Header()
}

func (w *compactKeepaliveWriter) Write(data []byte) (int, error) {
	w.suspend()
	if w.ResponseWriter == nil {
		return 0, nil
	}
	return w.ResponseWriter.Write(data)
}

func (w *compactKeepaliveWriter) WriteString(s string) (int, error) {
	w.suspend()
	if w.ResponseWriter == nil {
		return 0, nil
	}
	return w.ResponseWriter.WriteString(s)
}

func (w *compactKeepaliveWriter) WriteHeader(code int) {
	w.suspend()
	if w.ResponseWriter == nil {
		return
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *compactKeepaliveWriter) WriteHeaderNow() {
	w.suspend()
	if w.ResponseWriter == nil {
		return
	}
	w.ResponseWriter.WriteHeaderNow()
}

func (w *compactKeepaliveWriter) Flush() {
	w.suspend()
	if w.ResponseWriter == nil {
		return
	}
	w.ResponseWriter.Flush()
}

// Hijack 在内层 writer 已释放时返回空值。
func (w *compactKeepaliveWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if w.ResponseWriter == nil {
		return nil, nil, errors.New("response writer released")
	}
	return w.ResponseWriter.Hijack()
}

func (w *compactKeepaliveWriter) CloseNotify() <-chan bool {
	if w.ResponseWriter == nil {
		ch := make(chan bool)
		close(ch)
		return ch
	}
	return w.ResponseWriter.CloseNotify()
}

func (w *compactKeepaliveWriter) Pusher() http.Pusher {
	if w.ResponseWriter == nil {
		return nil
	}
	return w.ResponseWriter.Pusher()
}

// Status 状态读取只有在 keepalive 与内层 writer 都有效时才加锁委托。
func (w *compactKeepaliveWriter) Status() int {
	if w.k == nil || w.ResponseWriter == nil {
		return 0
	}
	w.k.mu.Lock()
	defer w.k.mu.Unlock()
	return w.ResponseWriter.Status()
}

func (w *compactKeepaliveWriter) Size() int {
	if w.k == nil || w.ResponseWriter == nil {
		return 0
	}
	w.k.mu.Lock()
	defer w.k.mu.Unlock()
	return w.ResponseWriter.Size()
}

func (w *compactKeepaliveWriter) Written() bool {
	if w.k == nil || w.ResponseWriter == nil {
		return false
	}
	w.k.mu.Lock()
	defer w.k.mu.Unlock()
	return w.ResponseWriter.Written()
}

// RecordOpenAIStreamKeepaliveBytes 单独记录流心跳字节数。
func RecordOpenAIStreamKeepaliveBytes(c *gin.Context, written int) {
	if c == nil || written <= 0 {
		return
	}
	current := 0
	if value, ok := c.Get(openAIStreamKeepaliveBytesContextKey); ok {
		current, _ = value.(int)
	}
	c.Set(openAIStreamKeepaliveBytesContextKey, current+written)
}

// OpenAIStreamClientOutputStarted 优先读取本地输出标记，再检查扣除心跳后的字节数。
func OpenAIStreamClientOutputStarted(c *gin.Context, localStarted bool) bool {
	if localStarted {
		return true
	}
	if c == nil || c.Writer == nil {
		return false
	}
	// compact 心跳会提交 HTTP 200，但不属于模型业务输出，不应阻止安全重试。
	return OpenAICompactKeepaliveAdjustedWrittenSize(c) >= 0
}
