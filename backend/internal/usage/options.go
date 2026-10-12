package usage

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/pkg/timezone"
	p "github.com/TokenFlux/TokenRouter/internal/settings/preaggregation"
)

var ErrCleanupTaskNotFound = errors.New("usage cleanup task not found")

type Options struct {
	Calendar      timezone.Calendar
	RunBackground func(string, func()) bool
	DashboardAgg  DashboardAggregationConfig
	UsageCleanup  UsageCleanupConfig
	Dashboard     DashboardConfig
	Logf          func(string, string, ...any)
}
type DashboardAggregationConfig struct {
	Enabled                          bool
	IntervalSeconds, LookbackSeconds int
	BackfillEnabled                  bool
	BackfillMaxDays                  int
	Retention                        DashboardAggregationRetentionConfig
	RecomputeDays                    int
}
type (
	DashboardAggregationRetentionConfig struct{ UsageLogsDays, UsageBillingDedupDays, HourlyDays, DailyDays int }
	UsageCleanupConfig                  struct {
		Enabled                                                            bool
		MaxRangeDays, BatchSize, WorkerIntervalSeconds, TaskTimeoutSeconds int
	}
)

type DashboardConfig struct {
	Enabled                                                           bool
	StatsFreshTTLSeconds, StatsTTLSeconds, StatsRefreshTimeoutSeconds int
}
type TimingWheel interface {
	ScheduleRecurring(string, time.Duration, func())
	CancelAndWait(string)
}
type AggregationSettings interface {
	UsageEnabled(context.Context) bool
	Resolve(context.Context) p.PreAggregationSettings
	RegisterListener(func(p.PreAggregationSettings, p.PreAggregationSettings))
}
type (
	PreAggregationSettings = p.PreAggregationSettings
	SingletonLocker        func(context.Context, string, string, time.Duration) (func(), bool)
)

func logUsage(component, format string, args ...any) { log.Printf(format, args...) }

func usageReporter(options *Options) func(string, string, ...any) {
	if options == nil {
		return nil
	}
	return options.Logf
}
