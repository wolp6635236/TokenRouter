package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	wire "github.com/TokenFlux/TokenRouter/internal/protocol/openai"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/provider/httpapi/dto"
	response "github.com/TokenFlux/TokenRouter/internal/server/httpx"
)

const (
	OpenAIQuotaResetWarningCacheRefreshFailed     = providercore.OpenAIQuotaResetWarningCacheRefreshFailed
	OpenAIQuotaResetWarningProviderRecoveryFailed = providercore.OpenAIQuotaResetWarningProviderRecoveryFailed
)

// OpenAIOAuthHandler handles OpenAI OAuth-related operations.
type OpenAIOAuthHandler struct {
	QuotaActions  *providercore.OpenAIQuotaActions
	Import        *providercore.OpenAIProviderImport
	Options       OpenAIHTTPOptions
	Authorization *providercore.OpenAIAuthorization
	Admin         OpenAIAdminOperations
	Quota         OpenAIQuotaService
	Recovery      OpenAIProviderStateRecoverer
}

type OpenAIQuotaService interface {
	QueryUsage(ctx context.Context, providerID int64) (*wire.OpenAIQuotaUsage, error)
	CacheResetCreditsSnapshot(ctx context.Context, providerID int64, credits *wire.OpenAIRateLimitResetCredits) error
	CachePostResetSnapshot(ctx context.Context, providerID int64, usage *wire.OpenAIQuotaUsage) error
	ResetCredit(ctx context.Context, providerID int64) (*wire.OpenAIQuotaResetResult, error)
}

type OpenAIProviderStateRecoverer interface {
	RecoverProviderState(ctx context.Context, providerID int64, options providercore.ProviderRecoveryOptions) (*providercore.SuccessfulTestRecovery, error)
}

type OpenAIQuotaResetResponse struct {
	wire.OpenAIQuotaResetResult
	Quota                  *wire.OpenAIQuotaUsage `json:"quota,omitempty"`
	Provider               *dto.Provider          `json:"provider,omitempty"`
	CacheRefreshed         bool                   `json:"cache_refreshed"`
	ProviderStateRecovered bool                   `json:"provider_state_recovered"`
	WarningCode            string                 `json:"warning_code,omitempty"`
}

type OpenAIQuotaRefreshResponse struct {
	wire.OpenAIQuotaUsage
	CachePersisted bool `json:"cache_persisted"`
}

// OpenAIGenerateAuthURLRequest represents the request for generating OpenAI auth URL.
type OpenAIGenerateAuthURLRequest struct {
	ProxyID     *int64 `json:"proxy_id"`
	RedirectURI string `json:"redirect_uri"`
}

// OpenAIExchangeCodeRequest represents the request for exchanging OpenAI auth code.
type OpenAIExchangeCodeRequest struct {
	SessionID              string `json:"session_id" binding:"required"`
	Code                   string `json:"code" binding:"required"`
	State                  string `json:"state" binding:"required"`
	RedirectURI            string `json:"redirect_uri"`
	ProxyID                *int64 `json:"proxy_id"`
	TLSFingerprintRouterID *int64 `json:"tls_fingerprint_router_id"`
}

// OpenAIRefreshTokenRequest represents the request for refreshing OpenAI token.
type OpenAIRefreshTokenRequest struct {
	RefreshToken           string `json:"refresh_token"`
	RT                     string `json:"rt"`
	ClientID               string `json:"client_id"`
	ProxyID                *int64 `json:"proxy_id"`
	TLSFingerprintRouterID *int64 `json:"tls_fingerprint_router_id"`
}

type OpenAICodexPATCreateRequest struct {
	AccessToken        string         `json:"access_token" binding:"required"`
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
}

// CreateShadowRequest 是创建 Spark 影子提供商的请求体。
type CreateShadowRequest struct {
	Name        string  `json:"name"`
	Priority    int     `json:"priority"`
	Concurrency int     `json:"concurrency"`
	GroupIDs    []int64 `json:"group_ids"`
}

type OpenAIAdminOperations interface {
	GetProvider(context.Context, int64) (*providercore.Record, error)
	CreateProvider(context.Context, *providercore.CreateProviderInput) (*providercore.Record, error)
	UpdateProvider(context.Context, int64, *providercore.UpdateProviderInput) (*providercore.Record, error)
	CreateShadow(context.Context, int64, providercore.ShadowOptions) (*providercore.Record, error)
}
type OpenAIHTTPOptions struct {
	ProxyURL func(context.Context, int64) (string, bool, error)
	ClientID func(string) (string, bool)
}

func oauthPlatformFromPath(c *gin.Context) string {
	return "openai"
}

func NewOpenAIOAuthHandler(auth *providercore.OpenAIAuthorization, admin OpenAIAdminOperations, quota OpenAIQuotaService, recovery OpenAIProviderStateRecoverer, options OpenAIHTTPOptions) *OpenAIOAuthHandler {
	return &OpenAIOAuthHandler{
		Authorization: auth,
		Admin:         admin,
		Quota:         quota,
		Recovery:      recovery,
		Options:       options,
		Import:        providercore.NewOpenAIProviderImport(auth, admin, options.ProxyURL),
		QuotaActions:  providercore.NewOpenAIQuotaActions(quota, recovery, admin, slog.Warn),
	}
}

