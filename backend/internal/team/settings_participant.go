package team

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/TokenFlux/TokenRouter/internal/settings"
)

// SettingKeyTeamEnabled 是团队功能的设置键。
const (
	SettingKeyTeamEnabled = "team_enabled"
)

// AdminSettings 保存团队功能的启用设置。
type AdminSettings struct {
	TeamEnabled bool `json:"team_enabled"`
}

// PrepareAdminSettings 将团队启用状态编码为设置键值。
func PrepareAdminSettings(value AdminSettings) (AdminSettings, map[string]string, error) {
	values := map[string]string{}
	values[SettingKeyTeamEnabled] = strconv.FormatBool(value.TeamEnabled)

	return value, values, nil
}

// SettingsParticipant 声明团队开关的存储键，并准备请求中提供的设置。
func SettingsParticipant() settings.Participant {
	fields := []string{"team_enabled"}
	keys := []string{SettingKeyTeamEnabled}
	return settings.Participant{Module: "team", Fields: fields, Keys: keys, Prepare: func(_ context.Context, input settings.Fields, _ map[string]string) (settings.PreparedChange, error) {
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
		_, values, err := PrepareAdminSettings(value)
		if err != nil {
			return settings.PreparedChange{}, err
		}
		for i, field := range fields {
			if _, ok := input[field]; !ok {
				delete(values, keys[i])
			}
		}
		return settings.PreparedChange{Values: values}, nil
	}}
}

// PublicSettings 返回配置与进程开关共同决定的团队启用状态，以及自助创建开关。
func PublicSettings(values map[string]string, enabled, selfService bool) (bool, bool) {
	return values[SettingKeyTeamEnabled] != "false" && enabled, selfService
}
