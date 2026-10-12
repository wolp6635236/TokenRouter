package audit

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/TokenFlux/TokenRouter/internal/settings"
)

// DefaultRetentionDays 是审计日志的默认保留天数。
const (
	DefaultRetentionDays            = 180
	SettingKeyAuditLogRetentionDays = "audit_log_retention_days"
)

// RetentionSettingsStore 读取审计日志保留期设置。
type RetentionSettingsStore interface {
	GetValue(context.Context, string) (string, error)
}

// RetentionSettings 解析审计日志保留天数。
type RetentionSettings struct{ settingRepo RetentionSettingsStore }

// AdminReadSettings 包含审计日志保留天数。
type AdminReadSettings struct{ AuditLogRetentionDays int }

// NewRetentionSettings 创建保留期设置读取器。
func NewRetentionSettings(repo RetentionSettingsStore) *RetentionSettings {
	return &RetentionSettings{settingRepo: repo}
}

// GetAuditLogRetentionDays 读取审计保留天数，零表示永久保留。
func (s *RetentionSettings) GetAuditLogRetentionDays(ctx context.Context) int {
	value, err := s.settingRepo.GetValue(ctx, SettingKeyAuditLogRetentionDays)
	if err != nil {
		return DefaultRetentionDays
	}
	return ParseRetentionDays(value)
}

// ParseRetentionDays 解析保留天数，空值或格式错误使用默认值，负数视为永久保留。
func ParseRetentionDays(value string) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return DefaultRetentionDays
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return DefaultRetentionDays
	}
	if n < 0 {
		return 0
	}
	return n
}

// ReadAdminSettings 从传入的设置值解析审计日志保留天数。
func ReadAdminSettings(settings map[string]string) *AdminReadSettings {
	result := &AdminReadSettings{}
	result.AuditLogRetentionDays = ParseRetentionDays(settings[SettingKeyAuditLogRetentionDays])

	return result
}

// PrepareRetentionDays 将保留天数转换为十进制整数字符串。
func PrepareRetentionDays(days int) string { return strconv.Itoa(days) }

// SettingsParticipant 准备审计保留期设置。
func SettingsParticipant() settings.Participant {
	return settings.Participant{Module: "audit", Fields: []string{"audit_log_retention_days"}, Keys: []string{SettingKeyAuditLogRetentionDays}, Prepare: func(_ context.Context, input settings.Fields, _ map[string]string) (settings.PreparedChange, error) {
		raw, ok := input["audit_log_retention_days"]
		if !ok {
			return settings.PreparedChange{}, nil
		}
		var days int
		if err := json.Unmarshal(raw, &days); err != nil {
			return settings.PreparedChange{}, err
		}
		return settings.PreparedChange{Values: map[string]string{SettingKeyAuditLogRetentionDays: PrepareRetentionDays(days)}}, nil
	}}
}
