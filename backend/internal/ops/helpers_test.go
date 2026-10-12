package ops

import (
	"context"
	"maps"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/settings"
)

const (
	PlatformOpenAI    = capability.PlatformOpenAI
	PlatformAnthropic = capability.PlatformAnthropic
	StatusActive      = "active"
	StatusError       = "error"
)

var _ OpsRepository = (*opsRepoMock)(nil)

// opsRepoMock is a test-only OpsRepository implementation with optional function hooks.
type opsRepoMock struct {
	InsertErrorLogFn              func(ctx context.Context, input *OpsInsertErrorLogInput) (int64, error)
	BatchInsertErrorLogsFn        func(ctx context.Context, inputs []*OpsInsertErrorLogInput) (int64, error)
	BatchInsertSystemLogsFn       func(ctx context.Context, inputs []*OpsInsertSystemLogInput) (int64, error)
	ListSystemLogsFn              func(ctx context.Context, filter *OpsSystemLogFilter) (*OpsSystemLogList, error)
	ListRequestTimingsFn          func(ctx context.Context, clientRequestIDs []string) (map[string]*OpsRequestTiming, error)
	DeleteSystemLogsFn            func(ctx context.Context, filter *OpsSystemLogCleanupFilter) (int64, error)
	InsertSystemLogCleanupAuditFn func(ctx context.Context, input *OpsSystemLogCleanupAudit) error
}

type Setting = settings.Setting

type runtimeSettingRepoStub struct {
	values           map[string]string
	deleted          map[string]bool
	setCalls         int
	getValueCalls    int
	getMultipleCalls int
	getValueFn       func(key string) (string, error)
	setFn            func(key, value string) error
	deleteFn         func(key string) error
}

func timePtr(v time.Time) *time.Time { return &v }

func newLegacyShapeOpsService(repo OpsRepository, settings Settings, cfg *Options, a ProviderReader, u UserReader, c ConcurrencyReader, _ any, _ any, _ any, _ any, sink *OpsSystemLogSink) *OpsService {
	return NewOpsService(repo, settings, cfg, a, u, c, sink, nil)
}

func (m *opsRepoMock) InsertErrorLog(ctx context.Context, input *OpsInsertErrorLogInput) (int64, error) {
	if m.InsertErrorLogFn != nil {
		return m.InsertErrorLogFn(ctx, input)
	}
	return 0, nil
}

func (m *opsRepoMock) BatchInsertErrorLogs(ctx context.Context, inputs []*OpsInsertErrorLogInput) (int64, error) {
	if m.BatchInsertErrorLogsFn != nil {
		return m.BatchInsertErrorLogsFn(ctx, inputs)
	}
	return int64(len(inputs)), nil
}

func (m *opsRepoMock) ListErrorLogs(ctx context.Context, filter *OpsErrorLogFilter) (*OpsErrorLogList, error) {
	return &OpsErrorLogList{Errors: []*OpsErrorLog{}, Page: 1, PageSize: 20}, nil
}

func (m *opsRepoMock) GetErrorLogByID(ctx context.Context, id int64) (*OpsErrorLogDetail, error) {
	return &OpsErrorLogDetail{}, nil
}

func (m *opsRepoMock) ListRequestDetails(ctx context.Context, filter *OpsRequestDetailFilter) ([]*OpsRequestDetail, int64, error) {
	return []*OpsRequestDetail{}, 0, nil
}

func (m *opsRepoMock) BatchInsertSystemLogs(ctx context.Context, inputs []*OpsInsertSystemLogInput) (int64, error) {
	if m.BatchInsertSystemLogsFn != nil {
		return m.BatchInsertSystemLogsFn(ctx, inputs)
	}
	return int64(len(inputs)), nil
}

func (m *opsRepoMock) ListSystemLogs(ctx context.Context, filter *OpsSystemLogFilter) (*OpsSystemLogList, error) {
	if m.ListSystemLogsFn != nil {
		return m.ListSystemLogsFn(ctx, filter)
	}
	return &OpsSystemLogList{Logs: []*OpsSystemLog{}, Total: 0, Page: 1, PageSize: 50}, nil
}

