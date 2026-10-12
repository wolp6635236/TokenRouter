package httpapi

import (
	"context"

	coderws "github.com/coder/websocket"

	gatewayws "github.com/TokenFlux/TokenRouter/internal/gateway/ws"
)

var _ gatewayws.ClientSocket = WSClientFrames{}

// WSClientFrames 转换网络库的帧和关闭枚举。
type WSClientFrames struct{ Conn *coderws.Conn }

func (c WSClientFrames) Read(ctx context.Context) (int, []byte, error) {
	typ, body, err := c.Conn.Read(ctx)
	return int(typ), body, err
}

// Write 同步返回网络写入结果，入站执行器据此处理关闭和取消。
func (c WSClientFrames) Write(ctx context.Context, typ int, body []byte) error {
	return c.Conn.Write(ctx, coderws.MessageType(typ), body)
}

func (c WSClientFrames) Close(status int, reason string) error {
	return c.Conn.Close(coderws.StatusCode(status), reason)
}
func (c WSClientFrames) CloseNow() error { return c.Conn.CloseNow() }
