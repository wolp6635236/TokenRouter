package billing

import (
	"time"

	"github.com/TokenFlux/TokenRouter/internal/identity/contact"
)

// UserSummary 包含权益查询所需的用户资料。
type UserSummary struct {
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
	BalanceNotifyEnabled       bool
	BalanceNotifyThresholdType string
	BalanceNotifyThreshold     *float64
	BalanceNotifyExtraEmails   []NotifyEmailSummary
	TotalRecharged             float64
	RPMLimit                   int
	APIKeyLimit                int
	DeletedAt                  *time.Time
}

// NotifyEmailSummary 是身份联系邮箱记录。
type NotifyEmailSummary = contact.Entry

// ParseNotifyEmails 解析字符串数组或邮箱记录数组格式的通知邮箱。
func ParseNotifyEmails(raw string) []NotifyEmailSummary { return contact.ParseNotifyEmails(raw) }
