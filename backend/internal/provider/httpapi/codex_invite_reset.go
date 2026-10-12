package httpapi

import (
	"context"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/TokenFlux/TokenRouter/internal/provider"
	response "github.com/TokenFlux/TokenRouter/internal/server/httpx"
)

// CodexInviteResetCommands 是管理 HTTP 对提供商用例的窄操作接口。
type CodexInviteResetCommands interface {
	GetStatus(context.Context, int64) (*provider.CodexInviteResetStatus, error)
	SendInvite(context.Context, int64, []string) (*provider.CodexInviteResetInviteResult, error)
	Consume(context.Context, int64, string) (*provider.CodexInviteResetConsumeResult, error)
}

// CodexInviteResetHandler 处理 Codex 邀请重置管理接口。
type CodexInviteResetHandler struct {
	service CodexInviteResetCommands
}

type codexInviteResetInviteRequest struct {
	Emails []string `json:"emails" binding:"required"`
}

type codexInviteResetConsumeRequest struct {
	// CreditID 可选；没有 credit 明细时由上游自动选择可用的重置机会。
	CreditID string `json:"credit_id"`
}

// NewCodexInviteResetHandler 创建 Codex 邀请重置管理处理器。
func NewCodexInviteResetHandler(service CodexInviteResetCommands) *CodexInviteResetHandler {
	return &CodexInviteResetHandler{service: service}
}

// GetStatus 查询当前提供商的邀请资格和可用重置次数。
func (h *CodexInviteResetHandler) GetStatus(c *gin.Context) {
	providerID, ok := parseCodexInviteResetProviderID(c)
	if !ok {
		return
	}
	result, err := h.service.GetStatus(c.Request.Context(), providerID)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, result)
}

// SendInvite 发送 Codex 邀请邮件。
func (h *CodexInviteResetHandler) SendInvite(c *gin.Context) {
	providerID, ok := parseCodexInviteResetProviderID(c)
	if !ok {
		return
	}
	var req codexInviteResetInviteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	result, err := h.service.SendInvite(c.Request.Context(), providerID, req.Emails)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, result)
}

// Consume 使用一次 Codex 重置机会。
func (h *CodexInviteResetHandler) Consume(c *gin.Context) {
	providerID, ok := parseCodexInviteResetProviderID(c)
	if !ok {
		return
	}
	var req codexInviteResetConsumeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	result, err := h.service.Consume(c.Request.Context(), providerID, req.CreditID)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, result)
}

func parseCodexInviteResetProviderID(c *gin.Context) (int64, bool) {
	providerID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid provider ID")
		return 0, false
	}
	return providerID, true
}
