package billing

import (
	"context"
)

// Observe 接收结构化组件和原日志内容，由 app 连接唯一日志后端。
type Observe func(component, format string, args ...any)

// EligibilityOptions 是计费准入的独立运行参数。
type EligibilityOptions struct {
	Dates   DateRuntime
	Billing BillingOptions
}
type BillingOptions struct {
	MinimumBalanceReserve float64
	CircuitBreaker        CircuitBreakerOptions
}
type CircuitBreakerOptions struct {
	Enabled             bool
	FailureThreshold    int
	ResetTimeoutSeconds int
	HalfOpenRequests    int
}

// BalanceReader 查询用户权益快照，余额更新使用原子写入接口。
type BalanceReader interface {
	GetByID(context.Context, int64) (*UserSummary, error)
}
type APIKeyRateLimitLoader interface {
	GetRateLimitData(context.Context, int64) (*APIKeyRateLimitData, error)
}

// KeySnapshot 包含 Key 的消费准入字段。
type KeySnapshot struct {
	ID          int64
	BillingMode string
	RateLimit5h float64
	RateLimit1d float64
	RateLimit7d float64
}

// GroupSnapshot 标识最终消费分组。
type GroupSnapshot struct{ ID int64 }

// CheckInput 包含付款人、Key 和分组的资金准入数据。
type CheckInput struct {
	Payer        *UserSummary
	Key          *KeySnapshot
	Group        *GroupSnapshot
	Subscription *UserSubscription
	Platform     string
}

func (o Observe) Printf(component, format string, args ...any) {
	if o != nil {
		o(component, format, args...)
	}
}

func (k *KeySnapshot) HasRateLimits() bool {
	return k.RateLimit5h > 0 || k.RateLimit1d > 0 || k.RateLimit7d > 0
}

func effectiveKeyBillingMode(k *KeySnapshot) string {
	if k == nil {
		return APIKeyBillingModeAuto
	}
	mode, ok := NormalizeAPIKeyBillingMode(k.BillingMode)
	if !ok {
		return APIKeyBillingModeAuto
	}
	return mode
}
