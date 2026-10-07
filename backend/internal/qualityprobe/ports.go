package qualityprobe

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// TextResult 是一次文字探测的回答。
type TextResult struct {
	Answer string
	Error  string
}

// Prober 通过提供商测试通道发送提示词。
type Prober interface {
	ProbeText(context.Context, int64, string, string) (TextResult, error)
}

// Snapshot 是探测需要的提供商字段。
type Snapshot struct {
	ID       int64
	Name     string
	Platform string
	Status   string
	// SchedulingOff 为 true 表示管理员关掉了「参与调度」。
	SchedulingOff           bool
	Schedulable             bool
	GroupIDs                []int64
	Extra                   map[string]any
	TempUnschedulableUntil  *time.Time
	TempUnschedulableReason string
}

// Directory 读取和更新提供商。
type Directory interface {
	Get(context.Context, int64) (*Snapshot, error)
	ListByPlatform(context.Context, string) ([]Snapshot, error)
	UpdateExtra(context.Context, int64, map[string]any) error
	SetTempUnschedulable(context.Context, int64, time.Time, string) error
	ClearTempUnschedulable(context.Context, int64) error
	SchedulableIDs(context.Context, int64) ([]int64, error)
}

// GroupIndex 提供当前启用中的分组 ID。
type GroupIndex interface {
	ActiveIDs(context.Context) ([]int64, error)
}

// Catalog 返回该提供商可测的模型名。
type Catalog interface {
	Models(context.Context, *Snapshot) []string
}

// Mailer 发送降智通知。
type Mailer interface {
	Send(context.Context, string, string, string) error
}

// SettingsRepo 读写探测配置。
type SettingsRepo interface {
	GetValue(context.Context, string) (string, error)
	Set(context.Context, string, string) error
}

// Engine 编排一轮探测并落状态。
type Engine struct {
	Settings SettingsRepo
	Dir      Directory
	Groups   GroupIndex
	Prober   Prober
	Catalog  Catalog
	Mail     Mailer
	Now      func() time.Time
	NotFound error
	// AnalyzeTrace 覆盖默认指纹归因，测试里注入。
	AnalyzeTrace func([]TraceOutput) (TraceResult, error)
}

// RunReport 是一轮探测的对外结果。
type RunReport struct {
	Skipped          bool          `json:"skipped"`
	SkipReason       string        `json:"skip_reason,omitempty"`
	ProviderID       int64         `json:"provider_id"`
	Model            string        `json:"model,omitempty"`
	CandyOK          bool          `json:"candy_ok"`
	TraceOK          bool          `json:"trace_ok"`
	TracePrediction  string        `json:"trace_prediction,omitempty"`
	TraceProbability float64       `json:"trace_probability,omitempty"`
	Degraded         bool          `json:"degraded"`
	UpstreamError    bool          `json:"upstream_error,omitempty"`
	TempUnscheduled  bool          `json:"temp_unscheduled"`
	KeptForCoverage  bool          `json:"kept_for_coverage"`
	EmailSent        bool          `json:"email_sent"`
	ConsecutiveFails int           `json:"consecutive_fails"`
	CycleStopped     bool          `json:"cycle_stopped"`
	Error            string        `json:"error,omitempty"`
	Trigger          Trigger       `json:"trigger,omitempty"`
	At               time.Time     `json:"at,omitempty"`
	ProviderName     string        `json:"provider_name,omitempty"`
	Samples          []ProbeSample `json:"samples,omitempty"`
}

