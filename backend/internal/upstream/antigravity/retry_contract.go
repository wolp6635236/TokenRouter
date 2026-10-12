package antigravity

import (
	"errors"
	"fmt"
	"sync"
	"time" // 限流相关常量
)

const (
	AntigravityMaxRetries               = 3
	AntigravityRetryBaseDelay           = 1 * time.Second
	AntigravityRetryMaxDelay            = 16 * time.Second
	AntigravityRateLimitThreshold       = 7 * time.Second
	AntigravitySmartRetryMinWait        = 1 * time.Second  // 智能重试最小等待时间
	AntigravitySmartRetryMaxAttempts    = 1                // 智能重试最大次数（仅重试 1 次，防止重复限流/长期等待）
	AntigravityDefaultRateLimitDuration = 30 * time.Second // 默认限流时间（无 retryDelay 时使用）

	// MODEL_CAPACITY_EXHAUSTED 专用重试参数
	// 模型容量不足时，所有提供商共享同一容量池，切换提供商无意义
	// 使用固定 1s 间隔重试，最多重试 60 次。
	AntigravityModelCapacityRetryMaxAttempts = 60
	AntigravityModelCapacityRetryWait        = 1 * time.Second

	// Google RPC 状态和类型常量。
	GoogleRPCStatusResourceExhausted      = "RESOURCE_EXHAUSTED"
	GoogleRPCStatusUnavailable            = "UNAVAILABLE"
	GoogleRPCTypeRetryInfo                = "type.googleapis.com/google.rpc.RetryInfo"
	GoogleRPCTypeErrorInfo                = "type.googleapis.com/google.rpc.ErrorInfo"
	GoogleRPCReasonModelCapacityExhausted = "MODEL_CAPACITY_EXHAUSTED"
	GoogleRPCReasonRateLimitExceeded      = "RATE_LIMIT_EXCEEDED"

	// 单提供商 503 退避重试：Service 层原地重试的最大次数
	// 在 HandleSmartRetry 中，对于 shouldRateLimitModel（长延迟 ≥ 7s）的情况，
	// 多提供商模式下会设限流+切换提供商；但单提供商模式下改为原地等待+重试。
	AntigravitySingleProviderSmartRetryMaxAttempts = 3

	// 单提供商 503 退避重试：原地重试时单次最大等待时间
	// 防止上游返回过长的 retryDelay 导致请求卡住太久。
	AntigravitySingleProviderSmartRetryMaxWait = 15 * time.Second

	// 单提供商 503 退避重试：原地重试的总累计等待时间上限
	// 超过此上限将不再重试，直接返回 503。
	AntigravitySingleProviderSmartRetryTotalMaxWait = 30 * time.Second

	// MODEL_CAPACITY_EXHAUSTED 全局去重：重试全部失败后的 cooldown 时间。
	AntigravityModelCapacityCooldown = 10 * time.Second
)

// MODEL_CAPACITY_EXHAUSTED 全局去重：避免多个并发请求同时对同一模型进行容量耗尽重试。
var (
	modelCapacityExhaustedMu    sync.RWMutex
	modelCapacityExhaustedUntil = make(map[string]time.Time) // modelName -> cooldown until
)

// AntigravityProviderSwitchError 提供商切换信号
// 当提供商限流时间超过阈值时，通知上层切换提供商。
type AntigravityProviderSwitchError struct {
	OriginalProviderID int64
	RateLimitedModel   string
	IsStickySession    bool // 是否为粘性会话切换（决定是否缓存计费）
}

func (e *AntigravityProviderSwitchError) Error() string {
	return fmt.Sprintf("provider %d model %s rate limited, need switch",
		e.OriginalProviderID, e.RateLimitedModel)
}

// IsAntigravityProviderSwitchError 检查错误是否为提供商切换信号。
func IsAntigravityProviderSwitchError(err error) (*AntigravityProviderSwitchError, bool) {
	if switchErr, ok := errors.AsType[*AntigravityProviderSwitchError](err); ok {
		return switchErr, true
	}
	return nil, false
}
