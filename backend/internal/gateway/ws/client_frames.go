package ws

import (
	"context"
	"errors"
	"sync/atomic"
	"time"
)

type clientFrameConn struct {
	closeError           func(int, string, error) error
	closedError          error
	normalizeCompleted   func([]byte) ([]byte, bool)
	conn                 ClientSocket
	controlCtx           context.Context
	interTurnIdleTimeout time.Duration
	idleTimeout          func() time.Duration
	interTurnStarted     chan struct{}
	waitingForNextTurn   atomic.Bool
	// The relay observes upstream payloads, while clients must keep seeing the
	// model identifier they supplied for the current turn.
	restoreResponseModel func([]byte) []byte
	restoreToolNames     func([]byte) []byte
}

// policyFrameConn 包装客户端 FrameConn，每个发往上游的帧先通过 OpenAI Fast 策略。
// filter 返回新 payload 时转发，返回 PolicyBlocked 时通过 onBlock 发送错误事件并终止 relay 读取，
// 其他错误按传输错误返回。入站会话通过 parseClientPayload 执行同类检查。
type policyFrameConn struct {
	closeError  func(int, string, error) error
	closedError error
	inner       FrameConn
	filter      func(msgType int, payload []byte) ([]byte, *PolicyBlocked, error)
	writeFilter func(msgType int, payload []byte) ([]byte, error)
	onBlock     func(blocked *PolicyBlocked)
}

// TurnActivity 通过同一个原子标记协调终态提交和下一轮空闲等待。
type TurnActivity struct {
	Waiting *atomic.Bool
	Started chan struct{}
}

func (c *policyFrameConn) ReadFrame(ctx context.Context) (int, []byte, error) {
	if c == nil || c.inner == nil {
		return TextFrame, nil, c.closedError
	}
	msgType, payload, err := c.inner.ReadFrame(ctx)
	if err != nil {
		return msgType, payload, err
	}
	if c.filter == nil {
		return msgType, payload, nil
	}
	updated, blocked, filterErr := c.filter(msgType, payload)
	if filterErr != nil {
		return msgType, payload, filterErr
	}
	if blocked != nil {
		if c.onBlock != nil {
			c.onBlock(blocked)
		}
		return msgType, nil, c.closeError(1008, blocked.Message, blocked)
	}
	return msgType, updated, nil
}

func (c *policyFrameConn) WriteFrame(ctx context.Context, msgType int, payload []byte) error {
	if c == nil || c.inner == nil {
		return c.closedError
	}
	if c.writeFilter != nil {
		updated, err := c.writeFilter(msgType, payload)
		if err != nil {
			return err
		}
		payload = updated
	}
	return c.inner.WriteFrame(ctx, msgType, payload)
}

func (c *policyFrameConn) Close() error {
	if c == nil || c.inner == nil {
		return nil
	}
	return c.inner.Close()
}

func (c *clientFrameConn) ReadFrame(ctx context.Context) (int, []byte, error) {
	if c == nil || c.conn == nil {
		return TextFrame, nil, c.closedError
	}
	controlCtx := ctx
	if c.controlCtx != nil {
		controlCtx = c.controlCtx
	}
	idleTimeout := c.interTurnIdleTimeout
	if c.idleTimeout != nil {
		idleTimeout = c.idleTimeout()
	}
	msgType, payload, err := ReadClientMessageWithTimeoutStart(
		controlCtx,
		c.conn,
		idleTimeout,
		1000,
		"websocket idle timeout",
		c.interTurnStarted,
		func() bool { return c.waitingForNextTurn.Load() },
	)
	if closeErr, ok := errors.AsType[*ClientCloseError](err); ok {
		err = c.closeError(closeErr.Status, closeErr.Reason, closeErr.Cause)
	}
	return msgType, payload, err
}

func (c *clientFrameConn) markTurnStarted() {
	if c != nil {
		TurnActivity{Waiting: &c.waitingForNextTurn, Started: c.interTurnStarted}.MarkStarted()
	}
}

func (c *clientFrameConn) markTurnCompleted() {
	if c != nil {
		TurnActivity{Waiting: &c.waitingForNextTurn, Started: c.interTurnStarted}.MarkCompleted()
	}
}

func (c *clientFrameConn) WriteFrame(ctx context.Context, msgType int, payload []byte) error {
	if c == nil || c.conn == nil {
		return c.closedError
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if msgType == TextFrame {
		if normalized, changed := c.normalizeCompleted(payload); changed {
			payload = normalized
		}
		if c.restoreResponseModel != nil {
			payload = c.restoreResponseModel(payload)
		}
		if c.restoreToolNames != nil {
			payload = c.restoreToolNames(payload)
		}
	}
	// 控制取消后，读路径发送带原因的关闭帧。写入使用原截止时间完成当前帧，再由关闭握手结束连接。
	// 若写入直接继承父取消，coder/websocket 可能在帧已到达客户端、Write 尚未返回时关闭 TCP。
	writeCtx := context.WithoutCancel(ctx)
	if deadline, ok := ctx.Deadline(); ok {
		var cancel context.CancelFunc
		writeCtx, cancel = context.WithDeadline(writeCtx, deadline)
		defer cancel()
	}
	return c.conn.Write(writeCtx, msgType, payload)
}

func (c *clientFrameConn) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	_ = c.conn.Close(1000, "")
	_ = c.conn.CloseNow()
	return nil
}

// NewPolicyFrames 组合同步入站/出站过滤，供 HTTP 与协议 Adapter 复用。
func NewPolicyFrames(inner FrameConn, filter func(int, []byte) ([]byte, *PolicyBlocked, error), writeFilter func(int, []byte) ([]byte, error), onBlock func(*PolicyBlocked), closeError func(int, string, error) error, closedError error) FrameConn {
	return &policyFrameConn{inner: inner, filter: filter, writeFilter: writeFilter, onBlock: onBlock, closeError: closeError, closedError: closedError}
}

func (a TurnActivity) MarkStarted() { a.Waiting.Store(false) }
func (a TurnActivity) MarkCompleted() {
	a.Waiting.Store(true)
	select {
	case a.Started <- struct{}{}:
	default:
	}
}
