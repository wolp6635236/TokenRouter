package qualityprobe

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Run 对单个提供商执行一轮探测并应用决策。
func (e *Engine) Run(ctx context.Context, providerID int64, trigger Trigger) (RunReport, error) {
	report := RunReport{ProviderID: providerID}
	if e == nil || e.Dir == nil {
		return report, fmt.Errorf("quality probe engine is incomplete")
	}
	cfg, err := e.LoadSettings(ctx)
	if err != nil {
		return report, err
	}
	snap, err := e.Dir.Get(ctx, providerID)
	if err != nil {
		return report, err
	}
	if snap == nil {
		return report, fmt.Errorf("provider %d is missing", providerID)
	}
	state := ParseStoredState(snap.Extra)
	if !cfg.Enabled {
		return RunReport{ProviderID: providerID, Skipped: true, SkipReason: "disabled"}, nil
	}
	if snap.Platform != PlatformOpenAI {
		return RunReport{ProviderID: providerID, Skipped: true, SkipReason: "platform"}, nil
	}
	now := e.now()
	if trigger == TriggerAuto && state.CycleStopped {
		return RunReport{
			ProviderID:       providerID,
			Skipped:          true,
			SkipReason:       "cycle_stopped",
			ConsecutiveFails: state.ConsecutiveFails,
			CycleStopped:     true,
		}, nil
	}
	if trigger == TriggerAuto && !due(state, now) {
		return RunReport{
			ProviderID:       providerID,
			Skipped:          true,
			SkipReason:       "not_due",
			ConsecutiveFails: state.ConsecutiveFails,
			CycleStopped:     state.CycleStopped,
		}, nil
	}
	model := ResolveProbeModel(cfg.Model, e.catalogModels(ctx, snap))
	report.Model = model
	round := e.probeRound(ctx, providerID, model)
	held := e.lastSchedulableHeld(ctx, snap)
	decision := Decide(DecideInput{
		Settings:            cfg,
		Platform:            snap.Platform,
		LastSchedulableHeld: held,
		State:               state.cycle(),
		Round:               round,
		Trigger:             trigger,
	})
	if decision.Skip {
		return RunReport{
			ProviderID:       providerID,
			Skipped:          true,
			SkipReason:       "decide",
			ConsecutiveFails: decision.ConsecutiveFails,
			CycleStopped:     decision.StopCycle,
			Error:            firstError(round),
		}, nil
	}
	next := nextRetry(now, cfg, decision)
	stored := StoredState{
		ConsecutiveFails: decision.ConsecutiveFails,
		CycleStopped:     decision.StopCycle,
		NextRetryAt:      next,
		LastCandyOK:      round.CandyOK,
		LastTraceOK:      round.ModelTraceOK,
		LastError:        firstError(round),
		LastModel:        model,
		UpdatedAt:        now,
	}
	if err := e.Dir.UpdateExtra(ctx, providerID, stored.extraUpdate()); err != nil {
		return report, err
	}
	if decision.ClearTemp {
		if strings.TrimSpace(snap.TempUnschedulableReason) == "" ||
			snap.TempUnschedulableReason == TempUnscheduleReason {
			if err := e.Dir.ClearTempUnschedulable(ctx, providerID); err != nil {
				return report, err
			}
		}
	}
	if decision.TempUnschedule {
		until := now.Add(decision.Cooldown)
		if err := e.Dir.SetTempUnschedulable(ctx, providerID, until, TempUnscheduleReason); err != nil {
			return report, err
		}
	}
	emailSent := false
	if decision.SendEmail && e.Mail != nil {
		subject, body := emailCopy(snap, stored)
		if err := e.Mail.Send(ctx, cfg.NotifyEmail, subject, body); err != nil {
			stored.LastError = joinErrors(stored.LastError, err.Error())
			_ = e.Dir.UpdateExtra(ctx, providerID, stored.extraUpdate())
		} else {
			emailSent = true
		}
	}
	return RunReport{
		ProviderID:       providerID,
		Model:            model,
		CandyOK:          round.CandyOK,
		TraceOK:          round.ModelTraceOK,
		Degraded:         decision.Degraded,
		TempUnscheduled:  decision.TempUnschedule,
		KeptForCoverage:  decision.SkipBecauseLastInGroup,
		EmailSent:        emailSent,
		ConsecutiveFails: decision.ConsecutiveFails,
		CycleStopped:     decision.StopCycle,
		Error:            stored.LastError,
	}, nil
}

