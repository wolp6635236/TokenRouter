package dto

// OverloadCooldownSettings 是过载冷却设置的 HTTP 数据结构。
type OverloadCooldownSettings struct {
	Enabled         bool `json:"enabled"`
	CooldownMinutes int  `json:"cooldown_minutes"`
}

// OpenAI403CooldownSettings 是 OpenAI 403 冷却设置的 HTTP 数据结构。
type OpenAI403CooldownSettings struct {
	Enabled                 bool `json:"enabled"`
	CooldownMinutes         int  `json:"cooldown_minutes"`
	ErrorOnThresholdEnabled bool `json:"error_on_threshold_enabled"`
	ThresholdCount          int  `json:"threshold_count"`
	ThresholdWindowMinutes  int  `json:"threshold_window_minutes"`
}

// RateLimit429CooldownSettings 是 429 冷却设置的 HTTP 数据结构。
type RateLimit429CooldownSettings struct {
	Enabled         bool `json:"enabled"`
	CooldownSeconds int  `json:"cooldown_seconds"`
}

// OpenAIImagesOAuthUnavailableCooldownSettings 是 OAuth 图片不可用时的冷却设置。
type OpenAIImagesOAuthUnavailableCooldownSettings struct {
	CooldownMinutes int `json:"cooldown_minutes"`
}

// StreamTimeoutSettings 是流式请求超时设置的 HTTP 数据结构。
type StreamTimeoutSettings struct {
	Enabled                bool   `json:"enabled"`
	Action                 string `json:"action"`
	TempUnschedMinutes     int    `json:"temp_unsched_minutes"`
	ThresholdCount         int    `json:"threshold_count"`
	ThresholdWindowMinutes int    `json:"threshold_window_minutes"`
}
