package provider

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/payment"
)

// TestEasyPayNotificationRejectsCheckoutParameters 检查下单参数不能被当作通知接收。
func TestEasyPayNotificationRejectsCheckoutParameters(t *testing.T) {
	t.Parallel()

	e := &EasyPay{config: map[string]string{
		"pid": "1000", "pkey": "test-merchant-secret",
		"apiBase": "https://pay.example.com", "paymentMode": "popup",
		"notifyUrl": "https://site.example.com/api/v1/payment/webhook/easypay",
	}}
	created, err := e.CreatePayment(context.Background(), payment.CreatePaymentRequest{
		OrderID: "ORDER123", PaymentType: payment.TypeAlipay,
		Subject: "balance recharge", Amount: "650.00",
		ReturnURL: "https://site.example.com/payment/result",
	})
	if err != nil {
		t.Fatal(err)
	}
	payURL, err := url.Parse(created.PayURL)
	if err != nil {
		t.Fatal(err)
	}
	notification, err := e.VerifyNotification(context.Background(), payURL.RawQuery, nil)
	if err == nil || notification != nil {
		t.Fatalf("checkout parameters accepted: notification=%+v, err=%v", notification, err)
	}
}

// TestEasyPayNotificationParameters 覆盖标准通知、额外字段和重复字段。
func TestEasyPayNotificationParameters(t *testing.T) {
	t.Parallel()

	e := &EasyPay{config: map[string]string{"pid": "1000", "pkey": "test-merchant-secret"}}
	tests := []struct {
		name      string
		payType   string
		extraKey  string
		extra     string
		duplicate string
		wantError bool
	}{
		{name: "standard notification"},
		{name: "wechat notification", payType: "wxpay"},
		{name: "custom payment type", payType: "usdt_trc20"},
		{name: "optional param", extraKey: "param", extra: "merchant=value&extra=中文"},
		{name: "empty optional param", extraKey: "param"},
		{name: "return URL", extraKey: "return_url", extra: "https://site.example.com/payment/result", wantError: true},
		{name: "notify URL", extraKey: "notify_url", extra: "https://site.example.com/notify", wantError: true},
		{name: "device", extraKey: "device", extra: "mobile", wantError: true},
		{name: "unknown field", extraKey: "unknown", extra: "value", wantError: true},
		{name: "empty unknown field", extraKey: "unknown", wantError: true},
		{name: "empty return URL", extraKey: "return_url", wantError: true},
		{name: "duplicate status", duplicate: "trade_status", wantError: true},
		{name: "duplicate order", duplicate: "out_trade_no", wantError: true},
		{name: "duplicate signature", duplicate: "sign", wantError: true},
		{name: "duplicate amount", duplicate: "money", wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			params := map[string]string{
				"pid": "1000", "trade_no": "UPSTREAM123", "out_trade_no": "ORDER123",
				"type": "alipay", "name": "充值 & 套餐=10%", "money": "650.00",
				"trade_status": tradeStatusSuccess,
			}
			if tt.payType != "" {
				params["type"] = tt.payType
			}
			if tt.extraKey != "" {
				params[tt.extraKey] = tt.extra
			}
			params["sign"] = easyPaySign(params, e.config["pkey"])
			params["sign_type"] = signTypeMD5
			values := url.Values{}
			for key, value := range params {
				values.Set(key, value)
			}
			if tt.duplicate != "" {
				values.Add(tt.duplicate, "different-value")
			}
			raw := values.Encode()
			notification, err := e.VerifyNotification(context.Background(), raw, nil)
			if tt.wantError {
				if err == nil || notification != nil {
					t.Fatalf("invalid parameters accepted: notification=%+v, err=%v", notification, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if notification.Status != payment.ProviderStatusSuccess || notification.Amount != 650 ||
				notification.OrderID != "ORDER123" || notification.TradeNo != "UPSTREAM123" ||
				notification.Metadata["pid"] != "1000" || notification.RawData != raw {
				t.Fatalf("unexpected notification: %+v", notification)
			}
			// 同一份有效通知篡改金额后应当验签失败。
			values.Set("money", "651.00")
			if _, err := e.VerifyNotification(context.Background(), values.Encode(), nil); err == nil || !strings.Contains(err.Error(), "invalid signature") {
				t.Fatalf("tampered notification: %v", err)
			}
		})
	}
}

// TestEasyPayNotificationRejectsInvalidFields 检查单个无效字段在验签前被拒绝。
func TestEasyPayNotificationRejectsInvalidFields(t *testing.T) {
	t.Parallel()

	e := &EasyPay{config: map[string]string{"pid": "1000", "pkey": "test-merchant-secret"}}
	for _, key := range []string{"pid", "trade_no", "out_trade_no", "type", "money", "trade_status"} {
		for _, value := range []string{"", "demo&label", "demo=label", "demo\x00label", "demo\rlabel", "demo\nlabel", " demo", "demo "} {
			t.Run(key+"/"+value, func(t *testing.T) {
				t.Parallel()
				values := easyPayTestNotificationValues()
				values.Set(key, value)
				want := "invalid notify param: " + key
				if value == "" {
					want = "missing notify param: " + key
				}
				notification, err := e.VerifyNotification(context.Background(), values.Encode(), nil)
				if err == nil || err.Error() != want || notification != nil {
					t.Fatalf("field validation: notification=%+v, err=%v, want %q", notification, err, want)
				}
			})
		}
	}
	for _, tt := range []struct {
		key   string
		value string
		want  string
	}{
		{key: "pid", value: "2000", want: "easypay notify pid mismatch"},
		{key: "type", value: "alipay/demo", want: "invalid notify param: type"},
		{key: "type", value: "alipay@demo", want: "invalid notify param: type"},
		{key: "type", value: "alipay%26demo", want: "invalid notify param: type"},
		{key: "money", value: "NaN", want: "invalid notify param: money"},
		{key: "money", value: "+Inf", want: "invalid notify param: money"},
		{key: "money", value: "0", want: "invalid notify param: money"},
		{key: "money", value: "-1", want: "invalid notify param: money"},
		{key: "money", value: "invalid", want: "invalid notify param: money"},
	} {
		t.Run(tt.key+"/"+tt.value, func(t *testing.T) {
			t.Parallel()
			values := easyPayTestNotificationValues()
			values.Set(tt.key, tt.value)
			notification, err := e.VerifyNotification(context.Background(), values.Encode(), nil)
			if err == nil || err.Error() != tt.want || notification != nil {
				t.Fatalf("field validation: notification=%+v, err=%v, want %q", notification, err, tt.want)
			}
		})
	}
}

// easyPayTestNotificationValues 提供没有签名的标准通知字段，供输入校验测试使用。
func easyPayTestNotificationValues() url.Values {
	return url.Values{
		"pid": {"1000"}, "trade_no": {"UPSTREAM123"}, "out_trade_no": {"ORDER123"},
		"type": {"alipay"}, "name": {"充值 & 套餐=10%"}, "money": {"650.00"},
		"trade_status": {tradeStatusSuccess},
	}
}
