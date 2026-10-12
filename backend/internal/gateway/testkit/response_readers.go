package testkit

import (
	"io"
	"sync"
)

// BlockingReadCloser 输出指定片段后等待关闭，用于验证流取消和资源释放。
type BlockingReadCloser struct {
	data      []byte
	offset    int
	closed    chan struct{}
	closeOnce sync.Once
}

// CloseTrackingReader 记录响应体是否被关闭，供跨协议资源断言共用。
type CloseTrackingReader struct {
	io.Reader
	Closed bool
}

func NewBlockingReadCloser(data []byte) *BlockingReadCloser {
	return &BlockingReadCloser{
		data:   data,
		closed: make(chan struct{}),
	}
}

func (r *BlockingReadCloser) Read(p []byte) (int, error) {
	if r.offset < len(r.data) {
		n := copy(p, r.data[r.offset:])
		r.offset += n
		return n, nil
	}
	<-r.closed
	return 0, io.EOF
}

func (r *BlockingReadCloser) Close() error {
	r.closeOnce.Do(func() {
		close(r.closed)
	})
	return nil
}

func (r *CloseTrackingReader) Close() error {
	r.Closed = true
	return nil
}
