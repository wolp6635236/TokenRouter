package qualityprobe

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// engine.go runs provider quality probes and applies their results to account
// scheduling, retry cycles, history, and notifications. Automatic probes filter
// the configured groups and models before issuing requests. Each selected
// model records its own evidence, while the account receives one decision
// after all selected model requests finish.
//
// Probe slots limit overlapping requests for each provider. A separate provider
// lock serializes state updates so concurrent manual and automatic probes
// append to the latest history. Completed probe evidence is persisted with a
// context that survives request cancellation, allowing an administrator to
// close the result dialog while the server finishes recording the round.

// autoProbeConcurrency 是自动探测同时跑的提供商数。
// 每个提供商内部仍是糖果题和一道 ModelTrace 依次打。再高主要叠加上游连接和 token，不是本机 goroutine。
const autoProbeConcurrency = 4

// modelProbeRound keeps one model's completed probe evidence until the account
// decision can evaluate every model selected for the same automatic cycle.
type modelProbeRound struct {
	Model string
	Round ProbeRound
}

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
	if trigger == TriggerAuto {
		models := e.autoProbeModels(cfg, catalog)
		if len(models) == 0 {
			return e.finishSkip(ctx, snap, state, trigger, "model")
		}
		if len(models) == 1 {
			model := models[0]
			release, slotErr := e.acquireProbeSlot(ctx, providerID)
			if slotErr != nil {
				return report, slotErr
			}
			defer release()
			return e.applyProbedRound(ctx, cfg, providerID, trigger, model, e.probeRound(ctx, providerID, model))
		}
		return e.runAutomaticModels(ctx, cfg, providerID, models)
	}
	model := ResolveProbeModels(cfg.Models, catalog)[0]
	if manualModel != "" {
		model = manualModel
	}
	report.Model = model
	release, slotErr := e.acquireProbeSlot(ctx, providerID)
	if slotErr != nil {
		return report, slotErr
	}
	defer release()
	round := e.probeRound(ctx, providerID, model)
	return e.applyProbedRound(ctx, cfg, providerID, trigger, model, round)
}

// autoProbeModels filters configured probe models through this provider's
// catalog. A missing model is skipped, preserving the prior eligibility rule.
func (e *Engine) autoProbeModels(cfg Settings, catalog []string) []string {
	configured := ResolveProbeModels(cfg.Models, catalog)
	models := make([]string, 0, len(configured))
	for _, model := range configured {
		if e.Catalog != nil && !CatalogContainsModel(catalog, model) {
			continue
		}
		models = append(models, model)
	}
	return models
}

// ModelCandidates returns the deduplicated requestable models from active
// selected groups. An empty group selection uses every active group, matching
// automatic probe scope before individual provider catalogs apply their skips.
func (e *Engine) ModelCandidates(ctx context.Context, selected []int64) ([]string, error) {
	if e == nil || e.Groups == nil || e.GroupModels == nil {
		return []string{}, nil
	}
	active, err := e.Groups.ActiveIDs(ctx)
	if err != nil {
		return nil, err
	}
	selectedSet := make(map[int64]struct{}, len(selected))
	for _, groupID := range NormalizeGroupIDs(selected) {
		selectedSet[groupID] = struct{}{}
	}
	models := make([]string, 0)
	seen := make(map[string]struct{})
	for _, groupID := range active {
		if len(selectedSet) > 0 {
			if _, selected := selectedSet[groupID]; !selected {
				continue
			}
		}
		candidates, err := e.GroupModels.Models(ctx, groupID)
		if err != nil {
			return nil, err
		}
		for _, model := range candidates {
			model = strings.TrimSpace(model)
			if model == "" {
				continue
			}
			if _, exists := seen[model]; exists {
				continue
			}
			seen[model] = struct{}{}
			models = append(models, model)
		}
	}
	sort.Strings(models)
	return models, nil
}

// runAutomaticModels probes every eligible configured model concurrently, then
// applies one account decision. This prevents goroutine completion order from
// changing consecutive failures or temporary unscheduling.
func (e *Engine) runAutomaticModels(ctx context.Context, cfg Settings, providerID int64, models []string) (RunReport, error) {
	rounds := make([]modelProbeRound, len(models))
	var wg sync.WaitGroup
	for index, model := range models {
		wg.Add(1)
		go func(index int, model string) {
			defer wg.Done()
			release, err := e.acquireProbeSlot(ctx, providerID)
			if err != nil {
				rounds[index] = modelProbeRound{Model: model, Round: probeFailureRound(err)}
				return
			}
			defer release()
			rounds[index] = modelProbeRound{Model: model, Round: e.probeRound(ctx, providerID, model)}
		}(index, model)
	}
	wg.Wait()
	return e.applyProbedRounds(ctx, cfg, providerID, TriggerAuto, rounds)
}

