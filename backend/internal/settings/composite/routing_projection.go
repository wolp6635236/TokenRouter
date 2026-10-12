package composite

import "github.com/TokenFlux/TokenRouter/internal/routing"

// RoutingAdminSettings 从综合快照提取路由设置。
func (s *Snapshot) RoutingAdminSettings() routing.AdminSettings {
	return routing.AdminSettings{
		EnableModelFallback:                  s.EnableModelFallback,
		FallbackModelAnthropic:               s.FallbackModelAnthropic,
		FallbackModelAntigravity:             s.FallbackModelAntigravity,
		FallbackModelGemini:                  s.FallbackModelGemini,
		FallbackModelOpenAI:                  s.FallbackModelOpenAI,
		MarketplaceAvailabilityBucketMinutes: s.MarketplaceAvailabilityBucketMinutes,
		MarketplaceAvailabilityWindowDays:    s.MarketplaceAvailabilityWindowDays,
	}
}

// ApplyRoutingAdminSettings 将路由设置写入综合快照。
func (s *Snapshot) ApplyRoutingAdminSettings(value routing.AdminSettings) {
	s.EnableModelFallback = value.EnableModelFallback
	s.FallbackModelAnthropic = value.FallbackModelAnthropic
	s.FallbackModelAntigravity = value.FallbackModelAntigravity
	s.FallbackModelGemini = value.FallbackModelGemini
	s.FallbackModelOpenAI = value.FallbackModelOpenAI
	s.MarketplaceAvailabilityBucketMinutes = value.MarketplaceAvailabilityBucketMinutes
	s.MarketplaceAvailabilityWindowDays = value.MarketplaceAvailabilityWindowDays
}

// ApplyRoutingAdminReadSettings 将模型回退和市场可用性统计窗口写入快照。
func (s *Snapshot) ApplyRoutingAdminReadSettings(value *routing.AdminReadSettings) {
	s.EnableModelFallback = value.EnableModelFallback
	s.FallbackModelAnthropic = value.FallbackModelAnthropic
	s.FallbackModelAntigravity = value.FallbackModelAntigravity
	s.FallbackModelGemini = value.FallbackModelGemini
	s.FallbackModelOpenAI = value.FallbackModelOpenAI
	s.MarketplaceAvailabilityBucketMinutes = value.MarketplaceAvailabilityBucketMinutes
	s.MarketplaceAvailabilityWindowDays = value.MarketplaceAvailabilityWindowDays
}
