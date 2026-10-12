package scheduler

import (
	"context"
	"sync/atomic"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

const (
	PlatformAnthropic   = capability.PlatformAnthropic
	PlatformOpenAI      = capability.PlatformOpenAI
	PlatformGemini      = capability.PlatformGemini
	PlatformAntigravity = capability.PlatformAntigravity
	PlatformQoder       = capability.PlatformQoder
	PlatformGrok        = capability.PlatformGrok
)

// runtimeSlotCache 用通道控制槽位释放，检查停止成功前已完成租约释放。
type runtimeSlotCache struct {
	ConcurrencyCache
	acquired        bool
	releaseStarted  chan struct{}
	releaseContinue chan struct{}
	releases        atomic.Int64
}

func (c *runtimeSlotCache) AcquireProviderSlot(context.Context, int64, int, string) (bool, error) {
	return c.acquired, nil
}

func (c *runtimeSlotCache) ReleaseProviderSlot(context.Context, int64, string) error {
	c.releases.Add(1)
	if c.releaseStarted != nil {
		close(c.releaseStarted)
		<-c.releaseContinue
	}
	return nil
}
