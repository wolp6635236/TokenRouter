package app

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	"github.com/TokenFlux/TokenRouter/internal/billing"
	pricingprovider "github.com/TokenFlux/TokenRouter/internal/billing/provider"
	catalogprovider "github.com/TokenFlux/TokenRouter/internal/modelcatalog/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	routingpostgres "github.com/TokenFlux/TokenRouter/internal/routing/postgres"
)

func providePricingConfigService(repo *routingpostgres.PricingConfigStore, groups *routingpostgres.GroupStore, invalidator apikey.APIKeyAuthCacheInvalidator) *routing.PricingConfigService {
	return routing.NewPricingConfigService(repo, invalidator, routing.PricingConfigOptions{Warn: slog.Warn, Now: time.Now, LoadLocation: pricingprovider.LoadPricingLocation, ReadGroup: func(ctx context.Context, id int64) (*routing.Group, error) {
		access, ok := apikey.AccessSnapshotFromContext(ctx)
		if ok {
			key := access.KeyView()
			if key != nil && key.Group != nil && key.Group.ID == id && key.Group.Hydrated {
				return key.Group, nil
			}
		}
		return groups.GetByIDLite(ctx, id)
	}})
}

func providePricingCatalog(calculator *billing.Calculator, prices *catalogprovider.Service) *routing.PricingCatalog {
	return &routing.PricingCatalog{Prices: calculator, Update: calculator.ForceUpdatePricing, Snapshot: func() routing.DefaultPricingSnapshot {
		snapshot := prices.ReadOnlySnapshot()
		data := snapshot.Snapshot()
		frozen := calculator.WithPriceCatalog(snapshot)
		entries := make(map[string]string)
		modes := make(map[string]string)
		for model, value := range data.Data {
			if value == nil {
				continue
			}
			platform := value.Provider
			switch platform {
			case "xai":
				platform = "grok"
			case "moonshot":
				platform = "kimi"
			}
			if platform == "" {
				platform = "other"
			}
			entries[model] = platform
			modes[model] = value.Mode
		}
		for _, entry := range prices.AttributesSnapshot().Items {
			if _, exists := entries[entry.Model]; !exists {
				entries[entry.Model] = entry.Provider
			}
		}
		names := make([]string, 0, len(entries))
		for model := range entries {
			names = append(names, model)
		}
		sort.Strings(names)
		result := routing.DefaultPricingSnapshot{UpdatedAt: data.LastUpdated, Version: data.LocalHash, LastError: data.LastError}
		for _, model := range names {
			mode := modes[model]
			if strings.Contains(model, "grok-imagine-video") {
				mode = "video"
			}
			result.Prices = append(result.Prices, frozen.DefaultModelPrice(model, entries[model], mode))
		}
		return result
	}}
}
