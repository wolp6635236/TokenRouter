package qualityprobe

import (
	"context"
	"encoding/json"
	"time"
)

// persistCtx 给 Extra 和临时停调单独一套不随请求取消的 context。
// 关掉探测弹窗后 HTTP context 已经取消，写库需要还能成功。
func persistCtx(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return context.WithoutCancel(ctx)
}

const (
	// ExtraKey 是提供商 Extra 里保存探测状态的键。
	ExtraKey = "quality_probe"
	// TempUnscheduleReason 写入临时停调原因，恢复时按该值识别。
	TempUnscheduleReason = "quality_degraded"
	// MaxProbeHistory 是每个提供商 Extra 里保留的探测记录条数。
	MaxProbeHistory = 50
	// MaxSampleChars 是写入 Extra 的单段提问或回答按 rune 截断后的上限。
	MaxSampleChars = 4000
)

// StoredState 是写入 Extra 的探测循环状态。
type StoredState struct {
	ConsecutiveFails int        `json:"consecutive_fails"`
	CycleStopped     bool       `json:"cycle_stopped"`
	NextRetryAt      *time.Time `json:"next_retry_at,omitempty"`
	LastCandyOK      bool       `json:"last_candy_ok"`
	LastTraceOK      bool       `json:"last_trace_ok"`
	LastError        string     `json:"last_error,omitempty"`
	LastModel        string     `json:"last_model,omitempty"`
	UpdatedAt        time.Time  `json:"updated_at"`
	History          []ProbeLog `json:"history,omitempty"`
}

// ProbeLog 是一轮探测写入 Extra 的记录。
type ProbeLog struct {
	At               time.Time     `json:"at"`
	Trigger          Trigger       `json:"trigger"`
	Skipped          bool          `json:"skipped"`
	SkipReason       string        `json:"skip_reason,omitempty"`
	Model            string        `json:"model,omitempty"`
	CandyOK          bool          `json:"candy_ok"`
	TraceOK          bool          `json:"trace_ok"`
	TracePrediction  string        `json:"trace_prediction,omitempty"`
	TraceProbability float64       `json:"trace_probability,omitempty"`
	Degraded         bool          `json:"degraded"`
	TempUnscheduled  bool          `json:"temp_unscheduled"`
	KeptForCoverage  bool          `json:"kept_for_coverage"`
	EmailSent        bool          `json:"email_sent"`
	ConsecutiveFails int           `json:"consecutive_fails"`
	CycleStopped     bool          `json:"cycle_stopped"`
	Error            string        `json:"error,omitempty"`
	Samples          []ProbeSample `json:"samples,omitempty"`
}

// ProbeSample 是一轮探测里单次测号的提问和回答。
type ProbeSample struct {
	Name   string `json:"name"`
	Prompt string `json:"prompt"`
	Answer string `json:"answer"`
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
}

// LogItem 是管理端记录列表的一行。
type LogItem struct {
	ProviderID   int64  `json:"provider_id"`
	ProviderName string `json:"provider_name"`
	ProbeLog
}

// ParseStoredState 从提供商 Extra 读取探测状态，缺省时返回零值。
func ParseStoredState(extra map[string]any) StoredState {
	if extra == nil {
		return StoredState{}
	}
	raw, ok := extra[ExtraKey]
	if !ok || raw == nil {
		return StoredState{}
	}
	payload, err := json.Marshal(raw)
	if err != nil {
		return StoredState{}
	}
	var state StoredState
	if err := json.Unmarshal(payload, &state); err != nil {
		return StoredState{}
	}
	return state
}

func appendProbeLog(history []ProbeLog, entry ProbeLog) []ProbeLog {
	entry.Samples = clipSamples(entry.Samples)
	out := make([]ProbeLog, 0, len(history)+1)
	out = append(out, entry)
	out = append(out, history...)
	if len(out) > MaxProbeHistory {
		out = out[:MaxProbeHistory]
	}
	return out
}

func clipSamples(samples []ProbeSample) []ProbeSample {
	if len(samples) == 0 {
		return samples
	}
	out := make([]ProbeSample, len(samples))
	copy(out, samples)
	for i := range out {
		out[i].Prompt = clipSampleText(out[i].Prompt)
		out[i].Answer = clipSampleText(out[i].Answer)
		out[i].Error = clipSampleText(out[i].Error)
	}
	return out
}

func clipSampleText(text string) string {
	runes := []rune(text)
	if len(runes) <= MaxSampleChars {
		return text
	}
	return string(runes[:MaxSampleChars]) + "…"
}

func shouldRecordLog(skipReason string) bool {
	switch skipReason {
	case "not_due", "cycle_stopped", "group", "account":
		return false
	default:
		return true
	}
}

func (s StoredState) cycle() CycleState {
	return CycleState{
		ConsecutiveFails: s.ConsecutiveFails,
		CycleStopped:     s.CycleStopped,
		NextRetryAt:      s.NextRetryAt,
	}
}

func (s StoredState) extraUpdate() map[string]any {
	return map[string]any{ExtraKey: s}
}
