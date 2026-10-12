package ws

import (
	"context"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/upstream/openai"
)

// wsPoolWaiter 记录一次等待，preferred 非空时租约固定到该连接。
type wsPoolWaiter struct {
	compatibility wsConnCompatibility
	preferred     *WSConn
	startedAt     time.Time
}

// addWaiterLocked 检查提供商和指定连接的排队上限，调用方持有 ap.mu。
func (p *WSConnPool) addWaiterLocked(ap *openAIWSProviderPool, ctx context.Context, compatibility wsConnCompatibility, preferred *WSConn, maxConns int) (*wsPoolWaiter, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_, waiters := providerPoolLoadLocked(ap)
	limit := p.queueLimitPerConn()
	// 除法比较避免连接上限和排队上限相乘时溢出。
	if waiters/limit >= maxConns || preferred != nil && int(preferred.waiters.Load()) >= limit {
		return nil, openai.ErrOpenAIWSConnQueueFull
	}
	waiter := &wsPoolWaiter{compatibility: compatibility, preferred: preferred, startedAt: time.Now()}
	if preferred != nil {
		preferred.waiters.Add(1)
	} else {
		if ap.waiters == nil {
			ap.waiters = make(map[wsConnCompatibility]int)
		}
		ap.waiters[compatibility]++
	}
	return waiter, nil
}

// removeWaiterLocked 归还排队名额，调用方持有 ap.mu。
func removeWaiterLocked(ap *openAIWSProviderPool, waiter *wsPoolWaiter) time.Duration {
	if waiter.preferred != nil {
		waiter.preferred.waiters.Add(-1)
	} else {
		ap.waiters[waiter.compatibility]--
		if ap.waiters[waiter.compatibility] == 0 {
			delete(ap.waiters, waiter.compatibility)
		}
	}
	ap.signalChangedLocked()
	return time.Since(waiter.startedAt)
}

// hasCompatibleWaiterLocked 让已归还的连接等待兼容请求领取，调用方持有 ap.mu。
func hasCompatibleWaiterLocked(ap *openAIWSProviderPool, conn *WSConn) bool {
	return ap.waiters[conn.compatibility] > 0
}
