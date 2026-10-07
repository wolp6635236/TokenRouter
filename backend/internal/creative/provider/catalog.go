package provider

import (
	"github.com/TokenFlux/TokenRouter/internal/creative"
	"github.com/TokenFlux/TokenRouter/internal/protocol"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

// CatalogProvider 向公开目录提供模型规则。
func CatalogProvider(value *provider.Record) creative.CatalogProvider {
	if value == nil {
		return nil
	}
	return catalogProvider{value}
}

type catalogProvider struct{ *provider.Record }

func (a catalogProvider) GetModelMapping() map[string]string {
	return provider.ResolveModelMapping(a.Record, provideradapter.ModelDefaults())
}

func (a catalogProvider) GetConfiguredRequestModels() []string {
	return a.Record.GetConfiguredRequestModels(provideradapter.ModelDefaults())
}

func (a catalogProvider) IsModelSupported(model string) bool {
	return a.Record.IsModelSupported(model, provideradapter.ModelDefaults(), provideradapter.ModelRules(a.Record))
}

func (a catalogProvider) ResolveMappedModel(model string) (string, bool) {
	return provider.ResolveMappedModel(a.GetModelMapping(), model)
}

func (a catalogProvider) PlatformID() string { return a.Platform }
func (a catalogProvider) AllowsProtocol(source protocol.ProtocolID, fallbacks map[protocol.ProtocolID][]protocol.ProtocolID) bool {
	_, ok := capability.ResolveRoute(a.RoutingSnapshot().Protocols(), source, fallbacks)
	return ok
}
