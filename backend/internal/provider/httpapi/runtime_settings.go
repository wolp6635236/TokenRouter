package httpapi

import (
	"github.com/TokenFlux/TokenRouter/internal/provider"
	providerdto "github.com/TokenFlux/TokenRouter/internal/provider/httpapi/dto"
	"github.com/TokenFlux/TokenRouter/internal/server/httpx"
	"github.com/gin-gonic/gin"
)

// RuntimeSettingsHandler 通过提供商设置用例处理管理请求。
type RuntimeSettingsHandler struct{ settingService *provider.RuntimeSettings }

// NewRuntimeSettingsHandler 注入唯一的提供商设置及其缓存。
func NewRuntimeSettingsHandler(service *provider.RuntimeSettings) *RuntimeSettingsHandler {
	return &RuntimeSettingsHandler{settingService: service}
}

// GetOverloadCooldownSettings 返回过载冷却设置。
func (h *RuntimeSettingsHandler) GetOverloadCooldownSettings(c *gin.Context) {
	settings, err := h.settingService.GetOverloadCooldownSettings(c.Request.Context())
	if err != nil {
		httpx.ErrorFrom(c, err)
		return
	}

	httpx.Success(c, providerdto.OverloadCooldownSettings{
		Enabled:         settings.Enabled,
		CooldownMinutes: settings.CooldownMinutes,
	})
}

// UpdateOverloadCooldownSettings 校验并保存过载冷却设置。
func (h *RuntimeSettingsHandler) UpdateOverloadCooldownSettings(c *gin.Context) {
	var req UpdateOverloadCooldownSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	settings := &provider.OverloadCooldownSettings{
		Enabled:         req.Enabled,
		CooldownMinutes: req.CooldownMinutes,
	}

	if err := h.settingService.SetOverloadCooldownSettings(c.Request.Context(), settings); err != nil {
		httpx.BadRequest(c, err.Error())
		return
	}

	updatedSettings, err := h.settingService.GetOverloadCooldownSettings(c.Request.Context())
	if err != nil {
		httpx.ErrorFrom(c, err)
		return
	}

	httpx.Success(c, providerdto.OverloadCooldownSettings{
		Enabled:         updatedSettings.Enabled,
		CooldownMinutes: updatedSettings.CooldownMinutes,
	})
}

// UpdateOverloadCooldownSettingsRequest 接收过载冷却设置。
type UpdateOverloadCooldownSettingsRequest struct {
	Enabled         bool `json:"enabled"`
	CooldownMinutes int  `json:"cooldown_minutes"`
}

// GetRateLimit429CooldownSettings 返回 429 冷却设置。
func (h *RuntimeSettingsHandler) GetRateLimit429CooldownSettings(c *gin.Context) {
	settings, err := h.settingService.GetRateLimit429CooldownSettings(c.Request.Context())
	if err != nil {
		httpx.ErrorFrom(c, err)
		return
	}

	httpx.Success(c, providerdto.RateLimit429CooldownSettings{
		Enabled:         settings.Enabled,
		CooldownSeconds: settings.CooldownSeconds,
	})
}

// UpdateRateLimit429CooldownSettings 校验并保存 429 冷却设置。
func (h *RuntimeSettingsHandler) UpdateRateLimit429CooldownSettings(c *gin.Context) {
	var req UpdateRateLimit429CooldownSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	settings := &provider.RateLimit429CooldownSettings{
		Enabled:         req.Enabled,
		CooldownSeconds: req.CooldownSeconds,
	}

	if err := h.settingService.SetRateLimit429CooldownSettings(c.Request.Context(), settings); err != nil {
		httpx.BadRequest(c, err.Error())
		return
	}

	updatedSettings, err := h.settingService.GetRateLimit429CooldownSettings(c.Request.Context())
	if err != nil {
		httpx.ErrorFrom(c, err)
		return
	}

	httpx.Success(c, providerdto.RateLimit429CooldownSettings{
		Enabled:         updatedSettings.Enabled,
		CooldownSeconds: updatedSettings.CooldownSeconds,
	})
}

