package payment

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	infraerrors "github.com/TokenFlux/TokenRouter/internal/pkg/apperror"
	"github.com/TokenFlux/TokenRouter/internal/settings"
)

// VisibleMethodSettings 包含管理入口的支付方式展示设置。
type VisibleMethodSettings struct {
	PaymentVisibleMethodAlipaySource  string `json:"payment_visible_method_alipay_source"`
	PaymentVisibleMethodWxpaySource   string `json:"payment_visible_method_wxpay_source"`
	PaymentVisibleMethodAlipayEnabled bool   `json:"payment_visible_method_alipay_enabled"`
	PaymentVisibleMethodWxpayEnabled  bool   `json:"payment_visible_method_wxpay_enabled"`
}

// AdminReadSettings 包含支付方式的展示开关和来源。
type AdminReadSettings struct {
	PaymentVisibleMethodAlipayEnabled bool
	PaymentVisibleMethodAlipaySource  string
	PaymentVisibleMethodWxpayEnabled  bool
	PaymentVisibleMethodWxpaySource   string
}

// PrepareVisibleMethodSettings 校验支付方式来源并生成待保存的设置值。
func PrepareVisibleMethodSettings(value *VisibleMethodSettings) (map[string]string, error) {
	var err error
	value.PaymentVisibleMethodAlipaySource, err = NormalizeVisibleMethodSettingSource("alipay", value.PaymentVisibleMethodAlipaySource, value.PaymentVisibleMethodAlipayEnabled)
	if err != nil {
		return nil, err
	}
	value.PaymentVisibleMethodWxpaySource, err = NormalizeVisibleMethodSettingSource("wxpay", value.PaymentVisibleMethodWxpaySource, value.PaymentVisibleMethodWxpayEnabled)
	if err != nil {
		return nil, err
	}
	return map[string]string{
		SettingPaymentVisibleMethodAlipaySource:  value.PaymentVisibleMethodAlipaySource,
		SettingPaymentVisibleMethodWxpaySource:   value.PaymentVisibleMethodWxpaySource,
		SettingPaymentVisibleMethodAlipayEnabled: strconv.FormatBool(value.PaymentVisibleMethodAlipayEnabled),
		SettingPaymentVisibleMethodWxpayEnabled:  strconv.FormatBool(value.PaymentVisibleMethodWxpayEnabled),
	}, nil
}

// VisibleSettingsParticipant 保存输入包含的支付方式展示字段。
func VisibleSettingsParticipant() settings.Participant {
	keys := []string{SettingPaymentVisibleMethodAlipaySource, SettingPaymentVisibleMethodWxpaySource, SettingPaymentVisibleMethodAlipayEnabled, SettingPaymentVisibleMethodWxpayEnabled}
	return settings.Participant{Module: "payment-visible-methods", Fields: keys, Keys: keys, Prepare: func(_ context.Context, input settings.Fields, _ map[string]string) (settings.PreparedChange, error) {
		if len(input) == 0 {
			return settings.PreparedChange{}, nil
		}
		raw, err := json.Marshal(input)
		if err != nil {
			return settings.PreparedChange{}, err
		}
		var value VisibleMethodSettings
		if err = json.Unmarshal(raw, &value); err != nil {
			return settings.PreparedChange{}, err
		}
		values, err := PrepareVisibleMethodSettings(&value)
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

// ReadAdminSettings 从传入的设置值解析支付方式的展示开关和来源。
func ReadAdminSettings(settings map[string]string) *AdminReadSettings {
	result := &AdminReadSettings{}

	result.PaymentVisibleMethodAlipaySource = NormalizeVisibleMethodSource("alipay", settings[SettingPaymentVisibleMethodAlipaySource])
	result.PaymentVisibleMethodWxpaySource = NormalizeVisibleMethodSource("wxpay", settings[SettingPaymentVisibleMethodWxpaySource])
	result.PaymentVisibleMethodAlipayEnabled = settings[SettingPaymentVisibleMethodAlipayEnabled] == "true"
	result.PaymentVisibleMethodWxpayEnabled = settings[SettingPaymentVisibleMethodWxpayEnabled] == "true"
	return result
}

// NormalizeVisibleMethodSettingSource 校验支付方式来源，空值表示自动选择。
func NormalizeVisibleMethodSettingSource(method, source string, enabled bool) (string, error) {
	_ = enabled
	source = strings.TrimSpace(source)
	if source == "" {
		return "", nil
	}

	normalized := NormalizeVisibleMethodSource(method, source)
	if normalized == "" {
		return "", infraerrors.BadRequest(
			"INVALID_PAYMENT_VISIBLE_METHOD_SOURCE",
			fmt.Sprintf("%s source must be one of the supported payment providers", method),
		)
	}
	return normalized, nil
}
