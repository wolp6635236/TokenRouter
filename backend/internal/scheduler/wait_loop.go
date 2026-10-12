package scheduler

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"
)

// 等待间隔从 100 毫秒开始，按 1.5 倍增长，最大为 2 秒。
const (
	InitialBackoff    = 100 * time.Millisecond
	MaxBackoff        = 2 * time.Second
	backoffMultiplier = 1.5
)

// ConcurrencyError 记录槽位类别和等待是否超时。
type ConcurrencyError struct {
	SlotType  string
	IsTimeout bool
}

// WaitQueueFullError 表示用户等待队列已满。
type WaitQueueFullError struct {
	SlotType string
}

// WaitObserver 同步发送等待事件，Begin 在进入等待时调用。立即取得槽位时跳过 Begin。
type WaitObserver struct {
	Interval  time.Duration
	Begin     func() error
	Heartbeat func() error
}

// UserAcquireOptions 仅包含本次准入及释放所需的独立输入。
type UserAcquireOptions struct {
	UserID   int64
	APIKeyID int64
	Limit    int
	Timeout  time.Duration
	Mode     ReleaseMode
	Observer WaitObserver
}

func (e *ConcurrencyError) Error() string {
	if e.IsTimeout {
		return fmt.Sprintf("timeout waiting for %s concurrency slot", e.SlotType)
	}
	return fmt.Sprintf("%s concurrency limit reached", e.SlotType)
}

func (e *WaitQueueFullError) Error() string {
	return "Too many pending requests, please retry later"
}

// WaitForSlot 可先立即尝试获取槽位，失败后按退避间隔重试，直到取得槽位、父 context 取消或超时。
func (s *ConcurrencyService) WaitForSlot(parent context.Context, slotType string, id int64, limit int, timeout time.Duration, immediate bool, observer WaitObserver) (func(), error) {
	operation, finish, err := s.runtime.Enter(parent, "slot-wait")
	if err != nil {
		return nil, err
	}
	defer finish()
	ctx, cancel := context.WithTimeout(operation, timeout)
	defer cancel()
	acquire := func() (*AcquireResult, error) {
		if slotType == "user" {
			return s.AcquireUserSlot(ctx, id, limit)
		}
		return s.AcquireProviderSlot(ctx, id, limit)
	}
	if immediate {
		result, err := acquire()
		if err != nil {
			return nil, err
		}
		if result.Acquired {
			return result.ReleaseFunc, nil
		}
	}
	if observer.Begin != nil {
		if err := observer.Begin(); err != nil {
			return nil, err
		}
	}
	var ping <-chan time.Time
	if observer.Heartbeat != nil && observer.Interval > 0 {
		t := time.NewTicker(observer.Interval)
		defer t.Stop()
		ping = t.C
	}
	backoff := InitialBackoff
	timer := time.NewTimer(backoff)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			if err := parent.Err(); err != nil {
				return nil, err
			}
			if operation.Err() != nil {
				return nil, operation.Err()
			}
			return nil, &ConcurrencyError{SlotType: slotType, IsTimeout: true}
		case <-ping:
			if err := observer.Heartbeat(); err != nil {
				return nil, err
			}
		case <-timer.C:
			result, err := acquire()
			if err != nil {
				return nil, err
			}
			if result.Acquired {
				return result.ReleaseFunc, nil
			}
			backoff = NextBackoff(backoff)
			timer.Reset(backoff)
		}
	}
}

// AcquireUser 拥有实际取得的用户槽和 Key 统计槽，等待名额在离开队列时归还。
func (s *ConcurrencyService) AcquireUser(ctx context.Context, options UserAcquireOptions) (*Lease, WaitResult, error) {
	result, err := s.AcquireUserSlot(ctx, options.UserID, options.Limit)
	if err != nil {
		return nil, WaitResult{}, err
	}
	var waited WaitResult
	release := result.ReleaseFunc
	if !result.Acquired {
		limit := max(CalculateMaxWait(options.Limit)-options.Limit, 1)
		waited, err = s.EnterUserWait(ctx, options.UserID, limit)
		if err != nil {
			return nil, waited, err
		}
		if !waited.Allowed {
			return nil, waited, &WaitQueueFullError{SlotType: "user"}
		}
		defer waited.Release()
		release, err = s.WaitForSlot(ctx, "user", options.UserID, options.Limit, options.Timeout, false, options.Observer)
		if err != nil {
			return nil, waited, err
		}
	}
	// 先登记用户槽的释放函数，再尝试登记统计槽。调用方选择取消时的释放方式。
	lease := NewLease(context.Background(), ReleaseOnCompletion, release)
	if options.APIKeyID > 0 {
		keyRelease := s.TrackAPIKeySlot(ctx, options.APIKeyID)
		// 保留用户槽先释放、统计槽随后释放的兼容次序。
		combined := NewLease(ctx, options.Mode, func() {
			lease.Release()
			if keyRelease != nil {
				keyRelease()
			}
		})
		return combined, waited, nil
	}
	return NewLease(ctx, options.Mode, lease.Release), waited, nil
}

func NextBackoff(current time.Duration) time.Duration {
	// 指数退避：当前时间 * 1.5
	next := min(time.Duration(float64(current)*backoffMultiplier), MaxBackoff)
	// 添加 ±20% 的随机抖动（jitter 范围 0.8 ~ 1.2）
	// 抖动将多个请求的 Redis 重试分散到不同时间点。
	jitter := 0.8 + rand.Float64()*0.4
	jittered := time.Duration(float64(next) * jitter)
	if jittered < InitialBackoff {
		return InitialBackoff
	}
	if jittered > MaxBackoff {
		return MaxBackoff
	}
	return jittered
}