func (m *opsRepoMock) ListRequestTimings(ctx context.Context, clientRequestIDs []string) (map[string]*OpsRequestTiming, error) {
	if m.ListRequestTimingsFn != nil {
		return m.ListRequestTimingsFn(ctx, clientRequestIDs)
	}
	return map[string]*OpsRequestTiming{}, nil
}

func (m *opsRepoMock) DeleteSystemLogs(ctx context.Context, filter *OpsSystemLogCleanupFilter) (int64, error) {
	if m.DeleteSystemLogsFn != nil {
		return m.DeleteSystemLogsFn(ctx, filter)
	}
	return 0, nil
}

func (m *opsRepoMock) InsertSystemLogCleanupAudit(ctx context.Context, input *OpsSystemLogCleanupAudit) error {
	if m.InsertSystemLogCleanupAuditFn != nil {
		return m.InsertSystemLogCleanupAuditFn(ctx, input)
	}
	return nil
}

func (m *opsRepoMock) UpdateErrorResolution(ctx context.Context, errorID int64, resolved bool, resolvedByUserID *int64, resolvedAt *time.Time) error {
	return nil
}

func (m *opsRepoMock) GetWindowStats(ctx context.Context, filter *OpsDashboardFilter) (*OpsWindowStats, error) {
	return &OpsWindowStats{}, nil
}

func (m *opsRepoMock) GetRealtimeTrafficSummary(ctx context.Context, filter *OpsDashboardFilter) (*OpsRealtimeTrafficSummary, error) {
	return &OpsRealtimeTrafficSummary{}, nil
}

func (m *opsRepoMock) GetDashboardOverview(ctx context.Context, filter *OpsDashboardFilter) (*OpsDashboardOverview, error) {
	return &OpsDashboardOverview{}, nil
}

func (m *opsRepoMock) GetThroughputTrend(ctx context.Context, filter *OpsDashboardFilter, bucketSeconds int) (*OpsThroughputTrendResponse, error) {
	return &OpsThroughputTrendResponse{}, nil
}

func (m *opsRepoMock) GetLatencyHistogram(ctx context.Context, filter *OpsDashboardFilter, bucketBoundariesMS []int64) (*OpsLatencyHistogramResponse, error) {
	return &OpsLatencyHistogramResponse{}, nil
}

func (m *opsRepoMock) GetErrorTrend(ctx context.Context, filter *OpsDashboardFilter, bucketSeconds int) (*OpsErrorTrendResponse, error) {
	return &OpsErrorTrendResponse{}, nil
}

func (m *opsRepoMock) GetErrorDistribution(ctx context.Context, filter *OpsDashboardFilter) (*OpsErrorDistributionResponse, error) {
	return &OpsErrorDistributionResponse{}, nil
}

func (m *opsRepoMock) GetTokenStats(ctx context.Context, filter *OpsTokenStatsFilter) (*OpsTokenStatsResponse, error) {
	return &OpsTokenStatsResponse{}, nil
}

func (m *opsRepoMock) InsertSystemMetrics(ctx context.Context, input *OpsInsertSystemMetricsInput) error {
	return nil
}

func (m *opsRepoMock) GetLatestSystemMetrics(ctx context.Context, windowMinutes int) (*OpsSystemMetricsSnapshot, error) {
	return &OpsSystemMetricsSnapshot{}, nil
}

func (m *opsRepoMock) UpsertJobHeartbeat(ctx context.Context, input *OpsUpsertJobHeartbeatInput) error {
	return nil
}

func (m *opsRepoMock) ListJobHeartbeats(ctx context.Context) ([]*OpsJobHeartbeat, error) {
	return []*OpsJobHeartbeat{}, nil
}

func (m *opsRepoMock) ListAlertRules(ctx context.Context) ([]*OpsAlertRule, error) {
	return []*OpsAlertRule{}, nil
}

func (m *opsRepoMock) CreateAlertRule(ctx context.Context, input *OpsAlertRule) (*OpsAlertRule, error) {
	return input, nil
}

func (m *opsRepoMock) UpdateAlertRule(ctx context.Context, input *OpsAlertRule) (*OpsAlertRule, error) {
	return input, nil
}

