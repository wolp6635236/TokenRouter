package identity

import (
	"slices"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/identity/contact"
)

const (
	RoleAdmin    = "admin"
	RoleUser     = "user"
	StatusActive = "active"

	DefaultUserAPIKeyLimit              = 100
	MaxUserAPIKeyLimit                  = 2_147_483_647
	LinuxDoConnectSyntheticEmailDomain  = "@linuxdo-connect.invalid"
	OIDCConnectSyntheticEmailDomain     = "@oidc-connect.invalid"
	WeChatConnectSyntheticEmailDomain   = "@wechat-connect.invalid"
	DingTalkConnectSyntheticEmailDomain = "@dingtalk-connect.invalid"
)

// ErrUserNotFound 与资金消费者维持同一个用户缺失错误。
var ErrUserNotFound = billing.ErrUserNotFound

type User struct {
	Subscriptions []billing.UserSubscription
	ID            int64
	Email         string
	// PreferredLocale 为空时使用浏览器或站点默认语言。
	PreferredLocale *string
	Username        string
	Notes           string
	AvatarURL       string
	AvatarSource    string
	AvatarMIME      string
	AvatarByteSize  int
	AvatarSHA256    string
	PasswordHash    string
	Role            string
	Balance         float64
	FrozenBalance   float64
	Concurrency     int
	Status          string
	AllowedGroups   []int64
	// DisabledPublicGroups 保存管理员为该用户显式禁止使用的公开分组 ID。
	DisabledPublicGroups []int64
	// GroupRestrictionsLoaded 表示用户分组授权/禁用列表已从仓储加载，避免认证缓存误判空列表。
	GroupRestrictionsLoaded bool
	TokenVersion            int64 // Incremented on password change to invalidate existing tokens
	// TokenVersionResolved indicates TokenVersion already contains the fingerprint-derived
	// value expected in JWT claims and refresh-token state.
	TokenVersionResolved bool
	SignupSource         string
	LastLoginAt          *time.Time
	LastActiveAt         *time.Time
	LastUsedAt           *time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
	DeletedAt            *time.Time // 非 nil 表示用户已软删除

	// GroupRates 用户专属分组倍率配置
	// map[groupID]rateMultiplier
	GroupRates map[int64]float64

	// TOTP 双因素认证字段
	TotpSecretEncrypted *string    // AES-256-GCM 加密的 TOTP 密钥
	TotpEnabled         bool       // 是否启用 TOTP
	TotpEnabledAt       *time.Time // TOTP 启用时间

	// 余额不足通知
	BalanceNotifyEnabled       bool
	BalanceNotifyThresholdType string // "fixed" (default) | "percentage"
	BalanceNotifyThreshold     *float64
	BalanceNotifyExtraEmails   []NotifyEmailEntry
	TotalRecharged             float64

	// RPMLimit 用户级每分钟请求数上限（0 = 不限制）。仅在所用分组未设置 rpm_limit
	// 且该 (用户, 分组) 无 rpm_override 时作为全局兜底生效，计数键 rpm:u:{userID}:{min}。
	RPMLimit int
	// APIKeyLimit 是用户可创建的 API Key 数量上限，0 表示不限制。
	APIKeyLimit int

	// UserGroupRPMOverride 来自 auth cache snapshot 的 (user, group) RPM 覆盖值。
	// nil = 该 API Key 对应的 (user, group) 无 override；非 nil 时 checkRPM 直接使用，
	// 避免每请求查 DB。字段不持久化到数据库。
	UserGroupRPMOverride *int
}

// NotifyEmailEntry 是已验证的通知邮箱值；验证流程归用户用例。
type NotifyEmailEntry = contact.Entry

// Principal 是通过认证的调用方身份。
type Principal struct {
	UserID         int64
	Role           string
	SessionID      string
	CredentialKind string
}

func (u *User) IsAdmin() bool {
	return u.Role == RoleAdmin
}

func (u *User) IsActive() bool {
	return u.Status == StatusActive
}

// CanBindGroup 检查用户是否可以绑定指定分组。
// 公开分组默认可用，但如果出现在 DisabledPublicGroups 中则禁止；专属分组必须在 AllowedGroups 中。
func (u *User) CanBindGroup(groupID int64, isExclusive bool) bool {
	if u == nil || groupID <= 0 {
		return false
	}
	if !isExclusive {
		// 公开分组默认可用，只按用户级禁用列表做排除。
		return !slices.Contains(u.DisabledPublicGroups, groupID)
	}
	// 专属分组：需要在 AllowedGroups 中
	return slices.Contains(u.AllowedGroups, groupID)
}

func (u *User) SetPassword(password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u.PasswordHash = string(hash)
	return nil
}

func (u *User) CheckPassword(password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) == nil
}

// ParseNotifyEmails 复用身份联系邮箱序列化兼容。
func ParseNotifyEmails(raw string) []NotifyEmailEntry { return contact.ParseNotifyEmails(raw) }

func MarshalNotifyEmails(entries []NotifyEmailEntry) string {
	return contact.MarshalNotifyEmails(entries)
}

func IsReservedEmail(email string) bool {
	normalized := strings.ToLower(strings.TrimSpace(email))
	return strings.HasSuffix(normalized, LinuxDoConnectSyntheticEmailDomain) ||
		strings.HasSuffix(normalized, OIDCConnectSyntheticEmailDomain) ||
		strings.HasSuffix(normalized, WeChatConnectSyntheticEmailDomain) ||
		strings.HasSuffix(normalized, DingTalkConnectSyntheticEmailDomain)
}

// CopyUser 浅复制用户结构体。
// 关联切片、映射和指针共享引用。跨请求缓存通过各自的入口深复制。
func CopyUser(user *User) *User {
	if user == nil {
		return nil
	}
	copy := *user
	return &copy
}
