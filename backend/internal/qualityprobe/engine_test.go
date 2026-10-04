//go:build unit

package qualityprobe

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type memSettings struct {
	raw string
	err error
}

func (m *memSettings) GetValue(context.Context, string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.raw, nil
}

func (m *memSettings) Set(_ context.Context, _ string, value string) error {
	m.raw = value
	return nil
}

type memDir struct {
	items       map[int64]*Snapshot
	schedulable map[int64][]int64
	temp        map[int64]string
	cleared     []int64
}

func (d *memDir) Get(_ context.Context, id int64) (*Snapshot, error) {
	item, ok := d.items[id]
	if !ok {
		return nil, errors.New("missing")
	}
	copyItem := *item
	return &copyItem, nil
}

func (d *memDir) ListByPlatform(_ context.Context, platform string) ([]Snapshot, error) {
	out := make([]Snapshot, 0)
	for _, item := range d.items {
		if item.Platform == platform {
			out = append(out, *item)
		}
	}
	return out, nil
}

func (d *memDir) UpdateExtra(_ context.Context, id int64, updates map[string]any) error {
	item := d.items[id]
	if item.Extra == nil {
		item.Extra = map[string]any{}
	}
	for key, value := range updates {
		item.Extra[key] = value
	}
	return nil
}

func (d *memDir) SetTempUnschedulable(_ context.Context, id int64, _ time.Time, reason string) error {
	if d.temp == nil {
		d.temp = map[int64]string{}
	}
	d.temp[id] = reason
	item := d.items[id]
	item.Schedulable = false
	item.TempUnschedulableReason = reason
	return nil
}

func (d *memDir) ClearTempUnschedulable(_ context.Context, id int64) error {
	d.cleared = append(d.cleared, id)
	delete(d.temp, id)
	item := d.items[id]
	item.Schedulable = true
	item.TempUnschedulableReason = ""
	return nil
}

func (d *memDir) SchedulableIDs(_ context.Context, groupID int64) ([]int64, error) {
	return d.schedulable[groupID], nil
}

type scriptedProber struct {
	answers []string
	index   int
}

func (p *scriptedProber) ProbeText(context.Context, int64, string, string) (TextResult, error) {
	if p.index >= len(p.answers) {
		return TextResult{}, errors.New("no more answers")
	}
	text := p.answers[p.index]
	p.index++
	return TextResult{Answer: text}, nil
}

type recordingMail struct {
	to      string
	subject string
	n       int
}

func (m *recordingMail) Send(_ context.Context, to, subject, _ string) error {
	m.to = to
	m.subject = subject
	m.n++
	return nil
}

func enabledJSON() string {
	return `{"enabled":true,"interval_minutes":30,"model":"gpt-6-astra","cooldown_minutes":5,"max_attempts":3,"notify_email":"295783453@qq.com"}`
}

func numbers(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "8"
	}
	return strings.Join(parts, " ")
}

func TestEngine_PassClearsTempAndSchedulesInterval(t *testing.T) {
	now := time.Date(2026, 10, 4, 8, 0, 0, 0, time.UTC)
	dir := &memDir{
		items: map[int64]*Snapshot{
			7: {
				ID:                      7,
				Name:                    "codex-a",
				Platform:                PlatformOpenAI,
				Schedulable:             true,
				GroupIDs:                []int64{1},
				TempUnschedulableReason: TempUnscheduleReason,
				Extra: map[string]any{
					ExtraKey: map[string]any{"consecutive_fails": 2},
				},
			},
		},
		schedulable: map[int64][]int64{1: {7, 8}},
	}
	engine := &Engine{
		Settings: &memSettings{raw: enabledJSON()},
		Dir:      dir,
		Prober:   &scriptedProber{answers: []string{"answer 21", numbers(400), numbers(400), numbers(400)}},
		Now:      func() time.Time { return now },
	}
	got, err := engine.Run(context.Background(), 7, TriggerAuto)
	if err != nil {
		t.Fatal(err)
	}
	if got.Degraded || !got.CandyOK || !got.TraceOK {
		t.Fatalf("report = %+v", got)
	}
	if len(dir.cleared) != 1 {
		t.Fatalf("cleared = %v", dir.cleared)
	}
	state := ParseStoredState(dir.items[7].Extra)
	if state.ConsecutiveFails != 0 || state.CycleStopped {
		t.Fatalf("state = %+v", state)
	}
	if state.NextRetryAt == nil || !state.NextRetryAt.Equal(now.Add(30*time.Minute)) {
		t.Fatalf("next retry = %v", state.NextRetryAt)
	}
}

