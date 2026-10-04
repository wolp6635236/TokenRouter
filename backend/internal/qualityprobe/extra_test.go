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

func TestAppendProbeLogCapsHistory(t *testing.T) {
	var history []ProbeLog
	for i := 0; i < MaxProbeHistory+5; i++ {
		history = appendProbeLog(history, ProbeLog{Trigger: TriggerManual, ConsecutiveFails: i})
	}
	if len(history) != MaxProbeHistory {
		t.Fatalf("len = %d", len(history))
	}
	if history[0].ConsecutiveFails != MaxProbeHistory+4 {
		t.Fatalf("newest = %+v", history[0])
	}
}

func TestClipSampleText(t *testing.T) {
	short := clipSampleText("ok")
	if short != "ok" {
		t.Fatalf("short = %q", short)
	}
	runes := make([]rune, MaxSampleChars+3)
	for i := range runes {
		runes[i] = 'a'
	}
	got := clipSampleText(string(runes))
	if []rune(got)[len([]rune(got))-1] != '…' {
		t.Fatalf("clipped = %q", got[len(got)-8:])
	}
	if len([]rune(got)) != MaxSampleChars+1 {
		t.Fatalf("len = %d", len([]rune(got)))
	}
}
