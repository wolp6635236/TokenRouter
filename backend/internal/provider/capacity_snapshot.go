package provider

import (
	"time"
)

// GroupProviderCapacityRow 包含容量汇总需要的提供商字段。
type GroupProviderCapacityRow struct {
	GroupID             int64
	ProviderID          int64
	Platform            string
	Concurrency         int
	Extra               map[string]any
	SessionWindowStart  *time.Time
	SessionWindowEnd    *time.Time
	SessionWindowStatus string
}

// CapacitySnapshot 不包含凭据或原始 Extra，供路由聚合独立消费。
type CapacitySnapshot struct {
	ID                        int64
	Concurrency               int
	MaxSessions               int
	SessionIdleTimeoutMinutes int
	BaseRPM                   int
	QuotaAutoPaused           bool
}

type LoadObservation struct {
	ID             int64
	MaxConcurrency int
}

func ProjectCapacity(id int64, config RuntimeConfig, quotaAutoPaused bool) CapacitySnapshot {
	return CapacitySnapshot{ID: id, Concurrency: config.Concurrency, MaxSessions: config.GetMaxSessions(), SessionIdleTimeoutMinutes: config.GetSessionIdleTimeoutMinutes(), BaseRPM: config.GetBaseRPM(), QuotaAutoPaused: quotaAutoPaused}
}

// ProjectObservedCapacity 汇总运行参数和阈值判断结果，调用方逐行传入观测时间。
func ProjectObservedCapacity(row GroupProviderCapacityRow, settings QuotaAutoPauseSettings, now time.Time) CapacitySnapshot {
	paused, _ := EvaluateQuotaAutoPause(row.Platform, row.Extra, settings, now)
	return ProjectCapacity(row.ProviderID, RuntimeConfig{Extra: row.Extra, Concurrency: row.Concurrency, SessionWindowStart: row.SessionWindowStart, SessionWindowEnd: row.SessionWindowEnd}, paused)
}
