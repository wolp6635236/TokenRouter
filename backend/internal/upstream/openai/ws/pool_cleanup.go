package ws

import (
	"time"
)

// requestCleanup 合并清理通知，网络关闭由池的后台任务完成。
func (p *WSConnPool) requestCleanup() {
	select {
	case p.cleanupWakeCh <- struct{}{}:
	default:
	}
}

// releaseUndeliveredConn 归还尚未交付的租约，取消路径通过后台任务回收连接。
func (p *WSConnPool) releaseUndeliveredConn(providerID int64, conn *WSConn) {
	conn.release()
	p.notifyProviderPoolChanged(providerID)
	p.requestCleanup()
}

// connMaxAgeReached 检查连接是否达到最大寿命。
func (p *WSConnPool) connMaxAgeReached(conn *WSConn, now time.Time) bool {
	maxAge := p.maxConnAge()
	return conn != nil && maxAge > 0 && conn.age(now) >= maxAge
}

// retireExpiredConnsLocked 淘汰已经关闭或过期的空闲连接，调用方持有 ap.mu。
func (p *WSConnPool) retireExpiredConnsLocked(ap *openAIWSProviderPool, now time.Time) []*WSConn {
	var evicted []*WSConn
	for id, conn := range ap.conns {
		if conn == nil {
			delete(ap.conns, id)
			delete(ap.pinnedConns, id)
			continue
		}
		expired := false
		select {
		case <-conn.closedCh:
			expired = true
		default:
			if conn.isLeased() || p.isConnPinnedLocked(ap, id) {
				continue
			}
			// 等待者可以领取仍有效的连接，过期连接需要重新拨号。
			expired = p.connMaxAgeReached(conn, now)
			if !conn.supportsIdlePingWithoutReader() && conn.idleDuration(now) >= openAIWSConnIdleRecycleAfter {
				expired = true
				p.metrics.scaleDownTotal.Add(1)
			}
		}
		if expired {
			delete(ap.conns, id)
			delete(ap.pinnedConns, id)
			evicted = append(evicted, conn)
		}
	}
	if len(evicted) > 0 {
		ap.signalChangedLocked()
	}
	return evicted
}
