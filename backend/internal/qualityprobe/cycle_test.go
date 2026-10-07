package qualityprobe

import (
	"testing"
	"time"
)

func TestDecide_FirstDegradeTempsForFiveMinutes(t *testing.T) {
	cfg := DefaultSettings()
	cfg.Enabled = true
	got := Decide(DecideInput{
		Settings: cfg,
		Platform: PlatformOpenAI,
		State:    CycleState{},
		Round:    ProbeRound{CandyOK: false, ModelTraceOK: false},
		Trigger:  TriggerAuto,
	})
	if !got.TempUnschedule {
		t.Fatal("expected temp unschedulable")
	}
	if got.Cooldown != 5*time.Minute {
		t.Fatalf("cooldown = %s, want 5m", got.Cooldown)
	}
	if got.ConsecutiveFails != 1 {
		t.Fatalf("consecutive = %d, want 1", got.ConsecutiveFails)
	}
	if got.SendEmail {
		t.Fatal("email should wait until the third failure")
	}
	if got.StopCycle {
		t.Fatal("cycle should continue after the first failure")
	}
}

func TestDecide_ThirdDegradeSendsEmailAndStopsCycle(t *testing.T) {
	cfg := DefaultSettings()
	cfg.Enabled = true
	got := Decide(DecideInput{
		Settings: cfg,
		Platform: PlatformOpenAI,
		State: CycleState{
			ConsecutiveFails: 2,
		},
		Round:   ProbeRound{CandyOK: false, ModelTraceOK: false},
		Trigger: TriggerAuto,
	})
	if got.ConsecutiveFails != 3 {
		t.Fatalf("consecutive = %d, want 3", got.ConsecutiveFails)
	}
	if !got.SendEmail {
		t.Fatal("expected email on third failure")
	}
	if !got.StopCycle {
		t.Fatal("expected cycle to stop after third failure")
	}
	if !got.TempUnschedule {
		t.Fatal("provider stays unschedulable after the third failure")
	}
}

func TestDecide_PassClearsFailures(t *testing.T) {
	cfg := DefaultSettings()
	cfg.Enabled = true
	got := Decide(DecideInput{
		Settings: cfg,
		Platform: PlatformOpenAI,
		State: CycleState{
			ConsecutiveFails: 2,
			CycleStopped:     true,
		},
		Round:   ProbeRound{CandyOK: true, ModelTraceOK: true},
		Trigger: TriggerManual,
	})
	if got.ConsecutiveFails != 0 {
		t.Fatalf("consecutive = %d, want 0", got.ConsecutiveFails)
	}
	if !got.ClearTemp {
		t.Fatal("expected temp unschedulable to clear")
	}
	if got.StopCycle {
		t.Fatal("passing probe reopens the cycle")
	}
	if got.SendEmail {
		t.Fatal("passing probe does not send email")
	}
}

func TestDecide_LastSchedulableSkipsTempUnschedule(t *testing.T) {
	cfg := DefaultSettings()
	cfg.Enabled = true
	got := Decide(DecideInput{
		Settings:            cfg,
		Platform:            PlatformOpenAI,
		LastSchedulableHeld: true,
		State:               CycleState{ConsecutiveFails: 2},
		Round:               ProbeRound{CandyOK: false, ModelTraceOK: false},
		Trigger:             TriggerAuto,
	})
	if got.TempUnschedule {
		t.Fatal("last remaining provider in a group stays schedulable")
	}
	if !got.SkipBecauseLastInGroup {
		t.Fatal("expected last-in-group skip")
	}
	if !got.SendEmail {
		t.Fatal("third failure still emails when the provider is kept for coverage")
	}
	if !got.StopCycle {
		t.Fatal("auto cycle still stops after the third failure")
	}
}

func TestDecide_DisabledOrNonOpenAIOrStoppedAutoDoesNothing(t *testing.T) {
	cfg := DefaultSettings()
	round := ProbeRound{CandyOK: false, ModelTraceOK: false}
	cases := []struct {
		name  string
		input DecideInput
	}{
		{
			name: "总开关关闭",
			input: DecideInput{
				Settings: cfg,
				Platform: PlatformOpenAI,
				Round:    round,
				Trigger:  TriggerAuto,
			},
		},
		{
			name: "非 OpenAI",
			input: DecideInput{
				Settings: func() Settings {
					s := DefaultSettings()
					s.Enabled = true
					return s
				}(),
				Platform: "anthropic",
				Round:    round,
				Trigger:  TriggerAuto,
			},
		},
		{
			name: "自动任务在停循环后跳过",
			input: DecideInput{
				Settings: func() Settings {
					s := DefaultSettings()
					s.Enabled = true
					return s
				}(),
				Platform: PlatformOpenAI,
				State:    CycleState{CycleStopped: true, ConsecutiveFails: 3},
				Round:    round,
				Trigger:  TriggerAuto,
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := Decide(tt.input)
			if got.TempUnschedule || got.SendEmail || got.ClearTemp {
				t.Fatalf("unexpected action: %+v", got)
			}
		})
	}
}

func TestDecide_ManualStillRunsAfterCycleStopped(t *testing.T) {
	cfg := DefaultSettings()
	cfg.Enabled = true
	got := Decide(DecideInput{
		Settings: cfg,
		Platform: PlatformOpenAI,
		State:    CycleState{CycleStopped: true, ConsecutiveFails: 3},
		Round:    ProbeRound{CandyOK: false, ModelTraceOK: false},
		Trigger:  TriggerManual,
	})
	if got.Skip {
		t.Fatal("manual trigger still evaluates after the auto cycle stops")
	}
	if got.SendEmail {
		t.Fatal("already notified failures do not send another email")
	}
	if !got.TempUnschedule {
		t.Fatal("manual retest that still fails keeps the provider unschedulable")
	}
}