func (e *Engine) now() time.Time {
	if e != nil && e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

// LoadSettings 读取运行配置，缺省或损坏时返回默认值。
func (e *Engine) LoadSettings(ctx context.Context) (Settings, error) {
	if e == nil || e.Settings == nil {
		return DefaultSettings(), nil
	}
	raw, err := e.Settings.GetValue(ctx, SettingKeyQualityProbe)
	if err != nil {
		if e.NotFound != nil && errors.Is(err, e.NotFound) {
			return DefaultSettings(), nil
		}
		return DefaultSettings(), err
	}
	if strings.TrimSpace(raw) == "" {
		return DefaultSettings(), nil
	}
	var file SettingsPayload
	if err := json.Unmarshal([]byte(raw), &file); err != nil {
		return DefaultSettings(), nil
	}
	return file.Settings(), nil
}

// SaveSettings 写入运行配置。关闭「降智后停调」时，立即清掉 OpenAI 账号上的 quality_degraded 临时停调。
func (e *Engine) SaveSettings(ctx context.Context, cfg Settings) error {
	if e == nil || e.Settings == nil {
		return errors.New("quality probe settings store is missing")
	}
	previous, err := e.LoadSettings(ctx)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(SettingsDTO(cfg))
	if err != nil {
		return err
	}
	if err := e.Settings.Set(ctx, SettingKeyQualityProbe, string(payload)); err != nil {
		return err
	}
	if previous.UnscheduleOnDegraded && !cfg.UnscheduleOnDegraded {
		if clearErr := e.clearQualityDegraded(ctx); clearErr != nil {
			return clearErr
		}
	}
	return nil
}

// clearQualityDegraded 清掉 OpenAI 提供商上由降智探测写入的临时停调。
func (e *Engine) clearQualityDegraded(ctx context.Context) error {
	if e == nil || e.Dir == nil {
		return nil
	}
	items, err := e.Dir.ListByPlatform(ctx, PlatformOpenAI)
	if err != nil {
		return err
	}
	writeCtx := persistCtx(ctx)
	for i := range items {
		if strings.TrimSpace(items[i].TempUnschedulableReason) != TempUnscheduleReason {
			continue
		}
		if err := e.Dir.ClearTempUnschedulable(writeCtx, items[i].ID); err != nil {
			return err
		}
	}
	return nil
}

// SettingsPayload 是管理端读写的 JSON 形状。
type SettingsPayload struct {
	Enabled              bool    `json:"enabled"`
	IntervalMinutes      int     `json:"interval_minutes"`
	Model                string  `json:"model"`
	CooldownMinutes      int     `json:"cooldown_minutes"`
	MaxAttempts          int     `json:"max_attempts"`
	NotifyEmail          string  `json:"notify_email"`
	GroupIDs             []int64 `json:"group_ids"`
	ScheduleEnabled      *bool   `json:"schedule_enabled"`
	ScheduleStart        string  `json:"schedule_start"`
	ScheduleEnd          string  `json:"schedule_end"`
	UnscheduleOnDegraded *bool   `json:"unschedule_on_degraded"`
}

func fileFromSettings(cfg Settings) SettingsPayload {
	interval := int(cfg.Interval / time.Minute)
	if interval <= 0 {
		interval = 30
	}
	cooldown := int(cfg.Cooldown / time.Minute)
	if cooldown <= 0 {
		cooldown = 5
	}
	maxAttempts := cfg.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	email := strings.TrimSpace(cfg.NotifyEmail)
	if email == "" {
		email = DefaultNotifyEmail
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = DefaultProbeModel
	}
	scheduleEnabled := cfg.ScheduleEnabled
	unscheduleOnDegraded := cfg.UnscheduleOnDegraded
	return SettingsPayload{
		Enabled:              cfg.Enabled,
		IntervalMinutes:      interval,
		Model:                model,
		CooldownMinutes:      cooldown,
		MaxAttempts:          maxAttempts,
		NotifyEmail:          email,
		GroupIDs:             NormalizeGroupIDs(cfg.GroupIDs),
		ScheduleEnabled:      &scheduleEnabled,
		ScheduleStart:        NormalizeClock(cfg.ScheduleStart, DefaultScheduleStart),
		ScheduleEnd:          NormalizeClock(cfg.ScheduleEnd, DefaultScheduleEnd),
		UnscheduleOnDegraded: &unscheduleOnDegraded,
	}
}

func (f SettingsPayload) Settings() Settings {
	cfg := DefaultSettings()
	cfg.Enabled = f.Enabled
	if f.IntervalMinutes > 0 {
		cfg.Interval = time.Duration(f.IntervalMinutes) * time.Minute
	}
	if strings.TrimSpace(f.Model) != "" {
		cfg.Model = strings.TrimSpace(f.Model)
	}
	if f.CooldownMinutes > 0 {
		cfg.Cooldown = time.Duration(f.CooldownMinutes) * time.Minute
	}
	if f.MaxAttempts > 0 {
		cfg.MaxAttempts = f.MaxAttempts
	}
	if strings.TrimSpace(f.NotifyEmail) != "" {
		cfg.NotifyEmail = strings.TrimSpace(f.NotifyEmail)
	}
	cfg.GroupIDs = NormalizeGroupIDs(f.GroupIDs)
	if f.ScheduleEnabled != nil {
		cfg.ScheduleEnabled = *f.ScheduleEnabled
	}
	if strings.TrimSpace(f.ScheduleStart) != "" {
		cfg.ScheduleStart = NormalizeClock(f.ScheduleStart, DefaultScheduleStart)
	}
	if strings.TrimSpace(f.ScheduleEnd) != "" {
		cfg.ScheduleEnd = NormalizeClock(f.ScheduleEnd, DefaultScheduleEnd)
	}
	if f.UnscheduleOnDegraded != nil {
		cfg.UnscheduleOnDegraded = *f.UnscheduleOnDegraded
	}
	return cfg
}

// SettingsDTO 返回给管理端的设置。
func SettingsDTO(cfg Settings) SettingsPayload {
	return fileFromSettings(cfg)
}
