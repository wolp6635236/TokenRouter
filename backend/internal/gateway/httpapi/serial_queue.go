package httpapi

import (
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/TokenFlux/TokenRouter/internal/scheduler"
)

// UserMsgQueueHelper 用户消息串行队列 Handler 层辅助
// 复用 ConcurrencyHelper 的退避 + SSE ping 模式。
type UserMsgQueueHelper struct {
	queueService *scheduler.UserMessageQueueService
	pingFormat   SSEPingFormat
	pingInterval time.Duration
}

// NewUserMsgQueueHelper 创建用户消息串行队列辅助。
func NewUserMsgQueueHelper(
	queueService *scheduler.UserMessageQueueService,
	pingFormat SSEPingFormat,
	pingInterval time.Duration,
) *UserMsgQueueHelper {
	if pingInterval <= 0 {
		pingInterval = DefaultPingInterval
	}
	return &UserMsgQueueHelper{
		queueService: queueService,
		pingFormat:   pingFormat,
		pingInterval: pingInterval,
	}
}

// AcquireWithWait 等待获取串行锁，流式请求期间发送 SSE ping
// 返回的 releaseFunc 使用 sync.Once 释放一次。
func (h *UserMsgQueueHelper) AcquireWithWait(c *gin.Context, providerID int64, baseRPM int, isStream bool, streamStarted *bool, timeout time.Duration, reqLog *zap.Logger) (func(), error) {
	lease, err := h.queueService.AcquireWithWait(c.Request.Context(), providerID, baseRPM, timeout, h.observer(c, providerID, isStream, streamStarted, reqLog))
	if err != nil {
		return nil, err
	}
	return lease.Release, nil
}

// observer 保留请求日志字段与 SSE 格式，等待和资源所有权由 scheduler 处理。
func (h *UserMsgQueueHelper) observer(c *gin.Context, providerID int64, isStream bool, started *bool, reqLog *zap.Logger) scheduler.QueueObserver {
	return scheduler.QueueObserver{
		Wait: WaitObserver(c, h.pingFormat, h.pingInterval, isStream, started, false),
		Event: func(name string, id int64, err error) {
			if err != nil {
				reqLog.Warn(name, zap.Int64("provider_id", id), zap.Error(err))
			} else {
				reqLog.Debug(name, zap.Int64("provider_id", id))
			}
		}, Delay: func(delay time.Duration) {
			reqLog.Debug("gateway.umq_throttle_delay", zap.Int64("provider_id", providerID), zap.Duration("delay", delay))
		},
	}
}

// ThrottleWithPing 保留 HTTP 入口，核心执行软限速等待。
func (h *UserMsgQueueHelper) ThrottleWithPing(c *gin.Context, providerID int64, baseRPM int, isStream bool, streamStarted *bool, timeout time.Duration, reqLog *zap.Logger) error {
	return h.queueService.ThrottleWithWait(c.Request.Context(), providerID, baseRPM, timeout, h.observer(c, providerID, isStream, streamStarted, reqLog))
}
