package httpapi

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/TokenFlux/TokenRouter/internal/gateway"
	gatewaydto "github.com/TokenFlux/TokenRouter/internal/gateway/httpapi/dto"
	"github.com/TokenFlux/TokenRouter/internal/server/httpx"
)

// GatewaySettingsEndpoints 声明设置的 HTTP 操作，对应模块实现设置规则。
type GatewaySettingsEndpoints interface {
	GetRectifierSettings(*gin.Context)
	UpdateRectifierSettings(*gin.Context)
	GetBetaPolicySettings(*gin.Context)
	UpdateBetaPolicySettings(*gin.Context)
}

// RuntimeSettingsHandler 处理网关策略设置的查询和更新。
type RuntimeSettingsHandler struct{ settingService *gateway.RuntimeSettings }

// UpdateRectifierSettingsRequest 保留旧请求字段。
type UpdateRectifierSettingsRequest struct {
	Enabled                  bool     `json:"enabled"`
	ThinkingSignatureEnabled bool     `json:"thinking_signature_enabled"`
	ThinkingBudgetEnabled    bool     `json:"thinking_budget_enabled"`
	APIKeySignatureEnabled   bool     `json:"apikey_signature_enabled"`
	APIKeySignaturePatterns  []string `json:"apikey_signature_patterns"`
}

// UpdateBetaPolicySettingsRequest 保留旧请求字段。
type UpdateBetaPolicySettingsRequest struct {
	Rules []gatewaydto.BetaPolicyRule `json:"rules"`
}

// RegisterGatewaySettingsRoutes 在已经鉴权和审计的设置组中注册网关设置接口。
func RegisterGatewaySettingsRoutes(adminSettings *gin.RouterGroup, endpoint GatewaySettingsEndpoints) {
	adminSettings.GET("/rectifier", endpoint.GetRectifierSettings)
	adminSettings.PUT("/rectifier", endpoint.UpdateRectifierSettings)
	adminSettings.GET("/beta-policy", endpoint.GetBetaPolicySettings)
	adminSettings.PUT("/beta-policy", endpoint.UpdateBetaPolicySettings)
}

// NewRuntimeSettingsHandler 注入生产链共用的规则实例。
func NewRuntimeSettingsHandler(settings *gateway.RuntimeSettings) *RuntimeSettingsHandler {
	return &RuntimeSettingsHandler{settingService: settings}
}

// GetRectifierSettings 返回请求修正策略设置。
func (h *RuntimeSettingsHandler) GetRectifierSettings(c *gin.Context) {
	settings, err := h.settingService.GetRectifierSettings(c.Request.Context())
	if err != nil {
		httpx.ErrorFrom(c, err)
		return
	}

	patterns := settings.APIKeySignaturePatterns
	if patterns == nil {
		patterns = []string{}
	}
	httpx.Success(c, gatewaydto.RectifierSettings{
		Enabled:                  settings.Enabled,
		ThinkingSignatureEnabled: settings.ThinkingSignatureEnabled,
		ThinkingBudgetEnabled:    settings.ThinkingBudgetEnabled,
		APIKeySignatureEnabled:   settings.APIKeySignatureEnabled,
		APIKeySignaturePatterns:  patterns,
	})
}

// UpdateRectifierSettings 更新请求修正策略设置。
func (h *RuntimeSettingsHandler) UpdateRectifierSettings(c *gin.Context) {
	var req UpdateRectifierSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	// 校验并清理自定义匹配关键词
	const maxPatterns = 50
	const maxPatternLen = 500
	if len(req.APIKeySignaturePatterns) > maxPatterns {
		httpx.BadRequest(c, "Too many signature patterns (max 50)")
		return
	}
	var cleanedPatterns []string
	for _, p := range req.APIKeySignaturePatterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if len(p) > maxPatternLen {
			httpx.BadRequest(c, "Signature pattern too long (max 500 characters)")
			return
		}
		cleanedPatterns = append(cleanedPatterns, p)
	}

	settings := &gateway.RectifierSettings{
		Enabled:                  req.Enabled,
		ThinkingSignatureEnabled: req.ThinkingSignatureEnabled,
		ThinkingBudgetEnabled:    req.ThinkingBudgetEnabled,
		APIKeySignatureEnabled:   req.APIKeySignatureEnabled,
		APIKeySignaturePatterns:  cleanedPatterns,
	}

	if err := h.settingService.SetRectifierSettings(c.Request.Context(), settings); err != nil {
		httpx.BadRequest(c, err.Error())
		return
	}

	// 重新获取设置返回
	updatedSettings, err := h.settingService.GetRectifierSettings(c.Request.Context())
	if err != nil {
		httpx.ErrorFrom(c, err)
		return
	}

	updatedPatterns := updatedSettings.APIKeySignaturePatterns
	if updatedPatterns == nil {
		updatedPatterns = []string{}
	}
	httpx.Success(c, gatewaydto.RectifierSettings{
		Enabled:                  updatedSettings.Enabled,
		ThinkingSignatureEnabled: updatedSettings.ThinkingSignatureEnabled,
		ThinkingBudgetEnabled:    updatedSettings.ThinkingBudgetEnabled,
		APIKeySignatureEnabled:   updatedSettings.APIKeySignatureEnabled,
		APIKeySignaturePatterns:  updatedPatterns,
	})
}

// GetBetaPolicySettings 返回 beta 策略设置。
func (h *RuntimeSettingsHandler) GetBetaPolicySettings(c *gin.Context) {
	settings, err := h.settingService.GetBetaPolicySettings(c.Request.Context())
	if err != nil {
		httpx.ErrorFrom(c, err)
		return
	}

	rules := make([]gatewaydto.BetaPolicyRule, len(settings.Rules))
	for i, r := range settings.Rules {
		rules[i] = gatewaydto.BetaPolicyRule(r)
	}
	httpx.Success(c, gatewaydto.BetaPolicySettings{Rules: rules})
}

// UpdateBetaPolicySettings 更新 beta 策略设置。
func (h *RuntimeSettingsHandler) UpdateBetaPolicySettings(c *gin.Context) {
	var req UpdateBetaPolicySettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	rules := make([]gateway.BetaPolicyRule, len(req.Rules))
	for i, r := range req.Rules {
		rules[i] = gateway.BetaPolicyRule(r)
	}

	settings := &gateway.BetaPolicySettings{Rules: rules}
	if err := h.settingService.SetBetaPolicySettings(c.Request.Context(), settings); err != nil {
		httpx.BadRequest(c, err.Error())
		return
	}

	// Re-fetch to return updated settings
	updated, err := h.settingService.GetBetaPolicySettings(c.Request.Context())
	if err != nil {
		httpx.ErrorFrom(c, err)
		return
	}

	outRules := make([]gatewaydto.BetaPolicyRule, len(updated.Rules))
	for i, r := range updated.Rules {
		outRules[i] = gatewaydto.BetaPolicyRule(r)
	}
	httpx.Success(c, gatewaydto.BetaPolicySettings{Rules: outRules})
}