// UpdateRateLimit429CooldownSettingsRequest 接收429 冷却设置。
type UpdateRateLimit429CooldownSettingsRequest struct {
	Enabled         bool `json:"enabled"`
	CooldownSeconds int  `json:"cooldown_seconds"`
}

// GetOpenAIImagesOAuthUnavailableCooldownSettings 返回 OAuth 图片不可用时的冷却设置。
func (h *RuntimeSettingsHandler) GetOpenAIImagesOAuthUnavailableCooldownSettings(c *gin.Context) {
	settings, err := h.settingService.GetOpenAIImagesOAuthUnavailableCooldownSettings(c.Request.Context())
	if err != nil {
		httpx.ErrorFrom(c, err)
		return
	}
	httpx.Success(c, providerdto.OpenAIImagesOAuthUnavailableCooldownSettings{CooldownMinutes: settings.CooldownMinutes})
}

// UpdateOpenAIImagesOAuthUnavailableCooldownSettings 校验并保存 OAuth 图片不可用时的冷却设置。
func (h *RuntimeSettingsHandler) UpdateOpenAIImagesOAuthUnavailableCooldownSettings(c *gin.Context) {
	var req UpdateOpenAIImagesOAuthUnavailableCooldownSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	settings := &provider.OpenAIImagesOAuthUnavailableCooldownSettings{CooldownMinutes: req.CooldownMinutes}
	if err := h.settingService.SetOpenAIImagesOAuthUnavailableCooldownSettings(c.Request.Context(), settings); err != nil {
		httpx.BadRequest(c, err.Error())
		return
	}
	httpx.Success(c, providerdto.OpenAIImagesOAuthUnavailableCooldownSettings{CooldownMinutes: settings.CooldownMinutes})
}

// UpdateOpenAIImagesOAuthUnavailableCooldownSettingsRequest 接收OAuth 图片不可用冷却设置。
type UpdateOpenAIImagesOAuthUnavailableCooldownSettingsRequest struct {
	CooldownMinutes int `json:"cooldown_minutes"`
}

// GetStreamTimeoutSettings 返回流式请求超时设置。
func (h *RuntimeSettingsHandler) GetStreamTimeoutSettings(c *gin.Context) {
	settings, err := h.settingService.GetStreamTimeoutSettings(c.Request.Context())
	if err != nil {
		httpx.ErrorFrom(c, err)
		return
	}

	httpx.Success(c, providerdto.StreamTimeoutSettings{
		Enabled:                settings.Enabled,
		Action:                 settings.Action,
		TempUnschedMinutes:     settings.TempUnschedMinutes,
		ThresholdCount:         settings.ThresholdCount,
		ThresholdWindowMinutes: settings.ThresholdWindowMinutes,
	})
}

// UpdateStreamTimeoutSettings 校验并保存流式请求超时设置。
func (h *RuntimeSettingsHandler) UpdateStreamTimeoutSettings(c *gin.Context) {
	var req UpdateStreamTimeoutSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	settings := &provider.StreamTimeoutSettings{
		Enabled:                req.Enabled,
		Action:                 req.Action,
		TempUnschedMinutes:     req.TempUnschedMinutes,
		ThresholdCount:         req.ThresholdCount,
		ThresholdWindowMinutes: req.ThresholdWindowMinutes,
	}

	if err := h.settingService.SetStreamTimeoutSettings(c.Request.Context(), settings); err != nil {
		httpx.BadRequest(c, err.Error())
		return
	}

	// 重新获取设置返回
	updatedSettings, err := h.settingService.GetStreamTimeoutSettings(c.Request.Context())
	if err != nil {
		httpx.ErrorFrom(c, err)
		return
	}

	httpx.Success(c, providerdto.StreamTimeoutSettings{
		Enabled:                updatedSettings.Enabled,
		Action:                 updatedSettings.Action,
		TempUnschedMinutes:     updatedSettings.TempUnschedMinutes,
		ThresholdCount:         updatedSettings.ThresholdCount,
		ThresholdWindowMinutes: updatedSettings.ThresholdWindowMinutes,
	})
}

