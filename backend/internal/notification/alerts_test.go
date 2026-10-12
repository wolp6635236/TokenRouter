package notification

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildBalanceLowEmailBody_ContainsRequiredFields(t *testing.T) {
	s := &AlertDelivery{}
	body := s.BuildBalanceLowEmailBody("Alice", 3.14, 10.0, "MySite", "")

	// 模板变量替换后应包含各字段值。
	require.Contains(t, body, "MySite")
	require.Contains(t, body, "Alice")
	require.Contains(t, body, "$3.14")
	require.Contains(t, body, "$10.00")

	// 格式化参数数量错误会在输出中留下错误标记。
	require.NotContains(t, body, "%!")
	require.NotContains(t, body, "MISSING")
	require.NotContains(t, body, "EXTRA")
}

func TestBuildBalanceLowEmailBody_WithRechargeURL(t *testing.T) {
	s := &AlertDelivery{}
	body := s.BuildBalanceLowEmailBody("Bob", 5.0, 20.0, "Site", "https://example.com/pay")

	// 充值链接使用传入的 URL。
	require.Contains(t, body, `href="https://example.com/pay"`)
	require.Contains(t, body, "立即充值")
	require.NotContains(t, body, "%!")
}

func TestBuildBalanceLowEmailBody_RechargeURLEscaped(t *testing.T) {
	s := &AlertDelivery{}
	// URL 包含需要 HTML 转义的字符。
	body := s.BuildBalanceLowEmailBody("u", 1.0, 5.0, "Site", `https://example.com/?a=1&b=<script>`)

	// href 中的 & 和 < 需要转义。
	require.Contains(t, body, "&amp;")
	require.Contains(t, body, "&lt;script&gt;")
	require.NotContains(t, body, "<script>")
}

func TestBuildBalanceLowEmailBody_NoRechargeURLOmitsButton(t *testing.T) {
	s := &AlertDelivery{}
	body := s.BuildBalanceLowEmailBody("u", 1.0, 5.0, "Site", "")
	// 空充值 URL 对应的模板省略充值链接。
	require.NotContains(t, body, `<a href`)
	require.NotContains(t, body, "立即充值")
}

func TestBuildQuotaAlertEmailBody_AllFieldsPresent(t *testing.T) {
	s := &AlertDelivery{}
	body := s.BuildQuotaAlertEmailBody(
		42,            // providerID
		"acc-foo",     // providerName
		"anthropic",   // platform
		"日限额 / Daily", // dimLabel
		750.50,        // used
		1000.0,        // limit
		249.50,        // remaining
		"$249.50",     // thresholdDisplay
		"MySite",      // siteName
	)

	require.Contains(t, body, "MySite")
	require.Contains(t, body, "#42")
	require.Contains(t, body, "acc-foo")
	require.Contains(t, body, "anthropic")
	require.Contains(t, body, "Daily")
	require.Contains(t, body, "$750.50")
	require.Contains(t, body, "$1000.00")
	require.Contains(t, body, "$249.50")

	// 格式化参数数量错误会在输出中留下错误标记。
	require.NotContains(t, body, "%!")
	require.NotContains(t, body, "MISSING")
	require.NotContains(t, body, "EXTRA")
}

func TestBuildQuotaAlertEmailBody_UnlimitedDisplay(t *testing.T) {
	s := &AlertDelivery{}
	body := s.BuildQuotaAlertEmailBody(
		1, "n", "p", "dim",
		100.0, 0.0, // limit=0 表示无限额度
		0.0, "30%", "Site",
	)
	require.Contains(t, body, "无限制")
	require.Contains(t, body, "Unlimited")
}

func TestBuildQuotaAlertEmailBody_PercentageThresholdDisplay(t *testing.T) {
	s := &AlertDelivery{}
	body := s.BuildQuotaAlertEmailBody(
		1, "n", "p", "dim",
		700.0, 1000.0, 300.0,
		"30%", // 百分比形式的阈值
		"Site",
	)
	require.Contains(t, body, "30%")
	require.NotContains(t, body, "%!")
}

func TestBuildQuotaAlertEmailBody_RemainingClampedAtZero(t *testing.T) {
	// 调用方把剩余额度截为零，这里检查零值的展示。
	s := &AlertDelivery{}
	body := s.BuildQuotaAlertEmailBody(
		1, "n", "p", "dim",
		1500.0, 1000.0, 0.0, // 用量超过额度
		"$100.00", "Site",
	)
	require.Contains(t, body, "$0.00")
}

func TestBuildBalanceLowEmailBody_NoCSSFormatError(t *testing.T) {
	s := &AlertDelivery{}
	body := s.BuildBalanceLowEmailBody("u", 1.0, 5.0, "Site", "")
	// CSS 渐变中的百分号由模板的 %% 转义得到。
	require.True(t,
		strings.Contains(body, "0%") && strings.Contains(body, "100%"),
		"CSS gradient percentages not rendered; got: %s", body)
}

func TestBuildQuotaAlertEmailBody_NoCSSFormatError(t *testing.T) {
	s := &AlertDelivery{}
	body := s.BuildQuotaAlertEmailBody(1, "n", "p", "d", 0, 0, 0, "$0.00", "Site")
	require.True(t,
		strings.Contains(body, "0%") && strings.Contains(body, "100%"),
		"CSS gradient percentages not rendered; got: %s", body)
}
