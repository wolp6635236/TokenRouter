//go:build unit

package qualityprobe

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParseStoredState_EmptyAndRoundTrip(t *testing.T) {
	if got := ParseStoredState(nil); got.ConsecutiveFails != 0 {
		t.Fatalf("empty extra = %+v", got)
	}
	until := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	src := StoredState{
		ConsecutiveFails: 2,
		CycleStopped:     false,
		NextRetryAt:      &until,
		LastCandyOK:      false,
		LastTraceOK:      true,
		LastModel:        DefaultProbeModel,
		UpdatedAt:        until,
	}
	raw, err := json.Marshal(src)
	if err != nil {
		t.Fatal(err)
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	got := ParseStoredState(map[string]any{ExtraKey: decoded})
	if got.ConsecutiveFails != 2 || got.LastTraceOK != true || got.LastModel != DefaultProbeModel {
		t.Fatalf("parsed = %+v", got)
	}
	if got.NextRetryAt == nil || !got.NextRetryAt.Equal(until) {
		t.Fatalf("next retry = %v", got.NextRetryAt)
	}
}