// GenerateAuthURL generates OpenAI OAuth authorization URL
// POST /api/v1/admin/openai/generate-auth-url
func (h *OpenAIOAuthHandler) GenerateAuthURL(c *gin.Context) {
	var req OpenAIGenerateAuthURLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// Allow empty body
		req = OpenAIGenerateAuthURLRequest{}
	}

	result, err := h.Authorization.GenerateAuthURL(
		c.Request.Context(),
		req.ProxyID,
		req.RedirectURI,
		oauthPlatformFromPath(c),
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, result)
}

// ExchangeCode exchanges OpenAI authorization code for tokens
// POST /api/v1/admin/openai/exchange-code
func (h *OpenAIOAuthHandler) ExchangeCode(c *gin.Context) {
	var req OpenAIExchangeCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	tokenInfo, err := h.Authorization.ExchangeCode(c.Request.Context(), &providercore.OpenAIExchangeCodeInput{
		SessionID: req.SessionID,

		Code: req.Code,

		State: req.State,

		RedirectURI: req.RedirectURI,

		ProxyID: req.ProxyID,

		TLSFingerprintRouterID: req.TLSFingerprintRouterID,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, tokenInfo)
}

// RefreshToken refreshes an OpenAI OAuth token
// POST /api/v1/admin/openai/refresh-token
func (h *OpenAIOAuthHandler) RefreshToken(c *gin.Context) {
	var req OpenAIRefreshTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	refreshToken := strings.TrimSpace(req.RefreshToken)
	if refreshToken == "" {
		refreshToken = strings.TrimSpace(req.RT)
	}
	if refreshToken == "" {
		response.BadRequest(c, "refresh_token is required")
		return
	}

	var proxyURL string
	if req.ProxyID != nil {
		proxyURLValue, found, err := h.Options.ProxyURL(c.Request.Context(), *req.ProxyID)
		if err == nil && found {
			proxyURL = proxyURLValue
		}
	}

	// 未传 client_id 时，按请求路径的平台填入默认值。
	clientID := strings.TrimSpace(req.ClientID)
	if clientID == "" {
		platform := oauthPlatformFromPath(c)
		clientID, _ = h.Options.ClientID(platform)
	}

	tokenInfo, err := h.Authorization.RefreshTokenWithClientIDAndRouter(c.Request.Context(), refreshToken, proxyURL, clientID, req.TLSFingerprintRouterID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, tokenInfo)
}

// RefreshProviderToken refreshes token for a specific OpenAI provider
// POST /api/v1/admin/openai/providers/:id/refresh
func (h *OpenAIOAuthHandler) RefreshProviderToken(c *gin.Context) {
	providerID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid provider ID")
		return
	}

	value, err := h.providerImport().RefreshProvider(c.Request.Context(), providerID, oauthPlatformFromPath(c))
	if err != nil {
		writeOpenAIProviderImportError(c, err)
		return
	}
	response.Success(c, dto.ProviderFromRecord(value))
}

