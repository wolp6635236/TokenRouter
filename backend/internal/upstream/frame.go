package upstream

import "context"

const (
	FrameText   FrameKind = 1
	FrameBinary FrameKind = 2
)

type FrameKind int

type FrameConn interface {
	ReadFrame(context.Context) (FrameKind, []byte, error)
	WriteFrame(context.Context, FrameKind, []byte) error
	Close() error
}
