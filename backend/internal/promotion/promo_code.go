package promotion

import (
	"time"

	"github.com/TokenFlux/TokenRouter/internal/identity/contact"
)

const (
	PromoCodeStatusActive   = "active"
	PromoCodeStatusDisabled = "disabled"
)

// PromoCode 注册优惠码。
type PromoCode struct {
	ID          int64
	Code        string
	BonusAmount float64
	MaxUses     int
	UsedCount   int
	Status      string
	ExpiresAt   *time.Time
	Notes       string
	CreatedAt   time.Time
	UpdatedAt   time.Time

	// 关联
	UsageRecords []PromoCodeUsage
}

// PromoCodeUsage 优惠码使用记录。
type PromoCodeUsage struct {
	ID          int64
	PromoCodeID int64
	UserID      int64
	BonusAmount float64
	UsedAt      time.Time

	// 关联
	PromoCode *PromoCode
	User      *UserView
}

// CreatePromoCodeInput 创建优惠码输入。
type CreatePromoCodeInput struct {
	Code        string
	BonusAmount float64
	MaxUses     int
	ExpiresAt   *time.Time
	Notes       string
}

// UpdatePromoCodeInput 更新优惠码输入。
type UpdatePromoCodeInput struct {
	Code        *string
	BonusAmount *float64
	MaxUses     *int
	Status      *string
	ExpiresAt   *time.Time
	Notes       *string
}

// UserView 包含优惠码使用记录中的用户资料。
type UserView struct {
	ID                         int64
	Email                      string
	Username                   string
	Role                       string
	Balance                    float64
	FrozenBalance              float64
	Concurrency                int
	Status                     string
	AllowedGroups              []int64
	DisabledPublicGroups       []int64
	LastActiveAt               *time.Time
	CreatedAt                  time.Time
	UpdatedAt                  time.Time
	DeletedAt                  *time.Time
	BalanceNotifyEnabled       bool
	BalanceNotifyThresholdType string
	BalanceNotifyThreshold     *float64
	BalanceNotifyExtraEmails   []contact.Entry
	TotalRecharged             float64
	RPMLimit                   int
	APIKeyLimit                int
}

func (p *PromoCode) CanUseAt(now time.Time) bool {
	if p.Status != PromoCodeStatusActive {
		return false
	}
	if p.ExpiresAt != nil && now.After(*p.ExpiresAt) {
		return false
	}
	if p.MaxUses > 0 && p.UsedCount >= p.MaxUses {
		return false
	}
	return true
}

// IsExpired 检查是否已过期。
func (p *PromoCode) IsExpired() bool { return p.IsExpiredAt(time.Now()) }

func (p *PromoCode) IsExpiredAt(now time.Time) bool {
	return p.ExpiresAt != nil && now.After(*p.ExpiresAt)
}
