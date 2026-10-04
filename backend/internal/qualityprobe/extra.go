package qualityprobe

import (
	"encoding/json"
	"time"
)

const (
	// ExtraKey 是提供商 Extra 里保存探测状态的键。
	ExtraKey = "quality_probe"
	// TempUnscheduleReason 写入临时停调原因，恢复时按该值识别。
	TempUnscheduleReason = "quality_degraded"
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
