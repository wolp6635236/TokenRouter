package settings_test

import (
	"context"
	"maps"

	settingscore "github.com/TokenFlux/TokenRouter/internal/settings"
)

type bmUpdateRepoStub struct {
	updates    map[string]string
	getValueFn func(ctx context.Context, key string) (string, error)
}

type settingUpdateRepoStub struct {
	updates        map[string]string
	values         map[string]string
	setMultipleErr error
}

type settingGetAllRepoStub struct {
	values map[string]string
}

func (s *bmUpdateRepoStub) Get(ctx context.Context, key string) (*settingscore.Setting, error) {
	panic("unexpected Get call")
}

func (s *bmUpdateRepoStub) GetValue(ctx context.Context, key string) (string, error) {
	if s.getValueFn == nil {
		panic("unexpected GetValue call")
	}
	return s.getValueFn(ctx, key)
}

func (s *bmUpdateRepoStub) Set(ctx context.Context, key, value string) error {
	panic("unexpected Set call")
}

func (s *bmUpdateRepoStub) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	panic("unexpected GetMultiple call")
}

func (s *bmUpdateRepoStub) SetMultiple(ctx context.Context, settings map[string]string) error {
	s.updates = make(map[string]string, len(settings))
	maps.Copy(s.updates, settings)
	return nil
}

func (s *bmUpdateRepoStub) GetAll(ctx context.Context) (map[string]string, error) {
	return maps.Clone(s.updates), nil
}

func (s *bmUpdateRepoStub) Delete(ctx context.Context, key string) error {
	panic("unexpected Delete call")
}

func (s *settingUpdateRepoStub) Get(ctx context.Context, key string) (*settingscore.Setting, error) {
	panic("unexpected Get call")
}

func (s *settingUpdateRepoStub) GetValue(ctx context.Context, key string) (string, error) {
	if s.values != nil {
		if value, ok := s.values[key]; ok {
			return value, nil
		}
	}
	return "", settingscore.ErrSettingNotFound
}

func (s *settingUpdateRepoStub) Set(ctx context.Context, key, value string) error {
	panic("unexpected Set call")
}

func (s *settingUpdateRepoStub) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	panic("unexpected GetMultiple call")
}

func (s *settingUpdateRepoStub) SetMultiple(ctx context.Context, settings map[string]string) error {
	s.updates = make(map[string]string, len(settings))
	for k, v := range settings {
		s.updates[k] = v
		if s.values == nil {
			s.values = map[string]string{}
		}
		s.values[k] = v
	}
	return s.setMultipleErr
}

func (s *settingUpdateRepoStub) GetAll(ctx context.Context) (map[string]string, error) {
	out := make(map[string]string, len(s.values))
	maps.Copy(out, s.values)
	return out, nil
}

func (s *settingUpdateRepoStub) Delete(ctx context.Context, key string) error {
	panic("unexpected Delete call")
}

func (s *settingGetAllRepoStub) Get(ctx context.Context, key string) (*settingscore.Setting, error) {
	panic("unexpected Get call")
}

func (s *settingGetAllRepoStub) GetValue(ctx context.Context, key string) (string, error) {
	panic("unexpected GetValue call")
}

func (s *settingGetAllRepoStub) Set(ctx context.Context, key, value string) error {
	panic("unexpected Set call")
}

func (s *settingGetAllRepoStub) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	panic("unexpected GetMultiple call")
}

func (s *settingGetAllRepoStub) SetMultiple(ctx context.Context, settings map[string]string) error {
	panic("unexpected SetMultiple call")
}

func (s *settingGetAllRepoStub) GetAll(ctx context.Context) (map[string]string, error) {
	out := make(map[string]string, len(s.values))
	maps.Copy(out, s.values)
	return out, nil
}

func (s *settingGetAllRepoStub) Delete(ctx context.Context, key string) error {
	panic("unexpected Delete call")
}
