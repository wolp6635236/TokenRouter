package httpapi

import (
	"context"

	"github.com/gin-gonic/gin"

	"github.com/TokenFlux/TokenRouter/internal/provider"
	response "github.com/TokenFlux/TokenRouter/internal/server/httpx"
)

// QoderAuthorizationUseCase 提供 HTTP 层需要的 Qoder 授权操作。
type QoderAuthorizationUseCase interface {
	ValidateSite(string) (string, error)
	GenerateAuthURLForSite(context.Context, string, *int64) (*provider.QoderAuthURLResult, error)
	ExchangeCode(context.Context, *provider.QoderExchangeCodeInput) (*provider.QoderTokenInfo, error)
	Poll(context.Context, string, string, *int64) (*provider.QoderPollResult, error)
}
type QoderOAuthHandler struct {
	qoderOAuthService QoderAuthorizationUseCase
}

type QoderGenerateAuthURLRequest struct {
	ProxyID *int64 `json:"proxy_id"`
	Site    string `json:"site"`
}

type QoderExchangeCodeRequest struct {
	SessionID   string `json:"session_id" binding:"required"`
	State       string `json:"state"`
	Code        string `json:"code"`
	CallbackURL string `json:"callback_url"`
	ProxyID     *int64 `json:"proxy_id"`
}

type QoderPollRequest struct {
	SessionID string `json:"session_id" binding:"required"`
	State     string `json:"state" binding:"required"`
	ProxyID   *int64 `json:"proxy_id"`
}

func NewQoderOAuthHandler(qoderOAuthService QoderAuthorizationUseCase) *QoderOAuthHandler {
	return &QoderOAuthHandler{qoderOAuthService: qoderOAuthService}
}

// GenerateAuthURL 生成 Qoder 浏览器授权 URL。
// 路由：POST /api/v1/admin/qoder/oauth/auth-url。
func (h *QoderOAuthHandler) GenerateAuthURL(c *gin.Context) {
	var req QoderGenerateAuthURLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求无效: "+err.Error())
		return
	}

	site, err := h.qoderOAuthService.ValidateSite(req.Site)
	if err != nil {
		response.BadRequest(c, "请求无效: "+err.Error())
		return
	}
	result, err := h.qoderOAuthService.GenerateAuthURLForSite(c.Request.Context(), site, req.ProxyID)
	if err != nil {
		response.InternalError(c, "生成授权链接失败: "+err.Error())
		return
	}

	response.Success(c, result)
}

// ExchangeCode 完成 Qoder 设备授权并返回提供商凭据。
// 路由：POST /api/v1/admin/qoder/oauth/exchange-code。
func (h *QoderOAuthHandler) ExchangeCode(c *gin.Context) {
	var req QoderExchangeCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求无效: "+err.Error())
		return
	}

	tokenInfo, err := h.qoderOAuthService.ExchangeCode(c.Request.Context(), &provider.QoderExchangeCodeInput{
		SessionID:   req.SessionID,
		State:       req.State,
		Code:        req.Code,
		CallbackURL: req.CallbackURL,
		ProxyID:     req.ProxyID,
	})
	if err != nil {
		response.BadRequest(c, "Token 交换失败: "+err.Error())
		return
	}

	response.Success(c, tokenInfo)
}

// Poll 检查 Qoder 浏览器授权是否已完成。
// 路由：POST /api/v1/admin/qoder/oauth/poll。
func (h *QoderOAuthHandler) Poll(c *gin.Context) {
	var req QoderPollRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求无效: "+err.Error())
		return
	}

	result, err := h.qoderOAuthService.Poll(c.Request.Context(), req.SessionID, req.State, req.ProxyID)
	if err != nil {
		response.BadRequest(c, "授权状态检查失败: "+err.Error())
		return
	}

	response.Success(c, result)
}
