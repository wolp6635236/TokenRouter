package payment

import (
	"context"
)

type InstanceFilter struct {
	EnabledOnly        bool
	ProviderKey        string
	SortByOrder        bool
	Limit              int
	UserRefundEligible bool
	ExcludeID          int64
}
type InstancePatch struct {
	ExpectedConfig  *string
	Name            *string
	Config          *string
	SupportedTypes  *string
	Enabled         *bool
	SortOrder       *int
	Limits          *string
	RefundEnabled   *bool
	AllowUserRefund *bool
	PaymentMode     *string
}
type ConfigurationStore interface {
	ListInstances(context.Context, InstanceFilter) ([]*ProviderInstance, error)
	Instance(context.Context, int64) (*ProviderInstance, error)
	CreateInstance(context.Context, ProviderInstance) (*ProviderInstance, error)
	UpdateInstance(context.Context, int64, InstancePatch) (*ProviderInstance, error)
	DeleteInstance(context.Context, int64) error
	CountInProgressByProvider(context.Context, int64) (int, error)
	CountInProgressByPlan(context.Context, int64) (int, error)
	CountForcedExpiredByProvider(context.Context, int64) (int, error)
	IsNotFound(error) bool
}

// InProgressOrderStatuses 保持原渠道配置保护和套餐删除的在途范围。
func InProgressOrderStatuses() []string {
	return []string{OrderStatusPending, OrderStatusProcessing, OrderStatusPaid, OrderStatusRecharging}
}