// probeFailureRound converts a slot or deadline failure into upstream evidence
// so the account preserves its prior failure cycle instead of being degraded.
func probeFailureRound(err error) ProbeRound {
	if err == nil {
		return ProbeRound{}
	}
	return ProbeRound{CandyError: err.Error(), ModelTraceError: err.Error()}
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
	unlock := e.lockProvider(snap.ID)
	defer unlock()
	writeCtx := persistCtx(ctx)
	if fresh, err := e.Dir.Get(writeCtx, snap.ID); err == nil && fresh != nil {
		snap = fresh
		state = ParseStoredState(fresh.Extra)
	}
	return e.recordSkip(writeCtx, snap, state, trigger, reason)
}

func (e *Engine) recordSkip(ctx context.Context, snap *Snapshot, state StoredState, trigger Trigger, reason string) (RunReport, error) {
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
	if err := e.Dir.UpdateExtra(ctx, snap.ID, state.extraUpdate()); err != nil {
		return report, err
	}
	return report, nil
}

func (e *Engine) applyProbedRound(
	ctx context.Context,
	cfg Settings,
	providerID int64,
	trigger Trigger,
	model string,
	round ProbeRound,
) (RunReport, error) {
	unlock := e.lockProvider(providerID)
	defer unlock()
	writeCtx := persistCtx(ctx)
	snap, err := e.Dir.Get(writeCtx, providerID)
	if err != nil {
		return RunReport{ProviderID: providerID, Model: model}, err
	}
	if snap == nil {
		return RunReport{ProviderID: providerID, Model: model}, fmt.Errorf("provider %d is missing", providerID)
	}
	state := ParseStoredState(snap.Extra)
	now := e.now()
	held := e.lastSchedulableHeld(writeCtx, snap)
	decision := Decide(DecideInput{
		Settings:            cfg,
		Platform:            snap.Platform,
		LastSchedulableHeld: held,
		State:               state.cycle(),
		Round:               round,
		Trigger:             trigger,
	})
	if decision.Skip {
		return e.recordSkip(writeCtx, snap, state, trigger, "decide")
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
	if decision.ClearTemp {
		if strings.TrimSpace(snap.TempUnschedulableReason) == "" ||
			snap.TempUnschedulableReason == TempUnscheduleReason {
			if err := e.Dir.ClearTempUnschedulable(writeCtx, providerID); err != nil {
				return RunReport{ProviderID: providerID, Model: model}, err
			}
		}
	}
	if decision.TempUnschedule {
		until := now.Add(decision.Cooldown)
		if err := e.Dir.SetTempUnschedulable(writeCtx, providerID, until, TempUnscheduleReason); err != nil {
			return RunReport{ProviderID: providerID, Model: model}, err
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
		return RunReport{ProviderID: providerID, Model: model}, err
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

// applyProbedRounds records every automatic model result, then derives one
// account decision. A provider degrades only when all successfully probed
// configured models degrade; any pass clears the previous failure cycle.
func (e *Engine) applyProbedRounds(
	ctx context.Context,
	cfg Settings,
	providerID int64,
	trigger Trigger,
	rounds []modelProbeRound,
) (RunReport, error) {
	unlock := e.lockProvider(providerID)
	defer unlock()
	writeCtx := persistCtx(ctx)
	snap, err := e.Dir.Get(writeCtx, providerID)
	if err != nil {
		return RunReport{ProviderID: providerID}, err
	}
	if snap == nil {
		return RunReport{ProviderID: providerID}, fmt.Errorf("provider %d is missing", providerID)
	}
	state := ParseStoredState(snap.Extra)
	accountRound := aggregateProbeRounds(rounds)
	now := e.now()
	held := e.lastSchedulableHeld(writeCtx, snap)
	decision := Decide(DecideInput{
		Settings:            cfg,
		Platform:            snap.Platform,
		LastSchedulableHeld: held,
		State:               state.cycle(),
		Round:               accountRound,
		Trigger:             trigger,
	})
	if decision.Skip {
		return e.recordSkip(writeCtx, snap, state, trigger, "decide")
	}
	stored := StoredState{
		ConsecutiveFails:  decision.ConsecutiveFails,
		CycleStopped:      decision.StopCycle,
		NextRetryAt:       nextRetry(now, cfg, decision),
		LastCandyOK:       accountRound.CandyOK,
		LastTraceOK:       accountRound.ModelTraceOK,
		LastUpstreamError: decision.UpstreamError,
		LastError:         firstError(accountRound),
		LastModel:         strings.Join(probeRoundModels(rounds), ", "),
		UpdatedAt:         now,
	}
	if decision.ClearTemp {
		if strings.TrimSpace(snap.TempUnschedulableReason) == "" ||
			snap.TempUnschedulableReason == TempUnscheduleReason {
			if err := e.Dir.ClearTempUnschedulable(writeCtx, providerID); err != nil {
				return RunReport{ProviderID: providerID}, err
			}
		}
	}
	if decision.TempUnschedule {
		until := now.Add(decision.Cooldown)
		if err := e.Dir.SetTempUnschedulable(writeCtx, providerID, until, TempUnscheduleReason); err != nil {
			return RunReport{ProviderID: providerID}, err
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
	stored.History = state.History
	for _, item := range slices.Backward(rounds) {
		stored.History = appendProbeLog(stored.History, ProbeLog{
			At:               now,
			Trigger:          trigger,
			Model:            item.Model,
			CandyOK:          item.Round.CandyOK,
			TraceOK:          item.Round.ModelTraceOK,
			TracePrediction:  item.Round.TracePrediction,
			TraceProbability: item.Round.TraceProbability,
			Degraded:         IsDegraded(item.Round),
			UpstreamError:    HasUpstreamError(item.Round),
			TempUnscheduled:  decision.TempUnschedule,
			KeptForCoverage:  decision.SkipBecauseLastInGroup,
			EmailSent:        emailSent,
			ConsecutiveFails: decision.ConsecutiveFails,
			CycleStopped:     decision.StopCycle,
			Error:            firstError(item.Round),
			Samples:          item.Round.Samples,
		})
	}
	if err := e.Dir.UpdateExtra(writeCtx, providerID, stored.extraUpdate()); err != nil {
		return RunReport{ProviderID: providerID}, err
	}
	return RunReport{
		ProviderID:       providerID,
		ProviderName:     snap.Name,
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
	}, nil
}

// probeRoundModels returns configured-order labels for account-level history
// and notification text after every model round has finished.
func probeRoundModels(rounds []modelProbeRound) []string {
	models := make([]string, 0, len(rounds))
	for _, item := range rounds {
		models = append(models, item.Model)
	}
	return models
}

// aggregateProbeRounds combines completed automatic model evidence for one
// account. Any passing model clears degradation, including when another model
// reports an upstream error. This priority applies to every configured model
// order because account recovery depends on finding one usable model.
//
// When no model passes, the first upstream error keeps the existing failure
// cycle unchanged. All degraded rounds produce a degraded account result;
// callers skip accounts with no eligible models before collecting these rounds.
func aggregateProbeRounds(rounds []modelProbeRound) ProbeRound {
	var result ProbeRound
	for _, item := range rounds {
		if HasUpstreamError(item.Round) {
			// Keep error evidence while checking later models: a usable model
			// clears the account even when another model cannot be tested.
			if !HasUpstreamError(result) {
				result = ProbeRound{CandyError: firstError(item.Round), ModelTraceError: firstError(item.Round)}
			}
			continue
		}
		if !IsDegraded(item.Round) {
			return ProbeRound{CandyOK: true, ModelTraceOK: true}
		}
	}
	return result
}

func (e *Engine) lockProvider(providerID int64) func() {
	if e == nil {
		return func() {}
	}
	actual, _ := e.applyMu.LoadOrStore(providerID, &sync.Mutex{})
	mu, ok := actual.(*sync.Mutex)
	if !ok || mu == nil {
		return func() {}
	}
	mu.Lock()
	return mu.Unlock
}

func (e *Engine) acquireProbeSlot(ctx context.Context, providerID int64) (func(), error) {
	if e == nil {
		return func() {}, nil
	}
	// 请求取消时这一轮继续跑完。探测超时到期后，新的一轮直接返回超时。
	if ctx != nil && ctx.Err() == context.DeadlineExceeded {
		return nil, ctx.Err()
	}
	actual, _ := e.probeSlots.LoadOrStore(providerID, make(chan struct{}, autoProbeConcurrency))
	slot, ok := actual.(chan struct{})
	if !ok || slot == nil {
		return func() {}, fmt.Errorf("quality probe slot for provider %d is missing", providerID)
	}
	slot <- struct{}{}
	return func() { <-slot }, nil
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
				GroupNames:   append([]string{}, items[i].GroupNames...),
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
	end := min(start+pageSize, total)
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
