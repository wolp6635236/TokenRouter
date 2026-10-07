package settings

import (
	"context"
	"encoding/json"
	"net/url"
	"reflect"
	"strings"

	"github.com/TokenFlux/TokenRouter/internal/pkg/apperror"
	"github.com/TokenFlux/TokenRouter/internal/pkg/locale"
)

// LocalizedTexts 是综合设置中普通用户可见的独立文案。
type LocalizedTexts = locale.TextUpdates

// LocalizedTextFields 将管理字段映射到各业务原文的存储键。
var LocalizedTextFields = map[string]string{
	"balance_unit_name":               "balance_unit_name",
	"balance_low_notify_recharge_url": "balance_low_notify_recharge_url",
	"oidc_connect_provider_name":      "oidc_connect_provider_name",
	"payment_help_text":               "PAYMENT_HELP_TEXT",
	"payment_help_image_url":          "PAYMENT_HELP_IMAGE_URL",
	"payment_product_name_prefix":     "PRODUCT_NAME_PREFIX",
	"payment_product_name_suffix":     "PRODUCT_NAME_SUFFIX",
	"smtp_from_name":                  "smtp_from_name",
}

// ReadLocalizedTexts 构建编辑值，历史原文在管理员选择之前保持未知语言。
func ReadLocalizedTexts(values map[string]string) LocalizedTexts {
	result := LocalizedTexts{}
	for field, key := range LocalizedTextFields {
		value := values[key]
		if field == "balance_unit_name" && value == "" {
			value = "USD"
		}
		content := locale.Original(value)
		_ = json.Unmarshal([]byte(values[locale.TextSettingKey(key)]), &content)
		result[field] = locale.Update[string]{Content: content}
	}
	return result
}

// LocalizationParticipant 校验文案及链接，在数据库事务中检查已保存版本。
func LocalizationParticipant() Participant {
	keys := make([]string, 0, len(LocalizedTextFields))
	for _, key := range LocalizedTextFields {
		keys = append(keys, locale.TextSettingKey(key))
	}
	return Participant{Module: "localized-texts", Fields: []string{"localized_settings"}, Keys: keys, Prepare: func(_ context.Context, input Fields, current map[string]string) (PreparedChange, error) {
		var fields LocalizedTexts
		if raw, exists := input["localized_settings"]; exists {
			if err := json.Unmarshal(raw, &fields); err != nil {
				return PreparedChange{}, err
			}
		}
		change := PreparedChange{Values: map[string]string{}, Expected: map[string]*string{}}
		for field, update := range fields {
			key, known := LocalizedTextFields[field]
			if !known {
				return change, apperror.BadRequest("UNKNOWN_LOCALIZED_FIELD", "Unknown localized field.")
			}
			stored := locale.TextSettingKey(key)
			old := ReadLocalizedTexts(current)[field].Content
			if raw, exists := current[stored]; exists {
				if err := json.Unmarshal([]byte(raw), &old); err != nil {
					return change, err
				}
				change.Expected[stored] = &raw
			} else {
				change.Expected[stored] = nil
			}
			if reflect.DeepEqual(old, update.Content) && len(update.ReviewedLocales) == 0 && len(update.DeletedLocales) == 0 {
				delete(change.Expected, stored)
				continue
			}
			next, err := locale.Prepare(old, update, func(value string) error {
				limit := 65536
				if field == "payment_help_image_url" {
					limit = 4 << 20
				}
				if len(value) > limit {
					return apperror.BadRequest("LOCALIZED_TEXT_TOO_LONG", "Text exceeds the allowed length.")
				}
				if field == "payment_help_image_url" && strings.HasPrefix(value, "data:image/") {
					return nil
				}
				if strings.HasSuffix(field, "_url") && strings.TrimSpace(value) != "" {
					parsed, err := url.Parse(value)
					if err != nil || parsed.Scheme != "" && parsed.Scheme != "http" && parsed.Scheme != "https" {
						return apperror.BadRequest("INVALID_LINK_URL", "Link URL is invalid.")
					}
				}
				return nil
			})
			if err != nil {
				return change, err
			}
			body, err := json.Marshal(next)
			if err != nil {
				return change, err
			}
			change.Values[stored] = string(body)
		}
		return change, nil
	}}
}
