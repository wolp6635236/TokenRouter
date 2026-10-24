package app

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/notification"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	providerpostgres "github.com/TokenFlux/TokenRouter/internal/provider/postgres"
	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"
	"github.com/TokenFlux/TokenRouter/internal/qualityprobe"
	qualityprobehttp "github.com/TokenFlux/TokenRouter/internal/qualityprobe/httpapi"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	routingpostgres "github.com/TokenFlux/TokenRouter/internal/routing/postgres"
	"github.com/TokenFlux/TokenRouter/internal/settings"
)

type qualityProbeDirectory struct {
	store *providerpostgres.ProviderStore
}

type qualityProbeGroups struct {
	store *routingpostgres.GroupStore
}

type qualityProbeGroupModels struct {
	admin *routing.GroupAdmin
}

type qualityProbeProber struct {
	tests *provider.TestService
}

type qualityProbeSink struct {
	text      strings.Builder
	lastError string
}

type qualityProbeCatalog struct {
	store *providerpostgres.ProviderStore
}

type qualityProbeMail struct {
	mailer *notification.Mailer
}

func provideQualityProbeEngine(
	store *settings.Store,
	providers *providerpostgres.ProviderStore,
	groups *routingpostgres.GroupStore,
	groupAdmin *routing.GroupAdmin,
	tests *provider.TestService,
	mailer *notification.Mailer,
) *qualityprobe.Engine {
	return &qualityprobe.Engine{
		Settings:    store,
		Dir:         qualityProbeDirectory{store: providers},
		Groups:      qualityProbeGroups{store: groups},
		GroupModels: qualityProbeGroupModels{admin: groupAdmin},
		Prober:      qualityProbeProber{tests: tests},
		Catalog:     qualityProbeCatalog{store: providers},
		Mail:        qualityProbeMail{mailer: mailer},
		NotFound:    settings.ErrSettingNotFound,
	}
}

func provideQualityProbeHTTP(engine *qualityprobe.Engine) *qualityprobehttp.Handler {
	return qualityprobehttp.New(engine)
}

func provideQualityProbeRunner(engine *qualityprobe.Engine) *qualityprobe.Runner {
	return &qualityprobe.Runner{Engine: engine}
}

func (d qualityProbeDirectory) Get(ctx context.Context, id int64) (*qualityprobe.Snapshot, error) {
	record, err := d.store.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	snap := snapshotFromRecord(record)
	return &snap, nil
}

func (d qualityProbeDirectory) ListByPlatform(ctx context.Context, platform string) ([]qualityprobe.Snapshot, error) {
	records, err := d.store.ListByPlatform(ctx, platform)
	if err != nil {
		return nil, err
	}
	out := make([]qualityprobe.Snapshot, 0, len(records))
	for i := range records {
		out = append(out, snapshotFromRecord(&records[i]))
	}
	return out, nil
}

func (d qualityProbeDirectory) UpdateExtra(ctx context.Context, id int64, updates map[string]any) error {
	return d.store.UpdateExtra(ctx, id, updates)
}

func (d qualityProbeDirectory) SetTempUnschedulable(ctx context.Context, id int64, until time.Time, reason string) error {
	return d.store.SetTempUnschedulable(ctx, id, until, reason)
}

func (d qualityProbeDirectory) ClearTempUnschedulable(ctx context.Context, id int64) error {
	record, err := d.store.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if record.TempUnschedulableReason != "" && record.TempUnschedulableReason != qualityprobe.TempUnscheduleReason {
		return nil
	}
	return d.store.ClearTempUnschedulable(ctx, id)
}

func (d qualityProbeDirectory) SchedulableIDs(ctx context.Context, groupID int64) ([]int64, error) {
	records, err := d.store.ListSchedulableByGroupID(ctx, groupID)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(records))
	for i := range records {
		ids = append(ids, records[i].ID)
	}
	return ids, nil
}

func snapshotFromRecord(record *provider.Record) qualityprobe.Snapshot {
	if record == nil {
		return qualityprobe.Snapshot{}
	}
	return qualityprobe.Snapshot{
		ID:                      record.ID,
		Name:                    record.Name,
		Platform:                record.Platform,
		Status:                  record.Status,
		SchedulingOff:           !record.Schedulable,
		Schedulable:             record.IsSchedulable(),
		GroupIDs:                record.GroupIDs,
		GroupNames:              qualityProbeGroupNames(record),
		Extra:                   record.Extra,
		TempUnschedulableUntil:  record.TempUnschedulableUntil,
		TempUnschedulableReason: record.TempUnschedulableReason,
	}
}

// qualityProbeGroupNames copies loaded group names for probe log presentation.
// A provider can retain IDs without hydrated group records in narrow storage
// reads, so the log leaves names empty until a management list hydrates them.
func qualityProbeGroupNames(record *provider.Record) []string {
	if record == nil {
		return nil
	}
	seen := make(map[string]struct{})
	names := make([]string, 0, len(record.Groups))
	for _, group := range record.Groups {
		if group == nil || strings.TrimSpace(group.Name) == "" {
			continue
		}
		name := strings.TrimSpace(group.Name)
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (g qualityProbeGroups) ActiveIDs(ctx context.Context) ([]int64, error) {
	if g.store == nil {
		return nil, nil
	}
	return g.store.ListActiveIDs(ctx)
}

// Models returns requestable model candidates from one active routing group.
func (m qualityProbeGroupModels) Models(ctx context.Context, groupID int64) ([]string, error) {
	if m.admin == nil {
		return []string{}, nil
	}
	return m.admin.GetGroupModelsListCandidates(ctx, groupID, "")
}

func (p qualityProbeProber) ProbeText(ctx context.Context, providerID int64, model, prompt string) (qualityprobe.TextResult, error) {
	testType := provider.ProviderTestTypeText
	sink := &qualityProbeSink{}
	err := p.tests.Test(ctx, provider.TestRequest{
		ProviderID: providerID,
		Model:      model,
		Prompt:     prompt,
		Type:       &testType,
		UserAgent:  "quality-probe",
		Originator: "quality-probe",
		Automatic:  true,
	}, sink)
	result := qualityprobe.TextResult{Answer: sink.text.String(), Error: sink.lastError}
	return result, err
}

func (s *qualityProbeSink) Begin(context.Context, bool) error { return nil }

func (s *qualityProbeSink) Emit(_ context.Context, event provider.TestEvent) error {
	if event.Text != "" {
		// strings.Builder.WriteString 不会返回错误，显式丢弃以满足 errcheck。
		_, _ = s.text.WriteString(event.Text)
	}
	if event.Error != "" {
		s.lastError = event.Error
	}
	return nil
}

func (c qualityProbeCatalog) Models(ctx context.Context, snap *qualityprobe.Snapshot) []string {
	if snap == nil {
		return nil
	}
	record, err := c.store.GetByID(ctx, snap.ID)
	if err != nil || record == nil {
		return nil
	}
	return record.GetConfiguredRequestModels(provideradapter.ModelDefaults())
}

func (m qualityProbeMail) Send(ctx context.Context, to, subject, body string) error {
	if m.mailer == nil {
		return nil
	}
	return m.mailer.SendEmail(ctx, to, subject, body)
}
