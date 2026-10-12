package app

import (
	"context"

	"github.com/TokenFlux/TokenRouter/internal/provider"
	providerpostgres "github.com/TokenFlux/TokenRouter/internal/provider/postgres"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	routingpostgres "github.com/TokenFlux/TokenRouter/internal/routing/postgres"
	"github.com/TokenFlux/TokenRouter/internal/scheduler"
	"github.com/TokenFlux/TokenRouter/internal/scheduler/rediscache/codec"
)

type schedulerProviderSource struct {
	*providerpostgres.ProviderStore
}

type schedulerGroupSource struct{ *routingpostgres.GroupStore }

func (r schedulerProviderSource) GetByID(ctx context.Context, id int64) (scheduler.SnapshotProvider, error) {
	value, err := r.ProviderStore.GetByID(ctx, id)
	return codec.WrapRecord(value), err
}

func (r schedulerProviderSource) GetByIDs(ctx context.Context, ids []int64) ([]scheduler.SnapshotProvider, error) {
	value, err := r.ProviderStore.GetByIDs(ctx, ids)
	return wrapSchedulerRecordPointers(value), err
}

func (r schedulerProviderSource) ListSchedulableByPlatform(ctx context.Context, platform string) ([]scheduler.SnapshotProvider, error) {
	value, err := r.ProviderStore.ListSchedulableByPlatform(ctx, platform)
	return wrapSchedulerRecords(value), err
}

func (r schedulerProviderSource) ListSchedulableUngroupedByPlatform(ctx context.Context, platform string) ([]scheduler.SnapshotProvider, error) {
	value, err := r.ProviderStore.ListSchedulableUngroupedByPlatform(ctx, platform)
	return wrapSchedulerRecords(value), err
}

func (r schedulerProviderSource) ListSchedulableByPlatforms(ctx context.Context, platforms []string) ([]scheduler.SnapshotProvider, error) {
	value, err := r.ProviderStore.ListSchedulableByPlatforms(ctx, platforms)
	return wrapSchedulerRecords(value), err
}

func (r schedulerProviderSource) ListSchedulableUngroupedByPlatforms(ctx context.Context, platforms []string) ([]scheduler.SnapshotProvider, error) {
	value, err := r.ProviderStore.ListSchedulableUngroupedByPlatforms(ctx, platforms)
	return wrapSchedulerRecords(value), err
}

func (r schedulerProviderSource) ListSchedulableByGroupIDAndPlatform(ctx context.Context, group int64, platform string) ([]scheduler.SnapshotProvider, error) {
	value, err := r.ProviderStore.ListSchedulableByGroupIDAndPlatform(ctx, group, platform)
	return wrapSchedulerRecords(value), err
}

func (r schedulerProviderSource) ListSchedulableByGroupIDAndPlatforms(ctx context.Context, group int64, platforms []string) ([]scheduler.SnapshotProvider, error) {
	value, err := r.ProviderStore.ListSchedulableByGroupIDAndPlatforms(ctx, group, platforms)
	return wrapSchedulerRecords(value), err
}

func schedulerSnapshotGroup(value *routing.Group) *scheduler.SnapshotGroup {
	if value == nil {
		return nil
	}
	return &scheduler.SnapshotGroup{ID: value.ID, Name: value.Name, Status: value.Status, Hydrated: value.Hydrated}
}

func (r schedulerGroupSource) GetByID(ctx context.Context, id int64) (*scheduler.SnapshotGroup, error) {
	value, err := r.GroupStore.GetByID(ctx, id)
	return schedulerSnapshotGroup(value), err
}

func (r schedulerGroupSource) GetByIDLite(ctx context.Context, id int64) (*scheduler.SnapshotGroup, error) {
	value, err := r.GroupStore.GetByIDLite(ctx, id)
	return schedulerSnapshotGroup(value), err
}

func (r schedulerGroupSource) ListActive(ctx context.Context) ([]scheduler.SnapshotGroup, error) {
	value, err := r.GroupStore.ListActive(ctx)
	if value == nil {
		return nil, err
	}
	out := make([]scheduler.SnapshotGroup, len(value))
	for i := range value {
		out[i] = *schedulerSnapshotGroup(&value[i])
	}
	return out, err
}

func (r schedulerGroupSource) ListActiveIDs(ctx context.Context) ([]int64, error) {
	return r.GroupStore.ListActiveIDs(ctx)
}

func wrapSchedulerRecords(values []provider.Record) []scheduler.SnapshotProvider {
	if values == nil {
		return nil
	}
	out := make([]scheduler.SnapshotProvider, len(values))
	for i := range values {
		out[i] = codec.WrapRecord(&values[i])
	}
	return out
}