func TestEngine_KeepLastDoesNotTempUnschedule(t *testing.T) {
	dir := &memDir{
		items: map[int64]*Snapshot{
			9: {
				ID:          9,
				Name:        "last-one",
				Platform:    PlatformOpenAI,
				Schedulable: true,
				GroupIDs:    []int64{3},
			},
		},
		schedulable: map[int64][]int64{3: {9}},
	}
	mail := &recordingMail{}
	engine := &Engine{
		Settings: &memSettings{raw: enabledJSON()},
		Dir:      dir,
		Prober:   &scriptedProber{answers: []string{"no", "1", "1", "1"}},
		Mail:     mail,
		Now:      func() time.Time { return time.Unix(1, 0) },
	}
	got, err := engine.Run(context.Background(), 9, TriggerAuto)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Degraded || !got.KeptForCoverage || got.TempUnscheduled {
		t.Fatalf("report = %+v", got)
	}
	if dir.temp[9] != "" {
		t.Fatalf("temp reason = %q", dir.temp[9])
	}
}

func TestEngine_ThirdFailEmailsAndStopsAuto(t *testing.T) {
	dir := &memDir{
		items: map[int64]*Snapshot{
			4: {
				ID:       4,
				Name:     "failing",
				Platform: PlatformOpenAI,
				GroupIDs: []int64{2},
				Extra: map[string]any{
					ExtraKey: map[string]any{"consecutive_fails": 2},
				},
			},
		},
		schedulable: map[int64][]int64{2: {4, 5}},
	}
	mail := &recordingMail{}
	engine := &Engine{
		Settings: &memSettings{raw: enabledJSON()},
		Dir:      dir,
		Prober:   &scriptedProber{answers: []string{"x", "1", "1", "1"}},
		Mail:     mail,
		Now:      func() time.Time { return time.Unix(10, 0) },
	}
	got, err := engine.Run(context.Background(), 4, TriggerAuto)
	if err != nil {
		t.Fatal(err)
	}
	if !got.EmailSent || !got.CycleStopped || !got.TempUnscheduled {
		t.Fatalf("report = %+v", got)
	}
	if mail.n != 1 || mail.to != DefaultNotifyEmail {
		t.Fatalf("mail = %+v", mail)
	}
	if !strings.Contains(mail.subject, "failing") {
		t.Fatalf("subject = %s", mail.subject)
	}
	again, err := engine.Run(context.Background(), 4, TriggerAuto)
	if err != nil {
		t.Fatal(err)
	}
	if !again.Skipped || again.SkipReason != "cycle_stopped" {
		t.Fatalf("second auto = %+v", again)
	}
	if mail.n != 1 {
		t.Fatalf("email resent: %d", mail.n)
	}
}

func TestEngine_NotDueSkipsProbe(t *testing.T) {
	until := time.Unix(100, 0)
	dir := &memDir{
		items: map[int64]*Snapshot{
			1: {
				ID:       1,
				Platform: PlatformOpenAI,
				GroupIDs: []int64{1},
				Extra: map[string]any{
					ExtraKey: map[string]any{
						"next_retry_at": until.Format(time.RFC3339),
					},
				},
			},
		},
	}
	prober := &scriptedProber{answers: []string{"21"}}
	engine := &Engine{
		Settings: &memSettings{raw: enabledJSON()},
		Dir:      dir,
		Prober:   prober,
		Now:      func() time.Time { return time.Unix(50, 0) },
	}
	got, err := engine.Run(context.Background(), 1, TriggerAuto)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Skipped || got.SkipReason != "not_due" {
		t.Fatalf("report = %+v", got)
	}
	if prober.index != 0 {
		t.Fatal("probe ran before due")
	}
}

func TestEngine_AutoSkipOutsideGroups(t *testing.T) {
	dir := &memDir{
		items: map[int64]*Snapshot{
			1: {ID: 1, Name: "a", Platform: PlatformOpenAI, GroupIDs: []int64{2}},
		},
	}
	prober := &scriptedProber{answers: []string{"21"}}
	engine := &Engine{
		Settings: &memSettings{raw: `{"enabled":true,"interval_minutes":30,"group_ids":[9],"max_attempts":3,"cooldown_minutes":5,"notify_email":"a@b.c","model":"gpt-6-astra"}`},
		Dir:      dir,
		Prober:   prober,
		Now:      func() time.Time { return time.Unix(1, 0) },
	}
	got, err := engine.Run(context.Background(), 1, TriggerAuto)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Skipped || got.SkipReason != "group" {
		t.Fatalf("report = %+v", got)
	}
	if prober.index != 0 {
		t.Fatal("auto probe ran outside selected groups")
	}
	manual, err := engine.Run(context.Background(), 1, TriggerManual)
	if err != nil {
		t.Fatal(err)
	}
	if manual.Skipped && manual.SkipReason == "group" {
		t.Fatal("manual probe should ignore group filter")
	}
}

