package scheduler

import (
	"context"
	"slices"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

type snapshotTestProvider struct {
	ID          int64
	Name        string
	Platform    string
	Status      string
	Schedulable bool
	GroupIDs    []int64
}

func (a snapshotTestProvider) SnapshotMetadata() SnapshotMetadata {
	return SnapshotMetadata{ID: a.ID, Name: a.Name, Platform: a.Platform, GroupIDs: slices.Clone(a.GroupIDs)}
}

func snapshotTestData(value SnapshotProvider) *snapshotTestProvider {
	switch v := value.(type) {
	case snapshotTestProvider:
		return &v
	case *snapshotTestProvider:
		return v
	default:
		panic("unexpected snapshot fixture")
	}
}

const (
	StatusActive   = "active"
	StatusDisabled = "disabled"
)

const (
	PlatformAnthropic   = capability.PlatformAnthropic
	PlatformOpenAI      = capability.PlatformOpenAI
	PlatformGemini      = capability.PlatformGemini
	PlatformAntigravity = capability.PlatformAntigravity
	PlatformQoder       = capability.PlatformQoder
	PlatformGrok        = capability.PlatformGrok
)

// ptrInt64 构造测试所需的可选分组 ID。
func ptrInt64(value int64) *int64 { return &value }

// retirementProviderSource 按平台过滤夹具，并通过屏障控制数据库查询完成时机。
type retirementProviderSource struct {
	SnapshotProviderSource
	providers        []SnapshotProvider
	listPlatformFunc func(context.Context, string) ([]SnapshotProvider, error)
}

func (r *retirementProviderSource) ListSchedulableByPlatform(ctx context.Context, platform string) ([]SnapshotProvider, error) {
	if r.listPlatformFunc != nil {
		return r.listPlatformFunc(ctx, platform)
	}
	var out []SnapshotProvider
	for _, a := range r.providers {
		if a.SnapshotMetadata().Platform == platform {
			out = append(out, a)
		}
	}
	return out, nil
}

func (r *retirementProviderSource) ListSchedulableByGroupIDAndPlatform(ctx context.Context, _ int64, platform string) ([]SnapshotProvider, error) {
	return r.ListSchedulableByPlatform(ctx, platform)
}

func (r *retirementProviderSource) ListSchedulableUngroupedByPlatform(ctx context.Context, platform string) ([]SnapshotProvider, error) {
	return r.ListSchedulableByPlatform(ctx, platform)
}

func (r *retirementProviderSource) ListSchedulableByPlatforms(ctx context.Context, platforms []string) ([]SnapshotProvider, error) {
	var out []SnapshotProvider
	for _, a := range r.providers {
		for _, platform := range platforms {
			if a.SnapshotMetadata().Platform == platform {
				out = append(out, a)
				break
			}
		}
	}
	return out, nil
}

func (r *retirementProviderSource) ListSchedulableByGroupIDAndPlatforms(ctx context.Context, _ int64, platforms []string) ([]SnapshotProvider, error) {
	return r.ListSchedulableByPlatforms(ctx, platforms)
}

func (r *retirementProviderSource) ListSchedulableUngroupedByPlatforms(ctx context.Context, platforms []string) ([]SnapshotProvider, error) {
	return r.ListSchedulableByPlatforms(ctx, platforms)
}
