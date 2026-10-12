package ws

import (
	"context"

	"github.com/TokenFlux/TokenRouter/internal/pkg/apperror"
	"github.com/TokenFlux/TokenRouter/internal/settings"
)

// SettingsParticipant 为综合设置登记字段和完整参数校验。
func SettingsParticipant(defaults Parameters) settings.Participant {
	return settings.Participant{Module: "responses-ws", Fields: []string{SettingKey}, Keys: []string{SettingKey}, Prepare: func(_ context.Context, fields settings.Fields, current map[string]string) (settings.PreparedChange, error) {
		raw, present := fields[SettingKey]
		if !present {
			return settings.PreparedChange{}, nil
		}
		merged, err := PatchParameters(defaults, current[SettingKey], raw)
		if err != nil {
			return settings.PreparedChange{}, apperror.BadRequest("INVALID_RESPONSES_WS_SETTINGS", err.Error())
		}
		return settings.PreparedChange{Values: map[string]string{SettingKey: merged}}, nil
	}}
}
