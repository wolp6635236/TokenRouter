package settings_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/config"
	"github.com/TokenFlux/TokenRouter/internal/identity"
	"github.com/TokenFlux/TokenRouter/internal/server/httpx"
	settingscore "github.com/TokenFlux/TokenRouter/internal/settings"
	"github.com/TokenFlux/TokenRouter/internal/settings/composite"
	settingskit "github.com/TokenFlux/TokenRouter/internal/settings/testkit"
)

type settingRepoStub struct {
	mu               sync.Mutex
	values           map[string]string
	err              error
	getValueCalls    int
	getMultipleCalls int
}

func TestSettingService_GetDefaultUserAPIKeyLimit(t *testing.T) {
	tests := []struct {
		name   string
		value  *string
		expect int
	}{
		{name: "设置缺失", expect: identity.DefaultUserAPIKeyLimit},
		{name: "非法字符串", value: stringPointer("invalid"), expect: identity.DefaultUserAPIKeyLimit},
		{name: "负数", value: stringPointer("-1"), expect: identity.DefaultUserAPIKeyLimit},
		{name: "超过数据库上限", value: stringPointer(fmt.Sprint(identity.MaxUserAPIKeyLimit + 1)), expect: identity.DefaultUserAPIKeyLimit},
		{name: "显式不限量", value: stringPointer("0"), expect: 0},
		{name: "显式上限", value: stringPointer("37"), expect: 37},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values := map[string]string{}
			if tt.value != nil {
				values[identity.SettingKeyDefaultUserAPIKeyLimit] = *tt.value
			}
			svc := settingskit.NewComposite(&settingRepoStub{values: values}, &config.Config{})
			require.Equal(t, tt.expect, svc.Identity.GetDefaultUserAPIKeyLimit(context.Background()))
		})
	}
}

func TestSettingService_ParseDefaultUserAPIKeyLimit(t *testing.T) {
	svc := settingskit.NewComposite(&settingGetAllRepoStub{}, &config.Config{})

	tests := []struct {
		name     string
		settings map[string]string
		expect   int
	}{
		{name: "设置缺失", settings: map[string]string{}, expect: identity.DefaultUserAPIKeyLimit},
		{name: "非法设置", settings: map[string]string{identity.SettingKeyDefaultUserAPIKeyLimit: "bad"}, expect: identity.DefaultUserAPIKeyLimit},
		{name: "负数设置", settings: map[string]string{identity.SettingKeyDefaultUserAPIKeyLimit: "-2"}, expect: identity.DefaultUserAPIKeyLimit},
		{name: "超过数据库上限", settings: map[string]string{identity.SettingKeyDefaultUserAPIKeyLimit: fmt.Sprint(identity.MaxUserAPIKeyLimit + 1)}, expect: identity.DefaultUserAPIKeyLimit},
		{name: "显式不限量", settings: map[string]string{identity.SettingKeyDefaultUserAPIKeyLimit: "0"}, expect: 0},
		{name: "显式上限", settings: map[string]string{identity.SettingKeyDefaultUserAPIKeyLimit: "58"}, expect: 58},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := composite.Parse(tt.settings, svc.Read)
			require.Equal(t, tt.expect, settings.DefaultUserAPIKeyLimit)
		})
	}
}

func TestSettingService_UpdateDefaultUserAPIKeyLimit(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	svc := settingskit.NewComposite(repo, &config.Config{})

	require.NoError(t, svc.Save(context.Background(), &composite.Snapshot{DefaultUserAPIKeyLimit: 0}))
	require.Equal(t, "0", repo.updates[identity.SettingKeyDefaultUserAPIKeyLimit])

	require.NoError(t, svc.Save(context.Background(), &composite.Snapshot{DefaultUserAPIKeyLimit: 64}))
	require.Equal(t, "64", repo.updates[identity.SettingKeyDefaultUserAPIKeyLimit])

	err := svc.Save(context.Background(), &composite.Snapshot{DefaultUserAPIKeyLimit: -1})
	require.ErrorIs(t, err, identity.ErrUserAPIKeyLimitInvalid)
	require.Equal(t, 400, httpx.ErrorCode(err))

	err = svc.Save(context.Background(), &composite.Snapshot{DefaultUserAPIKeyLimit: identity.MaxUserAPIKeyLimit + 1})
	require.ErrorIs(t, err, identity.ErrUserAPIKeyLimitInvalid)
	require.Equal(t, 400, httpx.ErrorCode(err))
}

func stringPointer(value string) *string {
	return &value
}

func (s *settingRepoStub) Get(ctx context.Context, key string) (*settingscore.Setting, error) {
	panic("unexpected Get call")
}

func (s *settingRepoStub) GetValue(ctx context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getValueCalls++
	if s.err != nil {
		return "", s.err
	}
	if v, ok := s.values[key]; ok {
		return v, nil
	}
	return "", settingscore.ErrSettingNotFound
}

func (s *settingRepoStub) Set(ctx context.Context, key, value string) error {
	panic("unexpected Set call")
}

func (s *settingRepoStub) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getMultipleCalls++
	if s.err != nil {
		return nil, s.err
	}
	result := make(map[string]string, len(keys))
	for _, key := range keys {
		if v, ok := s.values[key]; ok {
			result[key] = v
		}
	}
	return result, nil
}

func (s *settingRepoStub) SetMultiple(ctx context.Context, settings map[string]string) error {
	panic("unexpected SetMultiple call")
}

func (s *settingRepoStub) GetAll(ctx context.Context) (map[string]string, error) {
	panic("unexpected GetAll call")
}

func (s *settingRepoStub) Delete(ctx context.Context, key string) error {
	panic("unexpected Delete call")
}
