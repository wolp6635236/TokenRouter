package usageview

import (
	"github.com/TokenFlux/TokenRouter/internal/upstream/usageview"
)

// BillingSummary 和 QuotaWindow 是上游用量查询返回的账单与额度窗口。
type (
	BillingSummary = usageview.BillingSummary
	QuotaWindow    = usageview.QuotaWindow
)

// CloneQuotaWindow 复制额度窗口。
func CloneQuotaWindow(value *QuotaWindow) *QuotaWindow { return usageview.CloneQuotaWindow(value) }

// CloneBillingSummary 复制账单摘要。
func CloneBillingSummary(value *BillingSummary) *BillingSummary {
	return usageview.CloneBillingSummary(value)
}
