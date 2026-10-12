package provider

import (
	"context"
	"time"
)

const codexImportClockSkewSeconds int64 = 120

// CodexImportProviders 只提供导入所需的已校验管理用例。
type CodexImportProviders interface {
	CreateProvider(context.Context, *CreateProviderInput) (*Record, error)
	UpdateProvider(context.Context, int64, *UpdateProviderInput) (*Record, error)
}
type CodexImportOptions struct {
	Now                func() time.Time
	OAuthClientID      string
	ValidatePrivateKey func(string) error
	Invalidate         func(context.Context, *Record) error
}

// CodexImporter 逐项匹配、检查有效期并写入提供商，通过注入的接口解析平台密钥。
type CodexImporter struct {
	providers CodexImportProviders
	archive   *Archive
	options   CodexImportOptions
}

// CodexOrganizationClaim 保留导入 JWT 的原组织字段，仅供非认证提示解析。
type CodexOrganizationClaim struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Title     string `json:"title"`
	IsDefault bool   `json:"is_default"`
}

type CodexSessionImportRequest struct {
	Content            string         `json:"content"`
	Contents           []string       `json:"contents"`
	Name               string         `json:"name"`
	Notes              *string        `json:"notes"`
	GroupIDs           []int64        `json:"group_ids"`
	ProxyID            *int64         `json:"proxy_id"`
	Concurrency        *int           `json:"concurrency"`
	Priority           *int           `json:"priority"`
	RateMultiplier     *float64       `json:"rate_multiplier"`
	LoadFactor         *int           `json:"load_factor"`
	ExpiresAt          *int64         `json:"expires_at"`
	AutoPauseOnExpired *bool          `json:"auto_pause_on_expired"`
	CredentialExtras   map[string]any `json:"credential_extras"`
	Extra              map[string]any `json:"extra"`
	UpdateExisting     *bool          `json:"update_existing"`
}

type CodexSessionImportResult struct {
	Total    int                         `json:"total"`
	Created  int                         `json:"created"`
	Updated  int                         `json:"updated"`
	Skipped  int                         `json:"skipped"`
	Failed   int                         `json:"failed"`
	Items    []CodexSessionImportItem    `json:"items,omitempty"`
	Warnings []CodexSessionImportMessage `json:"warnings,omitempty"`
	Errors   []CodexSessionImportMessage `json:"errors,omitempty"`
}

type CodexSessionImportItem struct {
	Index      int    `json:"index"`
	Name       string `json:"name,omitempty"`
	Action     string `json:"action"`
	ProviderID int64  `json:"provider_id,omitempty"`
	Message    string `json:"message,omitempty"`
}

type CodexSessionImportMessage struct {
	Index   int    `json:"index"`
	Name    string `json:"name,omitempty"`
	Message string `json:"message"`
}

type CodexImportEntry struct {
	Index int
	Value any
}

type CodexImportProvider struct {
	Name            string
	AccessToken     string `json:"-"`
	RefreshToken    string `json:"-"`
	IDToken         string `json:"-"`
	Email           string
	ProviderID      string
	UserID          string
	PlanType        string
	Organization    string
	AgentRuntimeID  string
	AgentPrivateKey string `json:"-"`
	AgentTaskID     string
	AgentFedRAMP    bool
	IsAgentIdentity bool
	Credentials     map[string]any `json:"-"`
	Extra           map[string]any
	TokenExpiresAt  *time.Time
	IdentityKeys    []string
	WarningTexts    []string
}

type CodexJWTClaims struct {
	Sub        string                `json:"sub"`
	Email      string                `json:"email"`
	Exp        int64                 `json:"exp"`
	Iat        int64                 `json:"iat"`
	OpenAIAuth *CodexJWTOpenAIClaims `json:"https://api.openai.com/auth,omitempty"`
}

type CodexJWTOpenAIClaims struct {
	ChatGPTAccountID string                   `json:"chatgpt_account_id"`
	ChatGPTUserID    string                   `json:"chatgpt_user_id"`
	ChatGPTPlanType  string                   `json:"chatgpt_plan_type"`
	UserID           string                   `json:"user_id"`
	POID             string                   `json:"poid"`
	Organizations    []CodexOrganizationClaim `json:"organizations"`
}

func NewCodexImporter(providers CodexImportProviders, archive *Archive, options CodexImportOptions) *CodexImporter {
	if options.Now == nil {
		options.Now = time.Now
	}
	return &CodexImporter{providers, archive, options}
}
