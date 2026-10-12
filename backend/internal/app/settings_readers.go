package app

import (
	"log/slog"

	"github.com/TokenFlux/TokenRouter/internal/audit"
	"github.com/TokenFlux/TokenRouter/internal/creative"
	"github.com/TokenFlux/TokenRouter/internal/gateway/admission"
	"github.com/TokenFlux/TokenRouter/internal/identity"
	"github.com/TokenFlux/TokenRouter/internal/promotion"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/server/runtimeconfig"
	"github.com/TokenFlux/TokenRouter/internal/settings"
	"github.com/TokenFlux/TokenRouter/internal/usage"
)

// providePanelSettings 为各模块的配置读取器绑定共享 Store，缓存和读取时机由各模块管理。
// @project-doc docs/interfaces/configuration.md#runtime_settings
func providePanelSettings(store *settings.Store) *runtimeconfig.PanelSettings {
	return runtimeconfig.NewPanelSettings(store)
}

func providePromotionSettings(store *settings.Store) *promotion.RuntimeSettings {
	return promotion.NewRuntimeSettings(store)
}

func provideProviderSettings(store *settings.Store) *provider.RuntimeSettings {
	return provider.NewRuntimeSettings(store, settings.ErrSettingNotFound)
}

func provideUsageSettings(store *settings.Store) *usage.RuntimeSettings {
	return usage.NewRuntimeSettings(store)
}

func provideAuditSettings(store *settings.Store) *audit.RetentionSettings {
	return audit.NewRetentionSettings(store)
}

func provideIdentitySettings(store *settings.Store) *identity.RuntimeSettings {
	return identity.NewRuntimeSettings(store, settings.ErrSettingNotFound)
}

func provideRoutingSettings(store *settings.Store) *routing.RuntimeSettings {
	return routing.NewRuntimeSettings(store)
}

func provideBackendMode(store *settings.Store) *admission.BackendMode {
	return admission.NewBackendMode(store, slog.Warn)
}

// provideCreativeRuntimeSettings 绑定共享 Store，创作台在每次请求时读取设置。
func provideCreativeRuntimeSettings(store *settings.Store) *creative.RuntimeSettings {
	return creative.NewRuntimeSettings(store, settings.ErrSettingNotFound)
}