func (m *opsRepoMock) DeleteAlertRule(ctx context.Context, id int64) error {
	return nil
}

func (m *opsRepoMock) ListAlertEvents(ctx context.Context, filter *OpsAlertEventFilter) ([]*OpsAlertEvent, error) {
	return []*OpsAlertEvent{}, nil
}

func (m *opsRepoMock) GetAlertEventByID(ctx context.Context, eventID int64) (*OpsAlertEvent, error) {
	return &OpsAlertEvent{}, nil
}

func (m *opsRepoMock) GetActiveAlertEvent(ctx context.Context, ruleID int64) (*OpsAlertEvent, error) {
	return nil, nil
}

func (m *opsRepoMock) GetLatestAlertEvent(ctx context.Context, ruleID int64) (*OpsAlertEvent, error) {
	return nil, nil
}

func (m *opsRepoMock) CreateAlertEvent(ctx context.Context, event *OpsAlertEvent) (*OpsAlertEvent, error) {
	return event, nil
}

func (m *opsRepoMock) UpdateAlertEventStatus(ctx context.Context, eventID int64, status string, resolvedAt *time.Time) error {
	return nil
}

func (m *opsRepoMock) UpdateAlertEventEmailSent(ctx context.Context, eventID int64, emailSent bool) error {
	return nil
}

func (m *opsRepoMock) CreateAlertSilence(ctx context.Context, input *OpsAlertSilence) (*OpsAlertSilence, error) {
	return input, nil
}

func (m *opsRepoMock) IsAlertSilenced(ctx context.Context, ruleID int64, platform string, groupID *int64, region *string, now time.Time) (bool, error) {
	return false, nil
}

func (m *opsRepoMock) UpsertHourlyMetrics(ctx context.Context, startTime, endTime time.Time, ignoredStatusCodes []int) error {
	return nil
}

func (m *opsRepoMock) UpsertDailyMetrics(ctx context.Context, startTime, endTime time.Time) error {
	return nil
}

func (m *opsRepoMock) GetLatestHourlyBucketStart(ctx context.Context) (time.Time, bool, error) {
	return time.Time{}, false, nil
}

func (m *opsRepoMock) GetLatestDailyBucketDate(ctx context.Context) (time.Time, bool, error) {
	return time.Time{}, false, nil
}

func newRuntimeSettingRepoStub() *runtimeSettingRepoStub {
	return &runtimeSettingRepoStub{
		values:  map[string]string{},
		deleted: map[string]bool{},
	}
}

func (s *runtimeSettingRepoStub) Get(ctx context.Context, key string) (*Setting, error) {
	value, err := s.GetValue(ctx, key)
	if err != nil {
		return nil, err
	}
	return &Setting{Key: key, Value: value}, nil
}

func (s *runtimeSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	s.getValueCalls++
	if s.getValueFn != nil {
		return s.getValueFn(key)
	}
	value, ok := s.values[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return value, nil
}

func (s *runtimeSettingRepoStub) Set(_ context.Context, key, value string) error {
	if s.setFn != nil {
		if err := s.setFn(key, value); err != nil {
			return err
		}
	}
	s.values[key] = value
	s.setCalls++
	return nil
}

func (s *runtimeSettingRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	s.getMultipleCalls++
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := s.values[key]; ok {
			out[key] = value
		}
	}
	return out, nil
}

func (s *runtimeSettingRepoStub) SetMultiple(_ context.Context, settings map[string]string) error {
	maps.Copy(s.values, settings)
	return nil
}

func (s *runtimeSettingRepoStub) GetAll(_ context.Context) (map[string]string, error) {
	out := make(map[string]string, len(s.values))
	maps.Copy(out, s.values)
	return out, nil
}

func (s *runtimeSettingRepoStub) Delete(_ context.Context, key string) error {
	if s.deleteFn != nil {
		if err := s.deleteFn(key); err != nil {
			return err
		}
	}
	if _, ok := s.values[key]; !ok {
		return ErrSettingNotFound
	}
	delete(s.values, key)
	s.deleted[key] = true
	return nil
}
