package provider

import (
	"time"

	"github.com/TokenFlux/TokenRouter/internal/pkg/apperror"
	"github.com/TokenFlux/TokenRouter/internal/upstream/usageview"
)

const (
	OllamaCloudUsageStatusOK           = "ok"
	OllamaCloudUsageStatusUnauthorized = "unauthorized"
	OllamaCloudUsageStatusFailed       = "failed"

	OllamaCloudUsageSessionExtraKey     = "ollama_cloud_usage_session"
	OllamaCloudUsageAutoRefreshExtraKey = "ollama_cloud_usage_auto_refresh"
	OllamaCloudUsageMinFetchInterval    = 15 * time.Minute
)

var (
	ErrOllamaCloudUsageUnavailable = apperror.ServiceUnavailable(
		"OLLAMA_CLOUD_USAGE_UNAVAILABLE", "Ollama Cloud usage is unavailable",
	)
	ErrOllamaCloudUsageProviderInvalid = apperror.BadRequest(
		"OLLAMA_CLOUD_USAGE_PROVIDER_INVALID", "provider must be an OpenAI or Anthropic API key provider using https://ollama.com",
	)
	ErrOllamaCloudUsageSessionRequired = apperror.BadRequest(
		"OLLAMA_CLOUD_USAGE_SESSION_REQUIRED", "an Ollama web session must be configured first",
	)
	ErrOllamaCloudUsageEncryptionKey = apperror.BadRequest(
		"OLLAMA_CLOUD_USAGE_ENCRYPTION_KEY_NOT_CONFIGURED", "cannot store an Ollama web session without a fixed TOTP_ENCRYPTION_KEY",
	)
	ErrOllamaCloudUsageIdentityChanged = apperror.Conflict(
		"OLLAMA_CLOUD_USAGE_IDENTITY_CHANGED", "provider identity or Ollama web session changed during refresh; retry",
	)
	ErrOllamaCloudUsageRefreshRateLimited = apperror.TooManyRequests(
		"OLLAMA_CLOUD_USAGE_REFRESH_RATE_LIMITED", "Ollama Cloud usage can be refreshed manually once every 30 seconds",
	)
)

// OllamaUsageFetchInput 保存出站用量查询参数。
type OllamaUsageFetchInput struct {
	ProviderID  int64
	Concurrency int
	ProxyURL    string `json:"-"`
	Cookie      string `json:"-"`
	ObservedAt  time.Time
}

type OllamaUsageObservation = usageview.OllamaUsageObservation

// OllamaCloudUsageSettings 控制可选的请求驱动刷新任务。
//
// IntervalMinutes 是最大等待上限：模型请求持续到达并不断推迟尾随防抖时，
// 超过该时长会强制刷新。DebounceMinutes 是分组最近一次请求后的静默期。
type OllamaCloudUsageSettings struct {
	Enabled         bool `json:"enabled"`
	IntervalMinutes int  `json:"interval_minutes"` // 请求持续到达时的最大等待时间
	DebounceMinutes int  `json:"debounce_minutes"` // 最近一次请求后的尾随静默期
}

type OllamaCloudUsageData = usageview.OllamaCloudUsageData

// OllamaCloudUsageSnapshot 是保存在提供商 Extra 中的用量观测。
// NextRefreshAt 随快照持久化。ok 状态下它表示最大等待时间，自动刷新由分组活动时间与防抖、最大等待共同决定。
// failed 或 unauthorized 状态下它表示 Retry-After 或指数退避计算的最早重试时间，实际到期取它与 activityDue 的较晚值。
type OllamaCloudUsageSnapshot struct {
	Status        string                `json:"status"`
	Data          *OllamaCloudUsageData `json:"data,omitempty"`
	FetchedAt     *time.Time            `json:"fetched_at,omitempty"`
	LastAttemptAt time.Time             `json:"last_attempt_at"`
	NextRefreshAt time.Time             `json:"next_refresh_at"`
	FailureCount  int                   `json:"failure_count,omitempty"`
	HTTPStatus    int                   `json:"http_status,omitempty"`
	LastError     string                `json:"last_error,omitempty"`
}

// OllamaCloudUsageState 是向管理员暴露的专用 DTO。
type OllamaCloudUsageState struct {
	ProviderID              int64                     `json:"provider_id"`
	Eligible                bool                      `json:"eligible"`
	Configured              bool                      `json:"configured"`
	AutoRefreshEnabled      bool                      `json:"auto_refresh_enabled"`
	EncryptionKeyConfigured bool                      `json:"encryption_key_configured"`
	Snapshot                *OllamaCloudUsageSnapshot `json:"snapshot,omitempty"`
}
