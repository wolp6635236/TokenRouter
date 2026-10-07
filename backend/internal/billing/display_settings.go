package billing

import (
	"context"
	"strings"

	"github.com/TokenFlux/TokenRouter/internal/pkg/locale"
)

// ReadBalanceUnitName 按键读取余额单位并去除空白，读取失败时返回 USD。
func ReadBalanceUnitName(ctx context.Context, store interface {
	GetValue(context.Context, string) (string, error)
},
) string {
	value := locale.ReadSettingText(ctx, store, SettingKeyBalanceUnitName, "USD")
	value = strings.TrimSpace(value)
	if value == "" {
		return "USD"
	}
	return value
}