// UpdateStreamTimeoutSettingsRequest 接收流式请求超时设置。
type UpdateStreamTimeoutSettingsRequest struct {
	Enabled                bool   `json:"enabled"`
	Action                 string `json:"action"`
	TempUnschedMinutes     int    `json:"temp_unsched_minutes"`
	ThresholdCount         int    `json:"threshold_count"`
	ThresholdWindowMinutes int    `json:"threshold_window_minutes"`
}

// GetOpenAI403CooldownSettings 返回 OpenAI 403 冷却设置。
func (h *RuntimeSettingsHandler) GetOpenAI403CooldownSettings(c *gin.Context) {
	settings, err := h.settingService.GetOpenAI403CooldownSettings(c.Request.Context())
	if err != nil {
		httpx.ErrorFrom(c, err)
		return
	}

	httpx.Success(c, providerdto.OpenAI403CooldownSettings{
		Enabled:                 settings.Enabled,
		CooldownMinutes:         settings.CooldownMinutes,
		ErrorOnThresholdEnabled: settings.ErrorOnThresholdEnabled,
		ThresholdCount:          settings.ThresholdCount,
		ThresholdWindowMinutes:  settings.ThresholdWindowMinutes,
	})
}

// UpdateOpenAI403CooldownSettings 校验并保存 OpenAI 403 冷却设置。
func (h *RuntimeSettingsHandler) UpdateOpenAI403CooldownSettings(c *gin.Context) {
	var req UpdateOpenAI403CooldownSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	defaults := provider.DefaultOpenAI403CooldownSettings()
	errorOnThresholdEnabled := defaults.ErrorOnThresholdEnabled
	if req.ErrorOnThresholdEnabled != nil {
		errorOnThresholdEnabled = *req.ErrorOnThresholdEnabled
	}
	thresholdCount := defaults.ThresholdCount
	if req.ThresholdCount != nil {
		thresholdCount = *req.ThresholdCount
	}
	thresholdWindowMinutes := defaults.ThresholdWindowMinutes
	if req.ThresholdWindowMinutes != nil {
		thresholdWindowMinutes = *req.ThresholdWindowMinutes
	}

	settings := &provider.OpenAI403CooldownSettings{
		Enabled:                 req.Enabled,
		CooldownMinutes:         req.CooldownMinutes,
		ErrorOnThresholdEnabled: errorOnThresholdEnabled,
		ThresholdCount:          thresholdCount,
		ThresholdWindowMinutes:  thresholdWindowMinutes,
	}

	if err := h.settingService.SetOpenAI403CooldownSettings(c.Request.Context(), settings); err != nil {
		httpx.BadRequest(c, err.Error())
		return
	}

	updatedSettings, err := h.settingService.GetOpenAI403CooldownSettings(c.Request.Context())
	if err != nil {
		httpx.ErrorFrom(c, err)
		return
	}

	httpx.Success(c, providerdto.OpenAI403CooldownSettings{
		Enabled:                 updatedSettings.Enabled,
		CooldownMinutes:         updatedSettings.CooldownMinutes,
		ErrorOnThresholdEnabled: updatedSettings.ErrorOnThresholdEnabled,
		ThresholdCount:          updatedSettings.ThresholdCount,
		ThresholdWindowMinutes:  updatedSettings.ThresholdWindowMinutes,
	})
}

// UpdateOpenAI403CooldownSettingsRequest 接收OpenAI 403 冷却设置。
type UpdateOpenAI403CooldownSettingsRequest struct {
	Enabled                 bool  `json:"enabled"`
	CooldownMinutes         int   `json:"cooldown_minutes"`
	ErrorOnThresholdEnabled *bool `json:"error_on_threshold_enabled"`
	ThresholdCount          *int  `json:"threshold_count"`
	ThresholdWindowMinutes  *int  `json:"threshold_window_minutes"`
}
