package scheduler

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// updateSnapshotValue 为快照更新测试提供重建元数据。
type updateSnapshotValue struct{ id int64 }

func (v updateSnapshotValue) SnapshotMetadata() SnapshotMetadata {
	return SnapshotMetadata{ID: v.id}
}

type updateSnapshotCache struct {
	SnapshotCache
	values []SnapshotProvider
	err    error
}

func (c *updateSnapshotCache) SetProvider(_ context.Context, value SnapshotProvider) error {
	c.values = append(c.values, value)
	return c.err
}

// TestSchedulerSnapshotService_UpdateProviderInCache 检查提供商快照更新和错误处理。
func TestSchedulerSnapshotService_UpdateProviderInCache(t *testing.T) {
	t.Run("calls cache.SetProvider", func(t *testing.T) {
		cache := &updateSnapshotCache{}
		core := NewSnapshotService(cache, nil, nil, nil, nil)
		require.NoError(t, core.UpdateProviderInCache(t.Context(), updateSnapshotValue{123}))
		require.Len(t, cache.values, 1)
		require.Equal(t, int64(123), cache.values[0].SnapshotMetadata().ID)
	})
	t.Run("returns nil when cache is nil", func(t *testing.T) {
		core := NewSnapshotService(nil, nil, nil, nil, nil)
		require.NoError(t, core.UpdateProviderInCache(t.Context(), updateSnapshotValue{1}))
	})
	t.Run("returns nil when provider is nil", func(t *testing.T) {
		cache := &updateSnapshotCache{}
		core := NewSnapshotService(cache, nil, nil, nil, nil)
		require.NoError(t, core.UpdateProviderInCache(t.Context(), nil))
		require.Empty(t, cache.values)
	})
	t.Run("propagates cache error", func(t *testing.T) {
		expected := errors.New("cache error")
		core := NewSnapshotService(&updateSnapshotCache{err: expected}, nil, nil, nil, nil)
		require.ErrorIs(t, core.UpdateProviderInCache(t.Context(), updateSnapshotValue{1}), expected)
	})
}