func TestEngine_ListLogsOrdersNewestFirst(t *testing.T) {
	now := time.Date(2026, 10, 4, 8, 0, 0, 0, time.UTC)
	dir := &memDir{
		items: map[int64]*Snapshot{
			7: {
				ID:          7,
				Name:        "codex-a",
				Platform:    PlatformOpenAI,
				Schedulable: true,
				GroupIDs:    []int64{1},
			},
		},
		schedulable: map[int64][]int64{1: {7, 8}},
	}
	engine := &Engine{
		Settings: &memSettings{raw: enabledJSON()},
		Prober:   &scriptedProber{answers: []string{"21", numbers(400), numbers(400), numbers(400)}},
		Dir:      dir,
		Now:      func() time.Time { return now },
	}
	if _, err := engine.Run(context.Background(), 7, TriggerManual); err != nil {
		t.Fatal(err)
	}
	logs, err := engine.ListLogs(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].ProviderID != 7 || logs[0].Trigger != TriggerManual {
		t.Fatalf("logs = %+v", logs)
	}
	if len(logs[0].Samples) != 4 || logs[0].Samples[0].Name != "candy" || logs[0].Samples[0].Answer != "21" {
		t.Fatalf("samples = %+v", logs[0].Samples)
	}
}

type memGroups struct {
	ids []int64
}

func (g memGroups) ActiveIDs(context.Context) ([]int64, error) {
	return g.ids, nil
}

func TestEngine_AutoSkipInactiveAccount(t *testing.T) {
	dir := &memDir{
		items: map[int64]*Snapshot{
			1: {ID: 1, Platform: PlatformOpenAI, Status: "error", GroupIDs: []int64{1}},
		},
	}
	prober := &scriptedProber{answers: []string{"21"}}
	engine := &Engine{
		Settings: &memSettings{raw: enabledJSON()},
		Dir:      dir,
		Prober:   prober,
		Now:      func() time.Time { return time.Unix(1, 0) },
	}
	got, err := engine.Run(context.Background(), 1, TriggerAuto)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Skipped || got.SkipReason != "account" {
		t.Fatalf("report = %+v", got)
	}
	if prober.index != 0 {
		t.Fatal("auto probe ran on inactive account")
	}
	manual, err := engine.Run(context.Background(), 1, TriggerManual)
	if err != nil {
		t.Fatal(err)
	}
	if manual.SkipReason == "account" {
		t.Fatal("manual probe should ignore account filter")
	}
}

func TestEngine_AutoSkipSchedulingOff(t *testing.T) {
	dir := &memDir{
		items: map[int64]*Snapshot{
			1: {ID: 1, Platform: PlatformOpenAI, SchedulingOff: true, GroupIDs: []int64{1}},
		},
	}
	prober := &scriptedProber{answers: []string{"21"}}
	engine := &Engine{
		Settings: &memSettings{raw: enabledJSON()},
		Dir:      dir,
		Prober:   prober,
		Now:      func() time.Time { return time.Unix(1, 0) },
	}
	got, err := engine.Run(context.Background(), 1, TriggerAuto)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Skipped || got.SkipReason != "account" {
		t.Fatalf("report = %+v", got)
	}
	if prober.index != 0 {
		t.Fatal("auto probe ran with scheduling off")
	}
}

func TestEngine_AutoSkipDisabledGroup(t *testing.T) {
	dir := &memDir{
		items: map[int64]*Snapshot{
			1: {ID: 1, Platform: PlatformOpenAI, GroupIDs: []int64{8, 9}},
		},
	}
	prober := &scriptedProber{answers: []string{"21"}}
	engine := &Engine{
		Settings: &memSettings{raw: enabledJSON()},
		Dir:      dir,
		Groups:   memGroups{ids: []int64{3}},
		Prober:   prober,
		Now:      func() time.Time { return time.Unix(1, 0) },
	}
	got, err := engine.Run(context.Background(), 1, TriggerAuto)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Skipped || got.SkipReason != "group" {
		t.Fatalf("report = %+v", got)
	}
	if prober.index != 0 {
		t.Fatal("auto probe ran in disabled groups")
	}
}

func TestEngine_AutoRunsWhenOneGroupActive(t *testing.T) {
	dir := &memDir{
		items: map[int64]*Snapshot{
			1: {
				ID:       1,
				Platform: PlatformOpenAI,
				GroupIDs: []int64{8, 9},
			},
		},
		schedulable: map[int64][]int64{8: {1, 2}, 9: {1}},
	}
	engine := &Engine{
		Settings: &memSettings{raw: enabledJSON()},
		Dir:      dir,
		Groups:   memGroups{ids: []int64{9}},
		Prober:   &scriptedProber{answers: []string{"21", numbers(400), numbers(400), numbers(400)}},
		Now:      func() time.Time { return time.Unix(1, 0) },
	}
	got, err := engine.Run(context.Background(), 1, TriggerAuto)
	if err != nil {
		t.Fatal(err)
	}
	if got.Skipped {
		t.Fatalf("report = %+v", got)
	}
}
