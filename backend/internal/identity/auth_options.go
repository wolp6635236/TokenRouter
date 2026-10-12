package identity

import (
	"context"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/billing"
)

const (
	RedeemTypeInvitation = billing.RedeemTypeInvitation
	StatusUnused         = billing.StatusUnused
	StatusUsed           = billing.StatusUsed
)

var ErrRedeemCodeNotFound = billing.ErrRedeemCodeNotFound

type ProviderDefaultGrantSettings struct {
	Balance          float64
	Concurrency      int
	Subscriptions    []DefaultSubscriptionSetting
	GrantOnSignup    bool
	GrantOnFirstBind bool
}

type AuthSourceDefaultSettings struct {
	Email                        ProviderDefaultGrantSettings
	LinuxDo                      ProviderDefaultGrantSettings
	OIDC                         ProviderDefaultGrantSettings
	WeChat                       ProviderDefaultGrantSettings
	GitHub                       ProviderDefaultGrantSettings
	Google                       ProviderDefaultGrantSettings
	DingTalk                     ProviderDefaultGrantSettings
	ForceEmailOnThirdPartySignup bool
}

type DefaultSubscriptionSetting = billing.DefaultSubscriptionSetting

// AuthOptions 包含启动时的认证参数。
type AuthOptions struct {
	Default struct {
		UserBalance     float64
		UserConcurrency int
	}
	JWT       SessionOptions
	Server    struct{ Mode string }
	Turnstile struct{ Required bool }
}
type DingTalkRegistrationPolicy struct {
	Enabled, BypassRegistration bool
	CorpRestrictionPolicy       string
}
type (
	RedeemCode              = billing.RedeemCode
	RedeemCodeRepository    = billing.RedeemCodeRepository
	AssignSubscriptionInput = billing.AssignSubscriptionInput
	UserSubscription        = billing.UserSubscription
)

// AuthIdentity 是提供方持久主体的只读值。
type AuthIdentity struct {
	ID              int64          `json:"id,omitempty"`
	CreatedAt       time.Time      `json:"created_at,omitempty"`
	UpdatedAt       time.Time      `json:"updated_at,omitempty"`
	UserID          int64          `json:"user_id,omitempty"`
	ProviderType    string         `json:"provider_type,omitempty"`
	ProviderKey     string         `json:"provider_key,omitempty"`
	ProviderSubject string         `json:"provider_subject,omitempty"`
	VerifiedAt      *time.Time     `json:"verified_at,omitempty"`
	Issuer          *string        `json:"issuer,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
}

// AuthSettings 按动作取得动态策略，不能缓存为启动时副本。
type AuthSettings interface {
	GetCaptchaProviderConfig(ctx context.Context) (CaptchaProviderConfig, error)
	GetDefaultBalance(ctx context.Context) float64
	GetDefaultConcurrency(ctx context.Context) int
	GetDefaultSubscriptions(ctx context.Context) []DefaultSubscriptionSetting
	GetDefaultUserAPIKeyLimit(ctx context.Context) int
	GetDefaultUserRPMLimit(ctx context.Context) int
	GetDingTalkConnectOAuthConfig(ctx context.Context) (DingTalkRegistrationPolicy, error)
	GetRegistrationEmailSuffixWhitelist(ctx context.Context) []string
	GetSiteName(ctx context.Context) string
	IsEmailVerifyEnabled(ctx context.Context) bool
	IsInvitationCodeEnabled(ctx context.Context) bool
	IsPasswordResetEnabled(ctx context.Context) bool
	IsPromoCodeEnabled(ctx context.Context) bool
	IsRegistrationEmailDomainQuotaEnabled(ctx context.Context) bool
	IsRegistrationEmailNormalizationEnabled(ctx context.Context) bool
	IsRegistrationEnabled(ctx context.Context) bool
	IsSessionBindingEnabled(ctx context.Context) bool
	IsUserEmailChangeEnabled(ctx context.Context) bool
	ResolveAuthSourceGrantSettings(ctx context.Context, signupSource string, firstBind bool) (ProviderDefaultGrantSettings, bool, error)
}

type AuthEmail interface {
	ConsumePasswordResetToken(ctx context.Context, email, token string) error
	SendPasswordResetEmail(ctx context.Context, email, siteName, resetURL string, locale ...string) error
	SendVerifyCode(ctx context.Context, email, siteName string, locale ...string) error
	VerifyCode(ctx context.Context, email, code string) error
}

type AuthEmailQueue interface {
	EnqueuePasswordReset(email, siteName, resetURL string, locale ...string) error
	EnqueueVerifyCode(email, siteName string, locale ...string) error
}

type AuthPromo interface {
	ApplyPromoCode(context.Context, int64, string) error
}
type AuthAffiliate interface {
	EnsureUserAffiliate(context.Context, int64) error
	BindInviterByCode(context.Context, int64, string) error
}

// AuthDependencies 由 app 一次装配，端口共享原有存储与缓存实例。
type AuthDependencies struct {
	Users                   UserRepository
	Redeem                  RedeemCodeRepository
	RefreshTokens           RefreshTokenCache
	Options                 *AuthOptions
	Settings                AuthSettings
	Email                   AuthEmail
	Turnstile               *TurnstileService
	Tencent                 *TencentCaptchaService
	Aliyun                  *AliyunCaptchaService
	EmailQueue              AuthEmailQueue
	Promo                   AuthPromo
	Affiliate               AuthAffiliate
	DefaultSubscriptions    DefaultSubscriptionAssigner
	Invalidator             UserAuthInvalidator
	BalanceCache            UserBalanceCache
	Observer                Observer
	DomainRegistration      RegistrationEmailDomainRepository
	NormalizedEmailConflict AuthNormalizedEmailBindingConflictChecker
	EmailAliasGuard         AuthEmailIdentityAliasGuardRepository
	AliasLookup             EmailAliasLookupRepository
	AliasOwner              EmailAliasOwnerLookupRepository
}

// AuthService 拥有注册、登录与绑定规则，闭合 SQL 操作通过 Storage 执行。
// @project-doc docs/domains/identity_and_tenancy.md#authentication_boundaries
type AuthService struct {
	*AuthDependencies
	*SessionService
	Storage AuthStorage
}

// EmailAliasLookupRepository 是邮箱别名查重的可选仓储能力。
// 保持 UserRepository 主接口不变，以兼容仅用于其他服务的测试桩。
type EmailAliasLookupRepository interface {
	ExistsByEmailAlias(ctx context.Context, email string) (bool, error)
}

// EmailAliasOwnerLookupRepository 能在查重时区分当前用户和其他用户。
// 该能力由真实数据库仓储提供，避免用户把自己的 alias 变体误判为冲突。
type EmailAliasOwnerLookupRepository interface {
	EmailAliasOwnerID(ctx context.Context, email string, currentUserID int64) (int64, bool, error)
}

func NewAuthService(deps *AuthDependencies, storage AuthStorage) *AuthService {
	options := SessionOptions{}
	if deps.Options != nil {
		options = deps.Options.JWT
	}
	return &AuthService{AuthDependencies: deps, SessionService: NewSessionService(options, deps.Users, deps.RefreshTokens, deps.Settings, deps.Observer.Log), Storage: storage}
}
func (s *AuthService) HasDatabase() bool { return s.Storage != nil && s.Storage.HasDatabase() }
