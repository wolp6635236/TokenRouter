package httpapi

import (
	"net/http"
	"strings"

	coderws "github.com/coder/websocket"
)

// IsResponsesWSUpgrade 根据 Upgrade 和 Connection 头判断升级请求。
func IsResponsesWSUpgrade(r *http.Request) bool {
	if r == nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(r.Header.Get("Upgrade")), "websocket") {
		return false
	}
	return strings.Contains(strings.ToLower(strings.TrimSpace(r.Header.Get("Connection"))), "upgrade")
}

// CloseResponsesWS 截断关闭文本，发送关闭帧后关闭连接。
func CloseResponsesWS(conn *coderws.Conn, status coderws.StatusCode, reason string) {
	if conn == nil {
		return
	}
	reason = strings.TrimSpace(reason)
	if len(reason) > 120 {
		reason = reason[:120]
	}
	_ = conn.Close(status, reason)
	_ = conn.CloseNow()
}
