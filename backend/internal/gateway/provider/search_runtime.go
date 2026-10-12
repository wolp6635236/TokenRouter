package provider

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/TokenFlux/TokenRouter/internal/gateway/searchtools"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/search"
)

// searchSource 引用配置运行时拥有的同一个注册表，避免另建全局 Manager 指针。
type searchSource struct{ registry *search.Registry }

type searchGroupPolicy struct{ groupPolicies *routing.PricingConfigService }

func (s searchSource) Current() searchtools.Searcher {
	if s.registry == nil {
		return nil
	}
	manager := s.registry.Get()
	if manager == nil {
		return nil
	}
	return manager
}

func (p searchGroupPolicy) Enabled(ctx context.Context, groupID int64, platform string) (bool, error) {
	policy, err := p.groupPolicies.GetGroupPolicy(ctx, groupID)
	if err != nil || policy == nil {
		return false, err
	}
	return policy.IsWebSearchEmulationEnabled(platform), nil
}

// NewSearchTools 构造同步搜索工具，绑定分组策略、Manager 和配额读取接口。
func NewSearchTools(runtime *search.ConfigService, groupPolicies *routing.PricingConfigService) *searchtools.Emulator {
	var registry *search.Registry
	var settings searchtools.Settings
	if runtime != nil {
		registry = runtime.Registry()
		settings = runtime
	}
	var policy searchtools.GroupPolicy
	if groupPolicies != nil {
		policy = searchGroupPolicy{groupPolicies}
	}
	return searchtools.NewEmulator(searchSource{registry}, settings, policy, time.Now, func() string { return uuid.New().String() }, observeSearch)
}

// SearchProviderMode 读取搜索提供商模式并记录配置诊断。
func SearchProviderMode(value *searchtools.ProviderPolicy) string {
	selection := searchtools.ProviderMode(value)
	if selection.LegacyBool != nil {
		slog.Debug("legacy bool web_search_emulation value", "provider_id", value.ID, "value", *selection.LegacyBool)
	}
	return selection.Mode
}

func observeSearch(e searchtools.Event) {
	switch e.Kind {
	case "executing":
		slog.Info("web search emulation: executing search", "provider_id", e.ProviderID, "provider_name", e.ProviderName, "query", e.Query)
	case "completed":
		slog.Info("web search emulation: search completed", "provider", e.Provider, "results_count", e.Results)
	case "search_failed":
		slog.Error("web search emulation: search failed", "error", e.Err)
	case "write_failed":
		slog.Warn("web search emulation: SSE write failed, stopping", "error", e.Err)
	}
}