// RunDue 扫描 OpenAI 提供商并执行到期的自动探测。
func (e *Engine) RunDue(ctx context.Context) {
	if e == nil || e.Dir == nil {
		return
	}
	cfg, err := e.LoadSettings(ctx)
	if err != nil || !cfg.Enabled {
		return
	}
	items, err := e.Dir.ListByPlatform(ctx, PlatformOpenAI)
	if err != nil {
		return
	}
	for i := range items {
		if ctx.Err() != nil {
			return
		}
		_, _ = e.Run(ctx, items[i].ID, TriggerAuto)
	}
}

// Status 返回提供商 Extra 里的探测状态。
func (e *Engine) Status(ctx context.Context, providerID int64) (StoredState, error) {
	if e == nil || e.Dir == nil {
		return StoredState{}, fmt.Errorf("quality probe engine is incomplete")
	}
	snap, err := e.Dir.Get(ctx, providerID)
	if err != nil {
		return StoredState{}, err
	}
	if snap == nil {
		return StoredState{}, fmt.Errorf("provider %d is missing", providerID)
	}
	return ParseStoredState(snap.Extra), nil
}

func due(state StoredState, now time.Time) bool {
	if state.NextRetryAt == nil {
		return true
	}
	return !now.Before(*state.NextRetryAt)
}

func nextRetry(now time.Time, cfg Settings, decision Decision) *time.Time {
	if decision.StopCycle {
		return nil
	}
	wait := cfg.Interval
	if decision.Degraded {
		wait = decision.Cooldown
	}
	if wait <= 0 {
		wait = cfg.Interval
	}
	at := now.Add(wait)
	return &at
}

func (e *Engine) catalogModels(ctx context.Context, snap *Snapshot) []string {
	if e.Catalog == nil || snap == nil {
		return nil
	}
	return e.Catalog.Models(ctx, snap)
}

func (e *Engine) lastSchedulableHeld(ctx context.Context, snap *Snapshot) bool {
	if snap == nil {
		return false
	}
	for _, groupID := range snap.GroupIDs {
		ids, err := e.Dir.SchedulableIDs(ctx, groupID)
		if err != nil {
			continue
		}
		if len(ids) == 1 && ids[0] == snap.ID {
			return true
		}
		if len(ids) == 0 && snap.Schedulable {
			return true
		}
	}
	return false
}

func (e *Engine) probeRound(ctx context.Context, providerID int64, model string) ProbeRound {
	round := ProbeRound{}
	if e.Prober == nil {
		round.CandyError = "prober is missing"
		round.ModelTraceError = "prober is missing"
		return round
	}
	candy, err := e.Prober.ProbeText(ctx, providerID, model, CandyPrompt())
	if err != nil {
		round.CandyError = err.Error()
	} else {
		round.CandyError = candy.Error
		round.CandyOK = HasStandalone21(candy.Answer)
	}
	challenges := BuildTraceChallenges(nil)
	okCount := 0
	var traceErrs []string
	for _, challenge := range challenges {
		result, probeErr := e.Prober.ProbeText(ctx, providerID, model, challenge.Prompt)
		if probeErr != nil {
			traceErrs = append(traceErrs, probeErr.Error())
			continue
		}
		if result.Error != "" {
			traceErrs = append(traceErrs, result.Error)
		}
		if TraceSampleValid(result.Answer, challenge.ExpectedCount) {
			okCount++
		}
	}
	round.ModelTraceOK = okCount == len(challenges)
	round.ModelTraceError = strings.Join(traceErrs, "; ")
	return round
}

func firstError(round ProbeRound) string {
	if round.CandyError != "" {
		return round.CandyError
	}
	return round.ModelTraceError
}

func joinErrors(existing, next string) string {
	existing = strings.TrimSpace(existing)
	next = strings.TrimSpace(next)
	if existing == "" {
		return next
	}
	if next == "" {
		return existing
	}
	return existing + "; " + next
}

func emailCopy(snap *Snapshot, state StoredState) (string, string) {
	name := "unknown"
	id := int64(0)
	if snap != nil {
		name = snap.Name
		id = snap.ID
	}
	subject := fmt.Sprintf("TokenRouter 降智探测：%s 连续 3 次未通过", name)
	kept := ""
	if state.CycleStopped {
		kept = "自动循环已停止。"
	}
	body := fmt.Sprintf(
		"提供商 %s（ID %d）的糖果题和 ModelTrace 连续 %d 次都未通过。%s\n模型：%s\n错误：%s\n",
		name,
		id,
		state.ConsecutiveFails,
		kept,
		state.LastModel,
		state.LastError,
	)
	return subject, body
}