// CreateProviderFromOAuth creates a new OpenAI OAuth provider from token info
// POST /api/v1/admin/openai/create-from-oauth
func (h *OpenAIOAuthHandler) CreateProviderFromOAuth(c *gin.Context) {
	var req struct {
		SessionID              string  `json:"session_id" binding:"required"`
		Code                   string  `json:"code" binding:"required"`
		State                  string  `json:"state" binding:"required"`
		RedirectURI            string  `json:"redirect_uri"`
		ProxyID                *int64  `json:"proxy_id"`
		TLSFingerprintRouterID *int64  `json:"tls_fingerprint_router_id"`
		Name                   string  `json:"name"`
		Concurrency            int     `json:"concurrency"`
		Priority               int     `json:"priority"`
		GroupIDs               []int64 `json:"group_ids"`
	}
	if err := response.BindJSONStrict(c, &req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	value, err := h.providerImport().CreateOAuthProvider(c.Request.Context(), providercore.OpenAIOAuthProviderCreateInput{
		SessionID: req.SessionID,

		Code: req.Code,

		State: req.State,

		RedirectURI: req.RedirectURI,

		ProxyID: req.ProxyID,

		TLSFingerprintRouterID: req.TLSFingerprintRouterID,

		Name: req.Name,

		Concurrency: req.Concurrency,

		Priority: req.Priority,

		GroupIDs: req.GroupIDs,
	}, oauthPlatformFromPath(c))
	if err != nil {
		writeOpenAIProviderImportError(c, err)
		return
	}
	response.Success(c, dto.ProviderFromRecord(value))
}

// CreateProviderFromCodexPAT 使用 Codex at-* Personal Access Token 创建 OpenAI OAuth 提供商。
// POST /api/v1/admin/openai/create-from-codex-pat
func (h *OpenAIOAuthHandler) CreateProviderFromCodexPAT(c *gin.Context) {
	var req OpenAICodexPATCreateRequest
	if err := response.BindJSONStrict(c, &req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	value, err := h.providerImport().CreatePATProvider(c.Request.Context(), providercore.OpenAICodexPATCreateInput(req))
	if err != nil {
		writeOpenAIProviderImportError(c, err)
		return
	}
	response.Success(c, dto.ProviderFromRecord(value))
}

// QueryQuota 查询 OpenAI OAuth 提供商的上游限流窗口和可用重置次数。
// GET /api/v1/admin/openai/providers/:id/quota
func (h *OpenAIOAuthHandler) QueryQuota(c *gin.Context) {
	providerID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid provider ID")
		return
	}
	if h.Quota == nil {
		response.BadRequest(c, "openai quota service is not enabled")
		return
	}
	usage, err := h.Quota.QueryUsage(c.Request.Context(), providerID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, usage)
}

// RefreshQuota 查询上游额度，并把带到期时间的重置次数快照持久化到提供商 extra。
// POST /api/v1/admin/openai/providers/:id/quota/refresh
func (h *OpenAIOAuthHandler) RefreshQuota(c *gin.Context) {
	providerID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid provider ID")
		return
	}
	if h.Quota == nil {
		response.BadRequest(c, "openai quota service is not enabled")
		return
	}

	value, err := h.quotaActions().Refresh(c.Request.Context(), providerID)
	if err != nil {
		if missing, ok := errors.AsType[*providercore.OpenAIQuotaOutcomeError](err); ok {
			response.Error(c, http.StatusInternalServerError, missing.Message)
		} else {
			response.ErrorFrom(c, err)
		}
		return
	}
	output := OpenAIQuotaRefreshResponse{OpenAIQuotaUsage: value.OpenAIQuotaUsage, CachePersisted: value.CachePersisted}
	response.Success(c, output)
}

// CreateShadow 为母 OpenAI OAuth 提供商创建 spark 维度影子提供商。
// POST /api/v1/admin/providers/:id/shadow
func (h *OpenAIOAuthHandler) CreateShadow(c *gin.Context) {
	parentID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid provider ID")
		return
	}

	var req CreateShadowRequest
	if err := response.BindJSONStrict(c, &req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	shadow, err := h.Admin.CreateShadow(c.Request.Context(), parentID, providercore.ShadowOptions{
		Name:        req.Name,
		Priority:    req.Priority,
		Concurrency: req.Concurrency,
		GroupIDs:    req.GroupIDs,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, dto.ProviderFromRecordShallow(shadow))
}

// ResetQuota 消耗一次 OpenAI OAuth 提供商的上游限流重置次数。
// POST /api/v1/admin/openai/providers/:id/reset-quota
func (h *OpenAIOAuthHandler) ResetQuota(c *gin.Context) {
	providerID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid provider ID")
		return
	}
	if h.Quota == nil {
		response.BadRequest(c, "openai quota service is not enabled")
		return
	}
	value, err := h.quotaActions().Reset(c.Request.Context(), providerID)
	if err != nil {
		if missing, ok := errors.AsType[*providercore.OpenAIQuotaOutcomeError](err); ok {
			response.Error(c, http.StatusInternalServerError, missing.Message)
		} else {
			response.ErrorFrom(c, err)
		}
		return
	}
	output := OpenAIQuotaResetResponse{
		OpenAIQuotaResetResult: value.OpenAIQuotaResetResult,
		Quota:                  value.Quota,
		Provider:               dto.ProviderFromRecord(value.Provider),
		CacheRefreshed:         value.CacheRefreshed,
		ProviderStateRecovered: value.ProviderStateRecovered,
		WarningCode:            value.WarningCode,
	}
	response.Success(c, output)
}

// providerImport 无缓存用例只复用 handler 已持有的唯一提供商/授权依赖。
func (h *OpenAIOAuthHandler) providerImport() *providercore.OpenAIProviderImport {
	if h.Import != nil {
		return h.Import
	}
	// 兼容原白盒测试直接构造的部分 handler；生产构造始终持有同一用例实例。
	return providercore.NewOpenAIProviderImport(h.Authorization, h.Admin, h.Options.ProxyURL)
}

func writeOpenAIProviderImportError(c *gin.Context, err error) {
	if inputError, ok := errors.AsType[*providercore.OpenAIProviderInputError](err); ok {
		response.BadRequest(c, inputError.Message)
		return
	}
	response.ErrorFrom(c, err)
}

func (h *OpenAIOAuthHandler) quotaActions() *providercore.OpenAIQuotaActions {
	if h.QuotaActions != nil {
		return h.QuotaActions
	}
	// 兼容原白盒测试的部分构造；生产始终复用构造时装配的用例。
	return providercore.NewOpenAIQuotaActions(h.Quota, h.Recovery, h.Admin, slog.Warn)
}
