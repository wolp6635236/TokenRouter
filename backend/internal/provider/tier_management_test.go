package provider

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// 维护停止必须等待实际观测，迟到返回不能在 Stop 后写配置。
type tierLifecycleStore struct {
	TierManagementStore
	writes atomic.Int64
}

// 批量查询最多十并发，分别记录失败。总数排除非法提供商，缺失或空列表时通过存储查询。
type tierBatchStore struct {
	TierManagementStore
	values     []Record
	queryCalls int
	queryLimit int
}

func (s *tierLifecycleStore) UpdateProvider(context.Context, int64, *UpdateProviderInput) (*Record, error) {
	s.writes.Add(1)
	return nil, nil
}

func TestTierMaintenanceStopWaitsAndRejectsLateWrite(t *testing.T) {
	store := &tierLifecycleStore{}
	entered := make(chan struct{})
	release := make(chan struct{})
	core := NewTierManagement(store, TierManagementOptions{Observe: func(context.Context, *Record) (GoogleOneTierObservation, error) {
		close(entered)
		<-release
		return GoogleOneTierObservation{TierID: "new"}, nil
	}})
	v := &Record{ID: 1, Platform: PlatformGemini, Type: ProviderTypeOAuth, Credentials: map[string]any{"oauth_type": "google_one"}}
	done := make(chan error, 1)
	go func() { _, err := core.Refresh(context.Background(), v); done <- err }()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	stopErr := core.StopContext(ctx)
	require.Error(t, stopErr)
	_, err := core.Refresh(context.Background(), v)
	require.ErrorIs(t, err, ErrRefreshStopped)
	close(release)
	require.ErrorIs(t, <-done, context.Canceled)
	require.Equal(t, int64(0), store.writes.Load())
	require.Equal(t, stopErr, core.StopContext(context.Background()))
}

func (s *tierBatchStore) ListProviders(_ context.Context, page, size int, platform, kind, status, search string, id int64, privacy, sortBy, sortOrder string) ([]Record, int64, error) {
	s.queryCalls++
	s.queryLimit = size
	return s.values, int64(len(s.values)), nil
}

func (s *tierBatchStore) UpdateProvider(_ context.Context, id int64, input *UpdateProviderInput) (*Record, error) {
	if !input.PatchCredentials || !input.PatchExtra || input.ExpectedCredentials == nil {
		panic("维护必须使用身份条件与字段补丁")
	}
	return nil, nil
}

func TestTierMaintenanceBatchKeepsEmptySelectionAndPartialFailure(t *testing.T) {
	values := []Record{{ID: 1, Platform: PlatformGemini, Type: ProviderTypeOAuth, Credentials: map[string]any{"oauth_type": "google_one"}}, {ID: 2, Platform: PlatformGemini, Type: ProviderTypeOAuth, Credentials: map[string]any{"oauth_type": "google_one"}}, {ID: 3, Platform: PlatformGemini, Type: ProviderTypeOAuth, Credentials: map[string]any{"oauth_type": "code_assist"}}}
	store := &tierBatchStore{values: values}
	core := NewTierManagement(store, TierManagementOptions{Observe: func(_ context.Context, v *Record) (GoogleOneTierObservation, error) {
		if v.ID == 2 {
			return GoogleOneTierObservation{}, errors.New("upstream unavailable")
		}
		return GoogleOneTierObservation{TierID: "new"}, nil
	}})
	result, err := core.Batch(context.Background(), nil)
	require.NoError(t, err)
	require.Equal(t, 1, store.queryCalls)
	require.Equal(t, 10000, store.queryLimit)
	require.Equal(t, 2, result.Total)
	require.Equal(t, 1, result.Success)
	require.Equal(t, 1, result.Failed)
	require.Equal(t, []TierRefreshFailure{{ProviderID: 2, Error: "upstream unavailable"}}, result.Errors)
}

// TestTierMaintenanceStopPreventsQueuedObservations 验证已排队的批量项在停止后直接返回取消，不因十槽等待结束而继续调用供应商。
func TestTierMaintenanceStopPreventsQueuedObservations(t *testing.T) {
	values := make([]Record, 20)
	for i := range values {
		values[i] = Record{ID: int64(i + 1), Platform: PlatformGemini, Type: ProviderTypeOAuth, Credentials: map[string]any{"oauth_type": "google_one"}}
	}
	store := &tierBatchStore{values: values}
	entered, release := make(chan struct{}), make(chan struct{})
	var observations atomic.Int64
	core := NewTierManagement(store, TierManagementOptions{Observe: func(context.Context, *Record) (GoogleOneTierObservation, error) {
		if observations.Add(1) == 10 {
			close(entered)
		}
		<-release
		return GoogleOneTierObservation{TierID: "late"}, nil
	}})
	done := make(chan *TierBatchResult, 1)
	go func() { v, _ := core.Batch(context.Background(), nil); done <- v }()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	require.Error(t, core.StopContext(ctx))
	close(release)
	result := <-done
	require.Equal(t, int64(10), observations.Load())
	require.Equal(t, 20, result.Failed)
	require.Zero(t, result.Success)
}
