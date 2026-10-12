package gemini

import (
	"io"
	"sync/atomic"
)

// executeBody 统计上游响应体的关闭次数。
type executeBody struct {
	io.ReadCloser
	closes *atomic.Int32
}

func (b *executeBody) Close() error { b.closes.Add(1); return b.ReadCloser.Close() }
