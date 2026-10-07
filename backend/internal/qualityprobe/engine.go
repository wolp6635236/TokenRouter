package qualityprobe

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// autoProbeConcurrency 是自动探测同时跑的提供商数。
// 每个提供商内部仍是糖果题和一道 ModelTrace 依次打。再高主要叠加上游连接和 token，不是本机 goroutine。
const autoProbeConcurrency = 4

// Run 对单个提供商执行一轮探测并应用决策。
func (e *Engine) Run(ctx context.Context, providerID int64, trigger Trigger) (RunReport, error) {
	return e.RunWithModel(ctx, providerID, trigger, "")
}

// RunWithModel 执行一轮探测。manualModel 非空且 trigger 为手动时，用该模型，不用设置里的探测模型。
func (e *Engine) RunWithModel(ctx context.Context, providerID int64, trigger Trigger, manualModel string) (RunReport, error) {
	return e.runWithActive(ctx, providerID, trigger, nil, strings.TrimSpace(manualModel))
}

func (e *Engine) runWithActive(
	ctx context.Context,
	providerID int64,
	trigger Trigger,
	active map[int64]struct{},
	manualModel string,
) (RunReport, error) {
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
		return e.finishSkip(ctx, snap, state, trigger, "disabled")
	}
	now := e.now()
	if trigger == TriggerAuto && !AutoScheduleActive(cfg, now) {
		return e.finishSkip(ctx, snap, state, trigger, "schedule")
	}
	if snap.Platform != PlatformOpenAI {
		return e.finishSkip(ctx, snap, state, trigger, "platform")
	}
	if trigger == TriggerAuto && !AccountAutoEligible(snap) {
		return e.finishSkip(ctx, snap, state, trigger, "account")
	}
	if trigger == TriggerAuto {
		if active == nil {
			loaded, groupErr := e.activeGroupSet(ctx)
			if groupErr != nil {
				return report, groupErr
			}
			active = loaded
		}
		if !AutoScopeAllowed(cfg.GroupIDs, snap.GroupIDs, active) {
			return e.finishSkip(ctx, snap, state, trigger, "group")
		}
	}
	if trigger == TriggerAuto && state.CycleStopped {
		return e.finishSkip(ctx, snap, state, trigger, "cycle_stopped")
	}
	if trigger == TriggerAuto && !due(state, now) {
		return e.finishSkip(ctx, snap, state, trigger, "not_due")
	}
	catalog := e.catalogModels(ctx, snap)
	model := ResolveProbeModel(cfg.Model, catalog)
	if trigger == TriggerManual && manualModel != "" {
		model = manualModel
	}
	report.Model = model
	if trigger == TriggerAuto && e.Catalog != nil && !CatalogContainsModel(catalog, model) {
		return e.finishSkip(ctx, snap, state, trigger, "model")
	}
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
		return e.finishSkip(ctx, snap, state, trigger, "decide")
	}
	next := nextRetry(now, cfg, decision)
	stored := StoredState{
		ConsecutiveFails:  decision.ConsecutiveFails,
		CycleStopped:      decision.StopCycle,
		NextRetryAt:       next,
		LastCandyOK:       round.CandyOK,
		LastTraceOK:       round.ModelTraceOK,
		LastUpstreamError: decision.UpstreamError,
		LastError:         firstError(round),
		LastModel:         model,
		UpdatedAt:         now,
	}
	writeCtx := persistCtx(ctx)
	if decision.ClearTemp {
		if strings.TrimSpace(snap.TempUnschedulableReason) == "" ||
			snap.TempUnschedulableReason == TempUnscheduleReason {
			if err := e.Dir.ClearTempUnschedulable(writeCtx, providerID); err != nil {
				return report, err
			}
		}
	}
	if decision.TempUnschedule {
		until := now.Add(decision.Cooldown)
		if err := e.Dir.SetTempUnschedulable(writeCtx, providerID, until, TempUnscheduleReason); err != nil {
			return report, err
		}
	}
	emailSent := false
	if decision.SendEmail && e.Mail != nil {
		subject, body := emailCopy(snap, stored)
		if err := e.Mail.Send(ctx, cfg.NotifyEmail, subject, body); err != nil {
			stored.LastError = joinErrors(stored.LastError, err.Error())
		} else {
			emailSent = true
		}
	}
	stored.History = appendProbeLog(state.History, ProbeLog{
		At:               now,
		Trigger:          trigger,
		Model:            model,
		CandyOK:          round.CandyOK,
		TraceOK:          round.ModelTraceOK,
		TracePrediction:  round.TracePrediction,
		TraceProbability: round.TraceProbability,
		Degraded:         decision.Degraded,
		UpstreamError:    decision.UpstreamError,
		TempUnscheduled:  decision.TempUnschedule,
		KeptForCoverage:  decision.SkipBecauseLastInGroup,
		EmailSent:        emailSent,
		ConsecutiveFails: decision.ConsecutiveFails,
		CycleStopped:     decision.StopCycle,
		Error:            stored.LastError,
		Samples:          round.Samples,
	})
	if err := e.Dir.UpdateExtra(writeCtx, providerID, stored.extraUpdate()); err != nil {
		return report, err
	}
	return RunReport{
		ProviderID:       providerID,
		ProviderName:     snap.Name,
		Model:            model,
		CandyOK:          round.CandyOK,
		TraceOK:          round.ModelTraceOK,
		TracePrediction:  round.TracePrediction,
		TraceProbability: round.TraceProbability,
		Degraded:         decision.Degraded,
		UpstreamError:    decision.UpstreamError,
		TempUnscheduled:  decision.TempUnschedule,
		KeptForCoverage:  decision.SkipBecauseLastInGroup,
		EmailSent:        emailSent,
		ConsecutiveFails: decision.ConsecutiveFails,
		CycleStopped:     decision.StopCycle,
		Error:            stored.LastError,
		Trigger:          trigger,
		At:               now,
		Samples:          round.Samples,
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
	if !AutoScheduleActive(cfg, e.now()) {
		return
	}
	items, err := e.Dir.ListByPlatform(ctx, PlatformOpenAI)
	if err != nil {
		return
	}
	active, err := e.activeGroupSet(ctx)
	if err != nil {
		return
	}
	sem := make(chan struct{}, autoProbeConcurrency)
	var wg sync.WaitGroup
loop:
	for i := range items {
		id := items[i].ID
		select {
		case <-ctx.Done():
			break loop
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(providerID int64) {
			defer wg.Done()
			defer func() { <-sem }()
			_, _ = e.runWithActive(ctx, providerID, TriggerAuto, active, "")
		}(id)
	}
	wg.Wait()
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

func (e *Engine) finishSkip(ctx context.Context, snap *Snapshot, state StoredState, trigger Trigger, reason string) (RunReport, error) {
	report := RunReport{
		ProviderID:       snap.ID,
		ProviderName:     snap.Name,
		Skipped:          true,
		SkipReason:       reason,
		ConsecutiveFails: state.ConsecutiveFails,
		CycleStopped:     state.CycleStopped,
		Trigger:          trigger,
		At:               e.now(),
	}
	if !shouldRecordLog(reason) {
		return report, nil
	}
	state.History = appendProbeLog(state.History, ProbeLog{
		At:               report.At,
		Trigger:          trigger,
		Skipped:          true,
		SkipReason:       reason,
		ConsecutiveFails: state.ConsecutiveFails,
		CycleStopped:     state.CycleStopped,
	})
	state.UpdatedAt = report.At
	if err := e.Dir.UpdateExtra(persistCtx(ctx), snap.ID, state.extraUpdate()); err != nil {
		return report, err
	}
	return report, nil
}

// ListLogs 汇总 OpenAI 提供商 Extra 里的探测记录，按时间倒序分页。
func (e *Engine) ListLogs(ctx context.Context, page, pageSize int) ([]LogItem, int, error) {
	if e == nil || e.Dir == nil {
		return nil, 0, fmt.Errorf("quality probe engine is incomplete")
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	items, err := e.Dir.ListByPlatform(ctx, PlatformOpenAI)
	if err != nil {
		return nil, 0, err
	}
	out := make([]LogItem, 0)
	for i := range items {
		state := ParseStoredState(items[i].Extra)
		for _, entry := range state.History {
			out = append(out, LogItem{
				ProviderID:   items[i].ID,
				ProviderName: items[i].Name,
				ProbeLog:     entry,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].At.Equal(out[j].At) {
			return out[i].ProviderID > out[j].ProviderID
		}
		return out[i].At.After(out[j].At)
	})
	total := len(out)
	start := (page - 1) * pageSize
	if start >= total {
		return []LogItem{}, total, nil
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return append([]LogItem(nil), out[start:end]...), total, nil
}

func (e *Engine) activeGroupSet(ctx context.Context) (map[int64]struct{}, error) {
	if e == nil || e.Groups == nil {
		return nil, nil
	}
	ids, err := e.Groups.ActiveIDs(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		out[id] = struct{}{}
	}
	return out, nil
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
	candyPrompt := CandyPrompt()
	candy, err := e.Prober.ProbeText(ctx, providerID, model, candyPrompt)
	candySample := ProbeSample{Name: "candy", Prompt: candyPrompt}
	if err != nil {
		round.CandyError = err.Error()
		candySample.Error = err.Error()
	} else {
		round.CandyError = candy.Error
		round.CandyOK = HasStandalone21(candy.Answer)
		candySample.Answer = candy.Answer
		candySample.Error = candy.Error
		candySample.OK = round.CandyOK
	}
	round.Samples = append(round.Samples, candySample)
	challenges := BuildTraceChallenges(nil)
	outputs := make([]TraceOutput, 0, len(challenges))
	var traceErrs []string
	for i, challenge := range challenges {
		result, probeErr := e.Prober.ProbeText(ctx, providerID, model, challenge.Prompt)
		sample := ProbeSample{
			Name:   fmt.Sprintf("trace_%d", i+1),
			Prompt: challenge.Prompt,
		}
		if probeErr != nil {
			traceErrs = append(traceErrs, probeErr.Error())
			sample.Error = probeErr.Error()
			round.Samples = append(round.Samples, sample)
			continue
		}
		if result.Error != "" {
			traceErrs = append(traceErrs, result.Error)
		}
		sample.Answer = result.Answer
		sample.Error = result.Error
		if TraceSampleValid(result.Answer, challenge.ExpectedCount) {
			sample.OK = true
		}
		outputs = append(outputs, TraceOutput{
			Text:          result.Answer,
			ExpectedCount: challenge.ExpectedCount,
		})
		round.Samples = append(round.Samples, sample)
	}
	attr, attrErr := e.analyzeTrace(outputs)
	if attrErr != nil {
		traceErrs = append(traceErrs, attrErr.Error())
	} else {
		round.TracePrediction = attr.Prediction
		round.TraceProbability = attr.Probability
		round.ModelTraceOK = FingerprintMatchesRequested(model, attr.Prediction)
	}
	round.ModelTraceError = strings.Join(traceErrs, "; ")
	return round
}

func (e *Engine) analyzeTrace(outputs []TraceOutput) (TraceResult, error) {
	if e != nil && e.AnalyzeTrace != nil {
		return e.AnalyzeTrace(outputs)
	}
	return AnalyzeTraceOutputs(outputs)
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
