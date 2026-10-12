package testkit

import (
	"context"

	"github.com/TokenFlux/TokenRouter/internal/gateway"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/moderation"
	"github.com/TokenFlux/TokenRouter/internal/ops"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/search"
	"github.com/TokenFlux/TokenRouter/internal/settings"
	"github.com/TokenFlux/TokenRouter/internal/upstream/anthropic"
	"github.com/TokenFlux/TokenRouter/internal/upstream/antigravity"
)

type FastPolicySettingsRepo struct {
	Values map[string]string
}

func (s *FastPolicySettingsRepo) Get(ctx context.Context, key string) (*settings.Setting, error) {
	panic("unexpected Get call")
}

func (s *FastPolicySettingsRepo) GetValue(ctx context.Context, key string) (string, error) {
	if v, ok := s.Values[key]; ok {
		return v, nil
	}
	return "", settings.ErrSettingNotFound
}

func (s *FastPolicySettingsRepo) Set(ctx context.Context, key, value string) error {
	if s.Values == nil {
		s.Values = map[string]string{}
	}
	s.Values[key] = value
	return nil
}

// GetMultiple 按请求键读取夹具数据，缺省设置由生产读取器处理。
func (s *FastPolicySettingsRepo) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	Values := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := s.Values[key]; ok {
			Values[key] = value
		}
	}
	return Values, nil
}

func (s *FastPolicySettingsRepo) SetMultiple(ctx context.Context, settings map[string]string) error {
	panic("unexpected SetMultiple call")
}

func (s *FastPolicySettingsRepo) GetAll(ctx context.Context) (map[string]string, error) {
	panic("unexpected GetAll call")
}

func (s *FastPolicySettingsRepo) Delete(ctx context.Context, key string) error {
	panic("unexpected Delete call")
}

// RuntimeReaders 将 HTTP 夹具的动态设置数据交给设置读取器。
func RuntimeReaders(repo settings.Repository) *gatewayprovider.RuntimeReaders {
	runtime := gateway.NewRuntimeSettings(repo, settings.ErrSettingNotFound, func() *gateway.BetaPolicySettings {
		return gatewayprovider.GatewayBetaPolicy(anthropic.DefaultBetaPolicySettings())
	}, gateway.ClientSettingsOptions{NormalizeUserAgentVersion: antigravity.NormalizeUserAgentVersion, DefaultUserAgentVersion: antigravity.GetDefaultUserAgentVersion})
	return &gatewayprovider.RuntimeReaders{Gateway: runtime, Provider: provider.NewRuntimeSettings(repo, settings.ErrSettingNotFound), Quota: provider.NewQuotaSettingsCache(repo, settings.ErrSettingNotFound, ops.ParseRuntimeQuotaAutoPauseSettings), Routing: routing.NewRuntimeSettings(repo), Moderation: moderation.NewRuntimeSettings(repo, settings.ErrSettingNotFound), Search: search.NewConfigService(repo, nil, nil, search.NewRegistry()), Scheduler: repo}
}
