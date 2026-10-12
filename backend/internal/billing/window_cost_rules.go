package billing

import "time"

const (
	WindowCostSchedulable WindowCostSchedulability = iota
	WindowCostStickyOnly
	WindowCostNotSchedulable
)

// WindowCostSchedulability 表示可调度、仅粘性可用和不可调度三种费用窗口状态。
type WindowCostSchedulability int

func CheckWindowCost(current, limit, reserve float64) WindowCostSchedulability {
	if limit <= 0 || current < limit {
		return WindowCostSchedulable
	}
	if current < limit+reserve {
		return WindowCostStickyOnly
	}
	return WindowCostNotSchedulable
}

// CurrentCostWindowStart 返回活动窗口的起点，窗口过期后返回当前时区的整点。
func CurrentCostWindowStart(start, end *time.Time, now time.Time) time.Time {
	if start != nil && end != nil && now.Before(*end) {
		return *start
	}
	return time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, now.Location())
}
