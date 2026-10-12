package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/TokenFlux/TokenRouter/internal/identity/contact"
	"github.com/TokenFlux/TokenRouter/internal/settings"
)

const (
	SettingKeyProviderQuotaNotifyEnabled = "provider_quota_notify_enabled"
	SettingKeyProviderQuotaNotifyEmails  = "provider_quota_notify_emails"
)

// AdminSettings 明确区分提供商健康阈值和通知目的地。
type AdminSettings struct {
	ProviderQuotaNotifyEnabled   bool            `json:"provider_quota_notify_enabled"`
	ProviderQuotaNotifyEmails    []contact.Entry `json:"provider_quota_notify_emails"`
	ProviderSchedulingThresholds map[string]int  `json:"provider_scheduling_thresholds"`
}

// AdminReadSettings 保存综合管理页展示的提供商设置。
type AdminReadSettings struct {
	ProviderQuotaNotifyEmails    []contact.Entry
	ProviderQuotaNotifyEnabled   bool
	ProviderSchedulingThresholds map[string]int
}

// PrepareAdminSettings 准备管理设置的写入值，邮箱按设置格式序列化，nil 阈值按缺省值处理。
func PrepareAdminSettings(value AdminSettings) (map[string]string, error) {
	values := map[string]string{SettingKeyProviderQuotaNotifyEnabled: strconv.FormatBool(value.ProviderQuotaNotifyEnabled), SettingKeyProviderQuotaNotifyEmails: contact.MarshalNotifyEmails(value.ProviderQuotaNotifyEmails)}
	if value.ProviderSchedulingThresholds != nil {
		normalized, err := ValidateAndNormalizeProviderSchedulingThresholds(value.ProviderSchedulingThresholds)
		if err != nil {
			return nil, err
		}
		raw, err := json.Marshal(normalized)
		if err != nil {
			return nil, fmt.Errorf("marshal provider scheduling thresholds: %w", err)
		}
		values[SettingKeyProviderSchedulingThresholds] = string(raw)
	}
	return values, nil
}

// ReadAdminSettings 从同一批持久化数据解析展示值，并处理字段缺省值。
func ReadAdminSettings(settings map[string]string) *AdminReadSettings {
	result := &AdminReadSettings{}

	result.ProviderQuotaNotifyEnabled = settings[SettingKeyProviderQuotaNotifyEnabled] == "true"
	if raw := strings.TrimSpace(settings[SettingKeyProviderQuotaNotifyEmails]); raw != "" {
		result.ProviderQuotaNotifyEmails = contact.ParseNotifyEmails(raw)
	}
	if result.ProviderQuotaNotifyEmails == nil {
		result.ProviderQuotaNotifyEmails = []contact.Entry{}
	}
	result.ProviderSchedulingThresholds = DefaultProviderSchedulingThresholds()
	if raw := strings.TrimSpace(settings[SettingKeyProviderSchedulingThresholds]); raw != "" {
		if thresholds, err := ParseProviderSchedulingThresholdsSetting(raw); err != nil {
			slog.Warn("[Setting] parseSettings: unmarshal provider_scheduling_thresholds failed", "error", err)
		} else {
			result.ProviderSchedulingThresholds = thresholds
		}
	}
	return result
}

// SettingsParticipant 准备提供商设置键的写入数据。
func SettingsParticipant() settings.Participant {
	keys := []string{SettingKeyProviderQuotaNotifyEnabled, SettingKeyProviderQuotaNotifyEmails, SettingKeyProviderSchedulingThresholds}
	return settings.Participant{Module: "provider", Fields: keys, Keys: keys, Prepare: func(_ context.Context, input settings.Fields, _ map[string]string) (settings.PreparedChange, error) {
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
		values, err := PrepareAdminSettings(value)
		if err != nil {
			return settings.PreparedChange{}, err
		}
		for key := range values {
			if _, ok := input[key]; !ok {
				delete(values, key)
			}
		}
		return settings.PreparedChange{Values: values}, nil
	}}
}
