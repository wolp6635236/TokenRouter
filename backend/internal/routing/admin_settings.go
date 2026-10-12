package routing

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/TokenFlux/TokenRouter/internal/settings"
)

// 路由回退设置的存储键。
const (
	SettingKeyEnableModelFallback      = "enable_model_fallback"
	SettingKeyFallbackModelAnthropic   = "fallback_model_anthropic"
	SettingKeyFallbackModelAntigravity = "fallback_model_antigravity"
	SettingKeyFallbackModelGemini      = "fallback_model_gemini"
	SettingKeyFallbackModelOpenAI      = "fallback_model_openai"
)

// AdminSettings 包含路由回退和市场观测窗口配置。
type AdminSettings struct {
	EnableModelFallback                  bool   `json:"enable_model_fallback"`
	FallbackModelAnthropic               string `json:"fallback_model_anthropic"`
	FallbackModelAntigravity             string `json:"fallback_model_antigravity"`
	FallbackModelGemini                  string `json:"fallback_model_gemini"`
	FallbackModelOpenAI                  string `json:"fallback_model_openai"`
	MarketplaceAvailabilityBucketMinutes int    `json:"marketplace_availability_bucket_minutes"`
	MarketplaceAvailabilityWindowDays    int    `json:"marketplace_availability_window_days"`
}

// PrepareAdminSettings 校验市场观测窗口，并把路由设置转换成存储值。
func PrepareAdminSettings(settings *AdminSettings) map[string]string {
	updates := map[string]string{}
	settings.MarketplaceAvailabilityWindowDays, settings.MarketplaceAvailabilityBucketMinutes = NormalizeMarketplaceAvailabilityWindow(
		settings.MarketplaceAvailabilityWindowDays,
		settings.MarketplaceAvailabilityBucketMinutes,
	)
	updates[SettingKeyMarketplaceAvailabilityWindowDays] = strconv.Itoa(settings.MarketplaceAvailabilityWindowDays)
	updates[SettingKeyMarketplaceAvailabilityBucketMinutes] = strconv.Itoa(settings.MarketplaceAvailabilityBucketMinutes)
	updates[SettingKeyEnableModelFallback] = strconv.FormatBool(settings.EnableModelFallback)
	updates[SettingKeyFallbackModelAnthropic] = settings.FallbackModelAnthropic
	updates[SettingKeyFallbackModelOpenAI] = settings.FallbackModelOpenAI
	updates[SettingKeyFallbackModelGemini] = settings.FallbackModelGemini
	updates[SettingKeyFallbackModelAntigravity] = settings.FallbackModelAntigravity
	return updates
}

// SettingsParticipant 为路由设置注册字段，并准备传入字段的存储值。
func SettingsParticipant() settings.Participant {
	keys := []string{SettingKeyEnableModelFallback, SettingKeyFallbackModelAnthropic, SettingKeyFallbackModelAntigravity, SettingKeyFallbackModelGemini, SettingKeyFallbackModelOpenAI, SettingKeyMarketplaceAvailabilityBucketMinutes, SettingKeyMarketplaceAvailabilityWindowDays}
	return settings.Participant{Module: "routing", Fields: keys, Keys: keys, Prepare: func(_ context.Context, input settings.Fields, _ map[string]string) (settings.PreparedChange, error) {
		if len(input) == 0 {
			return settings.PreparedChange{}, nil
		}
		raw, err := json.Marshal(input)
		if err != nil {
			return settings.PreparedChange{}, err
		}
		var value AdminSettings
		if err = json.Unmarshal(raw, &value); err != nil {
			return settings.PreparedChange{}, err
		}
		values := PrepareAdminSettings(&value)
		for key := range values {
			if _, ok := input[key]; !ok {
				delete(values, key)
			}
		}
		return settings.PreparedChange{Values: values}, nil
	}}
}
