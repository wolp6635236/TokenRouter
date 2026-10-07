package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/egress"
	"github.com/TokenFlux/TokenRouter/internal/routing/accessview"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/routing/modelmap"
)

// Record 保存提供商的配置和运行数据，对外调用使用对应的快照。
// Credentials 使用 JSON 忽略标记，普通日志输出提供商 ID。
type Record struct {
	Proxy                   *egress.Proxy `json:"-"`
	Groups                  []*accessview.GroupConfig
	ProviderGroups          []GroupMembership
	ID                      int64
	Name                    string
	Notes                   *string
	Platform                string
	Type                    string
	Credentials             map[string]any `json:"-"`
	Extra                   map[string]any `json:"-"`
	ProxyID                 *int64
	ProxyFallbackOriginID   *int64
	ProxyFallbackOriginName *string
	Concurrency             int
	Priority                int
	RateMultiplier          *float64
	LoadFactor              *int
	Status                  string
	ErrorMessage            string
	LastUsedAt              *time.Time
	ExpiresAt               *time.Time
	AutoPauseOnExpired      bool
	CreatedAt               time.Time
	UpdatedAt               time.Time
	Schedulable             bool
	RateLimitedAt           *time.Time
	RateLimitResetAt        *time.Time
	OverloadUntil           *time.Time
	TempUnschedulableUntil  *time.Time
	TempUnschedulableReason string
	QuotaAutoPaused         bool `json:"-"`
	SessionWindowStart      *time.Time
	SessionWindowEnd        *time.Time
	SessionWindowStatus     string
	ParentProviderID        *int64
	QuotaDimension          string
	GroupIDs                []int64
	Now                     func() time.Time                     `json:"-"`
	LoadLocation            func(string) (*time.Location, error) `json:"-"`
}

func (r *Record) now() time.Time {
	if r != nil && r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Record) String() string {
	if r == nil {
		return "provider <nil>"
	}
	return fmt.Sprintf("provider (id=%d)", r.ID)
}
func (r *Record) GoString() string { return r.String() }

const OpenAIWorkloadCapabilitiesCredentialKey = "openai_workload_capabilities"

const (
	GeminiProviderTypeCredentialKey = "provider_type"
	GeminiProviderTypeThirdParty    = "third_party"
	GeminiOfficialAPIHost           = "generativelanguage.googleapis.com"
)

// GrokMediaEligibleExtraKey 是 providers.extra 中可选的提供商级覆盖：true 强制允许
// 媒体调度，false 禁用，缺失或 null 时使用上游观测自动判断。
const GrokMediaEligibleExtraKey = "grok_media_eligible"

const (
	OpenAIAuthModePersonalAccessToken = "personalAccessToken"
	OpenAIAuthModeCredentialKey       = "auth_mode"
	OpenAIAuthModeLegacyCredentialKey = "openai_auth_mode"
)

type TempUnschedulableRule struct {
	ErrorCode       int      `json:"error_code"`
	Keywords        []string `json:"keywords"`
	DurationMinutes int      `json:"duration_minutes"`
	Description     string   `json:"description"`
}

const (
	// OpenAICompactModeForceOn 表示管理员启用对应压缩能力。
	OpenAICompactModeForceOn = "force_on"
	// OpenAICompactModeForceOff 表示管理员关闭对应压缩能力。
	OpenAICompactModeForceOff            = "force_off"
	OpenAINativeCompactionV2ModeExtraKey = "openai_native_compaction_v2_mode"
)

const (
	DefaultPoolModeRetryCount = 3
	MaxPoolModeRetryCount     = 10
)

// DefaultPoolModeRetryableStatusCodes 池模式下默认触发同提供商重试的状态码。
// Provider.Credentials 缺少 pool_mode_retry_status_codes 时使用。
var DefaultPoolModeRetryableStatusCodes = []int{401, 403, 429}

const (
	OpenAIWSIngressModeOff         = "off"
	OpenAIWSIngressModeShared      = "shared"
	OpenAIWSIngressModeDedicated   = "dedicated"
	OpenAIWSIngressModeCtxPool     = "ctx_pool"
	OpenAIWSIngressModePassthrough = "passthrough"
	OpenAIWSIngressModeHTTPBridge  = "http_bridge"
)

const (
	// OpenAIOAuthClientPolicyAny 表示 OpenAI OAuth 提供商允许任意客户端访问。
	OpenAIOAuthClientPolicyAny = "any"
	// OpenAIOAuthClientPolicyCodexOnly 表示仅允许官方 Codex 客户端访问。
	OpenAIOAuthClientPolicyCodexOnly = "codex_only"
	// OpenAIOAuthClientPolicyTLSRouterMatchedOnly 表示仅允许 TLS 路由器命中的 UA 访问。
	OpenAIOAuthClientPolicyTLSRouterMatchedOnly = "tls_router_matched_only"
)

// Status constants
const (
	StatusActive   = "active"
	StatusDisabled = "disabled"
	StatusError    = "error"
)

// Platform constants
const (
	PlatformAnthropic   = capability.PlatformAnthropic
	PlatformOpenAI      = capability.PlatformOpenAI
	PlatformGemini      = capability.PlatformGemini
	PlatformAntigravity = capability.PlatformAntigravity
	PlatformQoder       = capability.PlatformQoder
	PlatformGrok        = capability.PlatformGrok
	PlatformKimi        = capability.PlatformKimi
	PlatformZhipu       = capability.PlatformZhipu
	PlatformDeepseek    = capability.PlatformDeepseek
)

// 提供商接入模式（国产供应商）：按量付费 vs Coding Plan。
const (
	ProviderModePayG   = "payg"
	ProviderModeCoding = "coding"
)

// 上游 API 协议（国产供应商）：决定转发端点与格式，与接入模式正交。
const (
	APIProtocolChatCompletions = "chat_completions"
	APIProtocolAnthropic       = "anthropic"
	APIProtocolResponses       = "responses"
	APIProtocolAdaptive        = "adaptive"
)

// 国产 OpenAI 兼容供应商各模式的默认 base_url。
// 与前端 credentialsBuilder.ts 中的预设保持一致。
const (
	DefaultKimiPayGBaseURL    = "https://api.moonshot.cn/v1"
	DefaultKimiCodingBaseURL  = "https://api.kimi.com/coding/v1"
	DefaultZhipuPayGBaseURL   = "https://open.bigmodel.cn/api/paas/v4"
	DefaultZhipuCodingBaseURL = "https://open.bigmodel.cn/api/coding/paas/v4"
	DefaultDeepseekBaseURL    = "https://api.deepseek.com"
)

// 国产供应商 Anthropic 协议端点的默认 base_url（上游路径为 {base}/v1/messages）。
// 与前端 credentialsBuilder.ts 中的预设保持一致。
const (
	DefaultKimiPayGAnthropicBaseURL   = "https://api.moonshot.cn/anthropic"
	DefaultKimiCodingAnthropicBaseURL = "https://api.kimi.com/coding"
	DefaultZhipuAnthropicBaseURL      = "https://open.bigmodel.cn/api/anthropic"
	DefaultDeepseekAnthropicBaseURL   = "https://api.deepseek.com/anthropic"
)

// Provider type constants
const (
	ProviderTypeOAuth          = capability.ProviderTypeOAuth          // OAuth类型提供商（full scope: profile + inference）
	ProviderTypeSetupToken     = capability.ProviderTypeSetupToken     // Setup Token类型提供商（inference only scope）
	ProviderTypeAPIKey         = capability.ProviderTypeAPIKey         // API Key类型提供商
	ProviderTypeUpstream       = capability.ProviderTypeUpstream       // 上游透传类型提供商（通过 Base URL + API Key 连接上游）
	ProviderTypeBedrock        = capability.ProviderTypeBedrock        // AWS Bedrock 类型提供商（通过 SigV4 签名或 API Key 连接 Bedrock，由 credentials.auth_mode 区分）
	ProviderTypeServiceAccount = capability.ProviderTypeServiceAccount // Google Service Account 类型提供商（用于 Vertex AI）
	ProviderTypeCosy           = capability.ProviderTypeCosy           // Qoder COSY 协议提供商
)

// QuotaDimension constants for spark shadow providers.
const (
	QuotaDimensionGlobal = "global"
	QuotaDimensionSpark  = "spark"
)

const (
	AntigravityPrivacySet    = "privacy_set"
	AntigravityPrivacyFailed = "privacy_set_failed"
)

const (
	PrivacyModeTrainingOff = "training_off"
	PrivacyModeFailed      = "training_set_failed"
	PrivacyModeCFBlocked   = "training_set_cf_blocked"
)

type OpenAIImagesCapability string

const (
	OpenAIImagesCapabilityBasic  OpenAIImagesCapability = "images-basic"
	OpenAIImagesCapabilityNative OpenAIImagesCapability = "images-native"
)

func IsOpenAIPersonalAccessTokenAuthMode(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "personalaccesstoken", "personal_access_token":
		return true
	default:
		return false
	}
}

func (a *Record) IsActive() bool {
	return a.Status == StatusActive
}

// BillingRateMultiplier 返回提供商计费倍率。
// - nil 表示未配置/旧缓存缺字段，按 1.0 处理
// - 允许 0，表示该提供商计费为 0
// - 负数属于非法数据，出于安全考虑按 1.0 处理
func (a *Record) BillingRateMultiplier() float64 {
	if a == nil || a.RateMultiplier == nil {
		return 1.0
	}
	if *a.RateMultiplier < 0 {
		return 1.0
	}
	return *a.RateMultiplier
}

func (a *Record) EffectiveLoadFactor() int {
	if a == nil {
		return 1
	}
	if a.LoadFactor != nil && *a.LoadFactor > 0 {
		return *a.LoadFactor
	}
	if a.Concurrency > 0 {
		return a.Concurrency
	}
	return 1
}

func (a *Record) IsSchedulable() bool {
	if !a.IsActive() || !a.Schedulable {
		return false
	}
	now := a.now()
	if a.AutoPauseOnExpired && a.ExpiresAt != nil && !now.Before(*a.ExpiresAt) {
		return false
	}
	if a.OverloadUntil != nil && now.Before(*a.OverloadUntil) {
		return false
	}
	if a.RateLimitResetAt != nil && now.Before(*a.RateLimitResetAt) {
		return false
	}
	if a.TempUnschedulableUntil != nil && now.Before(*a.TempUnschedulableUntil) {
		return false
	}
	if a.IsAPIKeyOrBedrock() && a.IsQuotaExceeded() {
		return false
	}
	return true
}

// IsCredentialUsableForShadow 判断母提供商的凭据和传输是否可供影子使用，nil 或非 active 提供商返回 false。
// 开启到期暂停时，ExpiresAt 需要晚于当前时间；缺省 ExpiresAt 时跳过到期检查。
// TempUnschedulableUntil 为空或当前时间已到达截止时刻时，临时冷却检查通过。
// OpenAI 的 401、token 刷新耗尽及代理或传输故障会设置临时冷却，影子共用母提供商的 token 和代理。
// 影子使用自己的 Spark 配额窗口和 Schedulable 开关，母提供商的全局限流与过载由其自身调度检查处理。
func (a *Record) IsCredentialUsableForShadow() bool {
	if a == nil || !a.IsActive() {
		return false
	}
	now := a.now()
	if a.AutoPauseOnExpired && a.ExpiresAt != nil && !now.Before(*a.ExpiresAt) {
		return false
	}
	if a.TempUnschedulableUntil != nil && now.Before(*a.TempUnschedulableUntil) {
		return false
	}
	return true
}

func (a *Record) IsRateLimited() bool {
	if a.RateLimitResetAt == nil {
		return false
	}
	return a.now().Before(*a.RateLimitResetAt)
}

func (a *Record) IsOAuth() bool {
	return a.Type == ProviderTypeOAuth || a.Type == ProviderTypeSetupToken
}

// IsPrivacySet 检查提供商的 privacy 是否已成功设置。
// OpenAI: privacy_mode == "training_off"
// Antigravity: privacy_mode == "privacy_set"
// 其他平台: 无 privacy 概念，始终返回 true
func (a *Record) IsPrivacySet() bool {
	switch a.Platform {
	case PlatformOpenAI:
		return a.GetExtraString("privacy_mode") == PrivacyModeTrainingOff
	case PlatformAntigravity:
		return a.GetExtraString("privacy_mode") == AntigravityPrivacySet
	default:
		return true
	}
}

func (a *Record) IsGemini() bool {
	return a.Platform == PlatformGemini
}

func (a *Record) IsGrok() bool {
	return a.Platform == PlatformGrok
}

func (a *Record) IsGrokOAuth() bool {
	return a.IsGrok() && a.Type == ProviderTypeOAuth
}

// IsCNProvider 报告是否为国产 OpenAI 兼容供应商（kimi/zhipu/deepseek）。
func (a *Record) IsCNProvider() bool {
	return a != nil && IsCNProvider(a.Platform)
}

// IsOpenAICompatible 报告提供商是否走 OpenAI 网关（OpenAI 协议族）。
// openai/grok 原生走 OpenAI 网关；kimi/zhipu/deepseek 同为 OpenAI Chat Completions
// 兼容上游，也经 OpenAI 网关转发。
func (a *Record) IsOpenAICompatible() bool {
	return a != nil && (a.Platform == PlatformOpenAI || a.Platform == PlatformGrok ||
		a.Platform == PlatformKimi || a.Platform == PlatformZhipu || a.Platform == PlatformDeepseek)
}

func (a *Record) GeminiOAuthType() string {
	if a.Platform != PlatformGemini || a.Type != ProviderTypeOAuth {
		return ""
	}
	oauthType := strings.TrimSpace(a.GetCredential("oauth_type"))
	if oauthType == "" && strings.TrimSpace(a.GetCredential("project_id")) != "" {
		return "code_assist"
	}
	return oauthType
}

func (a *Record) GeminiTierID() string {
	tierID := strings.TrimSpace(a.GetCredential("tier_id"))
	return tierID
}

// IsGeminiThirdPartyProvider 判断 Gemini API Key 是否通过第三方提供商接入。
// 该标记用于本地官方配额模拟，Gemini 请求仍按提供商类型认证和转发。
func (a *Record) IsGeminiThirdPartyProvider() bool {
	if a == nil || a.Platform != PlatformGemini || a.Type != ProviderTypeAPIKey {
		return false
	}
	return strings.EqualFold(a.GetCredential(GeminiProviderTypeCredentialKey), GeminiProviderTypeThirdParty)
}

// HasGeminiThirdPartyBaseURL 判断第三方 Gemini API Key 是否配置了非官方端点。
// 第三方来源不能依赖官方默认地址，否则会在关闭官方模拟配额的同时仍请求 Google 端点。
func (a *Record) HasGeminiThirdPartyBaseURL() bool {
	if !a.IsGeminiThirdPartyProvider() {
		return false
	}
	baseURL := strings.TrimSpace(a.GetCredential("base_url"))
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Hostname() == "" {
		return false
	}
	return !strings.EqualFold(parsed.Hostname(), GeminiOfficialAPIHost)
}

func (a *Record) IsGeminiCodeAssist() bool {
	if a.Platform != PlatformGemini || a.Type != ProviderTypeOAuth {
		return false
	}
	oauthType := a.GeminiOAuthType()
	if oauthType == "" {
		return strings.TrimSpace(a.GetCredential("project_id")) != ""
	}
	return oauthType == "code_assist"
}

// IsGeminiGoogleOne 判断提供商是否使用旧版消费者 Gemini CLI / Code Assist OAuth 通道。
func (a *Record) IsGeminiGoogleOne() bool {
	return a.Platform == PlatformGemini && a.Type == ProviderTypeOAuth && a.GeminiOAuthType() == "google_one"
}

func (a *Record) CanGetUsage() bool {
	return a.Type == ProviderTypeOAuth
}

func (a *Record) GetCredential(key string) string {
	if a.Credentials == nil {
		return ""
	}
	v, ok := a.Credentials[key]
	if !ok || v == nil {
		return ""
	}

	// 支持多种类型（兼容历史数据中 expires_at 等字段可能是数字或字符串）
	switch val := v.(type) {
	case string:
		return val
	case json.Number:
		// GORM datatypes.JSONMap 使用 UseNumber() 解析，数字类型为 json.Number
		return val.String()
	case float64:
		// JSON 解析后数字默认为 float64
		return strconv.FormatInt(int64(val), 10)
	case int64:
		return strconv.FormatInt(val, 10)
	case int:
		return strconv.Itoa(val)
	default:
		return ""
	}
}

// GetCredentialAsTime 解析凭证中的时间戳字段，支持多种格式
// 兼容以下格式：
//   - RFC3339 字符串: "2025-01-01T00:00:00Z"
//   - Unix 时间戳字符串: "1735689600"
//   - Unix 时间戳数字: 1735689600 (float64/int64/json.Number)
func (a *Record) GetCredentialAsTime(key string) *time.Time {
	s := a.GetCredential(key)
	if s == "" {
		return nil
	}
	// 尝试 RFC3339 格式
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return &t
	}
	// 尝试 Unix 时间戳（纯数字字符串）
	if ts, err := strconv.ParseInt(s, 10, 64); err == nil {
		t := time.Unix(ts, 0)
		return &t
	}
	return nil
}

// GetCredentialAsInt64 解析凭证中的 int64 字段
// 用于读取 _token_version 等内部字段
func (a *Record) GetCredentialAsInt64(key string) int64 {
	if a == nil || a.Credentials == nil {
		return 0
	}
	val, ok := a.Credentials[key]
	if !ok || val == nil {
		return 0
	}
	switch v := val.(type) {
	case int64:
		return v
	case float64:
		return int64(v)
	case int:
		return int64(v)
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return i
		}
	case string:
		if i, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
			return i
		}
	}
	return 0
}

func (a *Record) IsTempUnschedulableEnabled() bool {
	if a.Credentials == nil {
		return false
	}
	raw, ok := a.Credentials["temp_unschedulable_enabled"]
	if !ok || raw == nil {
		return false
	}
	enabled, ok := raw.(bool)
	return ok && enabled
}

func (a *Record) GetTempUnschedulableRules() []TempUnschedulableRule {
	if a.Credentials == nil {
		return nil
	}
	raw, ok := a.Credentials["temp_unschedulable_rules"]
	if !ok || raw == nil {
		return nil
	}

	arr, ok := raw.([]any)
	if !ok {
		return nil
	}

	rules := make([]TempUnschedulableRule, 0, len(arr))
	for _, item := range arr {
		entry, ok := item.(map[string]any)
		if !ok || entry == nil {
			continue
		}

		rule := TempUnschedulableRule{
			ErrorCode:       ParseExtraInt(entry["error_code"]),
			Keywords:        ParseTempUnschedStrings(entry["keywords"]),
			DurationMinutes: ParseExtraInt(entry["duration_minutes"]),
			Description:     ParseTempUnschedString(entry["description"]),
		}

		if rule.ErrorCode <= 0 || rule.DurationMinutes <= 0 || len(rule.Keywords) == 0 {
			continue
		}

		rules = append(rules, rule)
	}

	return rules
}

func ParseTempUnschedString(value any) string {
	s, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

func ParseTempUnschedStrings(value any) []string {
	if value == nil {
		return nil
	}

	var raw []string
	switch v := value.(type) {
	case []string:
		raw = v
	case []any:
		raw = make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				raw = append(raw, s)
			}
		}
	default:
		return nil
	}

	out := make([]string, 0, len(raw))
	for _, item := range raw {
		s := strings.TrimSpace(item)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func NormalizeProviderNotes(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func StringMappingFromRaw(raw any) map[string]string {
	switch mapping := raw.(type) {
	case map[string]any:
		if len(mapping) == 0 {
			return nil
		}
		result := make(map[string]string, len(mapping))
		for key, value := range mapping {
			if str, ok := value.(string); ok {
				result[key] = str
			}
		}
		if len(result) == 0 {
			return nil
		}
		return result
	case map[string]string:
		if len(mapping) == 0 {
			return nil
		}
		result := make(map[string]string, len(mapping))
		for key, value := range mapping {
			result[key] = value
		}
		return result
	default:
		return nil
	}
}

// ResolveRequestedModelInMapping 按精确名称和末尾通配符查找模型映射。
func ResolveRequestedModelInMapping(mapping map[string]string, requestedModel string) (mappedModel string, matched bool) {
	return modelmap.Resolve(mapping, requestedModel)
}

// ExtractFinalModelWhitelist 从 model_mapping 中提取“最终模型白名单”。
// 约定：只有精确自映射（from == to，且不含通配符）的条目才算白名单。
// 这样既能兼容历史上把白名单持久化为 key=value 的做法，又不会把普通映射规则误判成白名单。
func ExtractFinalModelWhitelist(mapping map[string]string) map[string]struct{} {
	if len(mapping) == 0 {
		return nil
	}
	whitelist := make(map[string]struct{})
	for rawFrom, rawTo := range mapping {
		if strings.Contains(rawFrom, "*") {
			continue
		}
		from := strings.TrimSpace(rawFrom)
		to := strings.TrimSpace(rawTo)
		if from == "" || to == "" || from != to {
			continue
		}
		whitelist[to] = struct{}{}
	}
	if len(whitelist) == 0 {
		return nil
	}
	return whitelist
}

// ExtractExplicitFinalModelWhitelist 从独立的 model_whitelist 字段提取最终模型白名单。
// 支持精确名称和末尾通配符；非法的中间通配符仍被忽略。
func ExtractExplicitFinalModelWhitelist(rawWhitelist any) map[string]struct{} {
	if rawWhitelist == nil {
		return nil
	}
	values := make([]string, 0)
	switch typed := rawWhitelist.(type) {
	case []string:
		values = append(values, typed...)
	case []any:
		for _, raw := range typed {
			if raw == nil {
				continue
			}
			values = append(values, strings.TrimSpace(fmt.Sprint(raw)))
		}
	default:
		return nil
	}
	whitelist := make(map[string]struct{})
	for _, rawModel := range values {
		model := strings.TrimSpace(rawModel)
		if model == "" || strings.Contains(strings.TrimSuffix(model, "*"), "*") {
			continue
		}
		whitelist[model] = struct{}{}
	}
	if len(whitelist) == 0 {
		return nil
	}
	return whitelist
}

// ResolveFinalModelWhitelist 优先读取独立的 model_whitelist 字段；
// 若该字段不存在，则回退到旧版“自映射即白名单”的兼容解析。
func ResolveFinalModelWhitelist(platform string, credentials map[string]any, mapping map[string]string) (map[string]struct{}, bool) {
	if credentials != nil {
		if rawWhitelist, exists := credentials["model_whitelist"]; exists {
			return ExtractExplicitFinalModelWhitelist(rawWhitelist), true
		}
	}
	if platform == PlatformQoder {
		return nil, false
	}
	return ExtractFinalModelWhitelist(mapping), false
}

// GetOpenAICompactMode 返回管理员选择的旧版压缩开关。
// 缺省为开启，写入时规范化历史输入。
func (a *Record) GetOpenAICompactMode() string {
	if a == nil || !a.IsOpenAI() {
		return OpenAICompactModeForceOff
	}
	mode, _ := a.Extra["openai_compact_mode"].(string)
	if strings.EqualFold(strings.TrimSpace(mode), OpenAICompactModeForceOff) {
		return OpenAICompactModeForceOff
	}
	return OpenAICompactModeForceOn
}

// AllowsOpenAICompact 判断管理员是否启用旧版压缩。
func (a *Record) AllowsOpenAICompact() bool {
	return a != nil && a.IsOpenAI() && a.GetOpenAICompactMode() == OpenAICompactModeForceOn
}

// GetOpenAINativeCompactionV2Mode 返回原生 V2 压缩的管理员开关。
func (a *Record) GetOpenAINativeCompactionV2Mode() string {
	if a == nil || !a.IsOpenAI() {
		return OpenAICompactModeForceOff
	}
	mode, _ := a.Extra[OpenAINativeCompactionV2ModeExtraKey].(string)
	if strings.EqualFold(strings.TrimSpace(mode), OpenAICompactModeForceOff) {
		return OpenAICompactModeForceOff
	}
	return OpenAICompactModeForceOn
}

// AllowsOpenAINativeCompactionV2 判断管理员是否启用原生 V2 压缩。
func (a *Record) AllowsOpenAINativeCompactionV2() bool {
	return a != nil && a.IsOpenAI() && a.GetOpenAINativeCompactionV2Mode() == OpenAICompactModeForceOn
}

// GetCompactModelMapping returns compact-only model remapping configuration.
// This mapping is intended for /responses/compact only and does not affect
// normal /responses traffic.
func (a *Record) GetCompactModelMapping() map[string]string {
	if a == nil || a.Credentials == nil {
		return nil
	}
	return StringMappingFromRaw(a.Credentials["compact_model_mapping"])
}

// ResolveCompactMappedModel resolves compact-only model remapping and reports
// whether a compact-specific mapping rule matched.
func (a *Record) ResolveCompactMappedModel(requestedModel string) (mappedModel string, matched bool) {
	mapping := a.GetCompactModelMapping()
	if len(mapping) == 0 {
		return requestedModel, false
	}
	if mappedModel, matched := ResolveRequestedModelInMapping(mapping, requestedModel); matched {
		return mappedModel, true
	}
	return requestedModel, false
}

func (a *Record) GetBaseURL() string {
	if a.Type != ProviderTypeAPIKey {
		return ""
	}
	baseURL := a.GetCredential("base_url")
	if baseURL == "" {
		return "https://api.anthropic.com"
	}
	return baseURL
}

// GetGeminiBaseURL 返回 Gemini 兼容端点的 base URL。
func (a *Record) GetGeminiBaseURL(defaultBaseURL string) string {
	baseURL := strings.TrimSpace(a.GetCredential("base_url"))
	if baseURL == "" {
		return defaultBaseURL
	}
	return baseURL
}

func (a *Record) GetExtraString(key string) string {
	if a.Extra == nil {
		return ""
	}
	if v, ok := a.Extra[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func (a *Record) GetClaudeUserID() string {
	if v := strings.TrimSpace(a.GetExtraString("claude_user_id")); v != "" {
		return v
	}
	if v := strings.TrimSpace(a.GetExtraString("anthropic_user_id")); v != "" {
		return v
	}
	if v := strings.TrimSpace(a.GetCredential("claude_user_id")); v != "" {
		return v
	}
	if v := strings.TrimSpace(a.GetCredential("anthropic_user_id")); v != "" {
		return v
	}
	return ""
}

func MatchWildcard(pattern, str string) bool { return modelmap.Matches(pattern, str) }

func (a *Record) IsCustomErrorCodesEnabled() bool {
	if a.Type != ProviderTypeAPIKey || a.Credentials == nil {
		return false
	}
	if v, ok := a.Credentials["custom_error_codes_enabled"]; ok {
		if enabled, ok := v.(bool); ok {
			return enabled
		}
	}
	return false
}

// IsPoolMode 检查 API Key 提供商是否启用池模式。
// 上游错误默认交给池处理，管理员配置的错误策略优先，配置的重试状态码可在同一提供商上重试。
func (a *Record) IsPoolMode() bool {
	if !a.IsAPIKeyOrBedrock() || a.Credentials == nil {
		return false
	}
	if v, ok := a.Credentials["pool_mode"]; ok {
		if enabled, ok := v.(bool); ok {
			return enabled
		}
	}
	return false
}

// GetPoolModeRetryCount 返回池模式同提供商重试次数。
// 未配置或配置非法时回退为默认值 3；小于 0 按 0 处理；过大则截断到 10。
func (a *Record) GetPoolModeRetryCount() int {
	if a == nil || !a.IsPoolMode() || a.Credentials == nil {
		return DefaultPoolModeRetryCount
	}
	raw, ok := a.Credentials["pool_mode_retry_count"]
	if !ok || raw == nil {
		return DefaultPoolModeRetryCount
	}
	count := ParsePoolModeRetryCount(raw)
	if count < 0 {
		return 0
	}
	if count > MaxPoolModeRetryCount {
		return MaxPoolModeRetryCount
	}
	return count
}

func ParsePoolModeRetryCount(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return int(i)
		}
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return i
		}
	}
	return DefaultPoolModeRetryCount
}

// IsPoolModeRetryableStatus 池模式下应触发同提供商重试的状态码（默认列表）。
func IsPoolModeRetryableStatus(statusCode int) bool {
	for _, c := range DefaultPoolModeRetryableStatusCodes {
		if c == statusCode {
			return true
		}
	}
	return false
}

// GetPoolModeRetryStatusCodes 返回提供商自定义的池模式同提供商重试状态码列表。
//
// 返回值含义：
//   - nil：未配置 → 调用方应回退到默认值 [401, 403, 429]
//
// - 长度为 0 的切片表示关闭按状态码触发的同提供商重试。
//   - 非空切片：去重、过滤为合法 HTTP 状态码（100-599）后的覆盖列表
func (a *Record) GetPoolModeRetryStatusCodes() []int {
	if a == nil || a.Credentials == nil {
		return nil
	}
	raw, ok := a.Credentials["pool_mode_retry_status_codes"]
	if !ok || raw == nil {
		return nil
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	seen := make(map[int]struct{}, len(arr))
	codes := make([]int, 0, len(arr))
	for _, v := range arr {
		var code int
		switch n := v.(type) {
		case float64:
			code = int(n)
		case int:
			code = n
		case int64:
			code = int(n)
		case json.Number:
			i, err := n.Int64()
			if err != nil {
				continue
			}
			code = int(i)
		case string:
			i, err := strconv.Atoi(strings.TrimSpace(n))
			if err != nil {
				continue
			}
			code = i
		default:
			continue
		}
		if code < 100 || code > 599 {
			continue
		}
		if _, exists := seen[code]; exists {
			continue
		}
		seen[code] = struct{}{}
		codes = append(codes, code)
	}
	sort.Ints(codes)
	return codes
}

// IsPoolModeRetryableStatus 在提供商上下文中判断给定状态码是否应触发同提供商重试。
// 若提供商未配置 pool_mode_retry_status_codes，则回退到默认列表。
func (a *Record) IsPoolModeRetryableStatus(statusCode int) bool {
	codes := a.GetPoolModeRetryStatusCodes()
	if codes == nil {
		return IsPoolModeRetryableStatus(statusCode)
	}
	for _, c := range codes {
		if c == statusCode {
			return true
		}
	}
	return false
}

func (a *Record) GetCustomErrorCodes() []int {
	if a.Credentials == nil {
		return nil
	}
	raw, ok := a.Credentials["custom_error_codes"]
	if !ok || raw == nil {
		return nil
	}
	if arr, ok := raw.([]any); ok {
		result := make([]int, 0, len(arr))
		for _, v := range arr {
			if f, ok := v.(float64); ok {
				result = append(result, int(f))
			}
		}
		return result
	}
	return nil
}

func (a *Record) ShouldHandleErrorCode(statusCode int) bool {
	if !a.IsCustomErrorCodesEnabled() {
		return true
	}
	codes := a.GetCustomErrorCodes()
	if len(codes) == 0 {
		return true
	}
	for _, code := range codes {
		if code == statusCode {
			return true
		}
	}
	return false
}

func (a *Record) IsInterceptWarmupEnabled() bool {
	if a.Credentials == nil {
		return false
	}
	if v, ok := a.Credentials["intercept_warmup_requests"]; ok {
		if enabled, ok := v.(bool); ok {
			return enabled
		}
	}
	return false
}

func (a *Record) IsBedrock() bool {
	return a.Platform == PlatformAnthropic && a.Type == ProviderTypeBedrock
}

func (a *Record) IsBedrockAPIKey() bool {
	return a.IsBedrock() && a.GetCredential("auth_mode") == "apikey"
}

// IsAPIKeyOrBedrock 返回提供商类型是否支持配额和池模式等特性
func (a *Record) IsAPIKeyOrBedrock() bool {
	return a.Type == ProviderTypeAPIKey || a.Type == ProviderTypeBedrock
}

func (a *Record) IsOpenAI() bool {
	return a.Platform == PlatformOpenAI
}

func (a *Record) IsAnthropic() bool {
	return a.Platform == PlatformAnthropic
}

func (a *Record) IsQoder() bool {
	return a.Platform == PlatformQoder
}

func (a *Record) IsQoderCosy() bool {
	return a.IsQoder() && a.Type == ProviderTypeCosy
}

func (a *Record) IsOpenAIOAuth() bool {
	return a.IsOpenAI() && a.Type == ProviderTypeOAuth
}

// IsOpenAIOAuthLike reports OpenAI credentials that use the ChatGPT/Codex
// inference protocol. Setup tokens share that forwarding contract but do not
// participate in the refreshable OAuth credential lifecycle.
func (a *Record) IsOpenAIOAuthLike() bool {
	return a != nil && a.IsOpenAI() && (a.Type == ProviderTypeOAuth || a.Type == ProviderTypeSetupToken)
}

// UsesOpenAICodexProtocol preserves legacy OpenAI gateway OAuth routing for
// providers whose platform is implicit, while adding OpenAI SetupToken.
func (a *Record) UsesOpenAICodexProtocol() bool {
	return a != nil && (a.Type == ProviderTypeOAuth || a.IsOpenAIOAuthLike())
}

func (a *Record) IsOpenAIChatGPTSubscription() bool {
	if !a.IsOpenAIOAuth() {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(a.GetCredential("plan_type"))) {
	case "", "free", "abnormal":
		return false
	default:
		return true
	}
}

// IsOpenAIPersonalAccessToken 判断 OpenAI OAuth 提供商是否使用 Codex PAT 认证模式。
func (a *Record) IsOpenAIPersonalAccessToken() bool {
	if a == nil || !a.IsOpenAIOAuth() {
		return false
	}
	return IsOpenAIPersonalAccessTokenAuthMode(a.GetCredential(OpenAIAuthModeCredentialKey)) ||
		IsOpenAIPersonalAccessTokenAuthMode(a.GetCredential(OpenAIAuthModeLegacyCredentialKey))
}

func (a *Record) IsOpenAIApiKey() bool {
	return a.IsOpenAI() && a.Type == ProviderTypeAPIKey
}

// GetProviderMode 返回国产平台提供商的接入模式（payg / coding）。历史提供商缺少字段时
// 按 payg 读取；非国产供应商返回空串。存储于 credentials["provider_mode"]。
func (a *Record) GetProviderMode() string {
	if a == nil || !a.IsCNProvider() {
		return ""
	}
	mode := strings.TrimSpace(a.GetCredential("provider_mode"))
	if mode == ProviderModePayG || mode == ProviderModeCoding {
		return mode
	}
	return ProviderModePayG
}

// IsCodingPlan 报告提供商是否为 Coding Plan 模式（用于滚动用量窗口冷却）。
func (a *Record) IsCodingPlan() bool {
	return a.GetProviderMode() == ProviderModeCoding
}

// SupportsNativeCNResponses 报告该国产供应商是否提供原生 Responses 端点。
// DeepSeek 官方为 /responses（无 /v1）；Kimi 按量付费与 Coding Plan 均为
// /v1/responses（moonshot.cn / kimi.com/coding）。
func (a *Record) SupportsNativeCNResponses() bool {
	if a == nil {
		return false
	}
	switch a.Platform {
	case PlatformDeepseek, PlatformKimi:
		return true
	default:
		return false
	}
}

func (a *Record) DefaultCNProtocolBaseURL(protocol string) string {
	switch protocol {
	case APIProtocolAnthropic:
		switch a.Platform {
		case PlatformKimi:
			if a.GetProviderMode() == ProviderModeCoding {
				return DefaultKimiCodingAnthropicBaseURL
			}
			return DefaultKimiPayGAnthropicBaseURL
		case PlatformZhipu:
			return DefaultZhipuAnthropicBaseURL
		case PlatformDeepseek:
			return DefaultDeepseekAnthropicBaseURL
		}
	case APIProtocolChatCompletions, APIProtocolResponses:
		switch a.Platform {
		case PlatformKimi:
			if a.GetProviderMode() == ProviderModeCoding {
				return DefaultKimiCodingBaseURL
			}
			return DefaultKimiPayGBaseURL
		case PlatformZhipu:
			if a.GetProviderMode() == ProviderModeCoding {
				return DefaultZhipuCodingBaseURL
			}
			return DefaultZhipuPayGBaseURL
		case PlatformDeepseek:
			return DefaultDeepseekBaseURL
		}
	}
	return ""
}

// IsDefaultCNAnthropicBaseURL 判断地址是否为内置官方端点，同时比较主机和路径。
func IsDefaultCNAnthropicBaseURL(baseURL string) bool {
	normalized := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	switch normalized {
	case DefaultKimiPayGAnthropicBaseURL,
		DefaultKimiCodingAnthropicBaseURL,
		DefaultZhipuAnthropicBaseURL,
		DefaultDeepseekAnthropicBaseURL:
		return true
	default:
		return false
	}
}

// StripCNAnthropicPathSuffix 将自定义中继的 Anthropic 协议根转换为同一中继的
// OpenAI 格式根，解析失败时返回输入值，由调用方的 URL 校验报告错误。
func StripCNAnthropicPathSuffix(baseURL string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return trimmed
	}
	path := strings.TrimRight(parsed.Path, "/")
	if path == "/anthropic" {
		parsed.Path = ""
		parsed.RawPath = ""
	} else if strings.HasSuffix(path, "/anthropic") {
		parsed.Path = strings.TrimSuffix(path, "/anthropic")
		parsed.RawPath = ""
	}
	return strings.TrimRight(parsed.String(), "/")
}

// GetCNAPIKey 返回国产 OpenAI 兼容提供商的 api_key 凭据（kimi/zhipu/deepseek）。
// 与 openai 的 GetOpenAIApiKey 区分：后者仅对 openai 平台返回。
func (a *Record) GetCNAPIKey() string {
	if a == nil || !a.IsCNProvider() {
		return ""
	}
	return a.GetCredential("api_key")
}

// GetCodingPlanProvider 根据提供商平台识别 Coding Plan 供应商。管理员可以使用自定义
// 中继地址，因此不得通过 URL 内容反推供应商身份。
func (a *Record) GetCodingPlanProvider() string {
	if a == nil || a.GetProviderMode() != ProviderModeCoding {
		return ""
	}
	switch a.Platform {
	case PlatformKimi:
		return PlatformKimi
	case PlatformZhipu:
		return PlatformZhipu
	default:
		return ""
	}
}

func (a *Record) GetOpenAIAccessToken() string {
	if !a.IsOpenAI() {
		return ""
	}
	return a.GetCredential("access_token")
}

func (a *Record) GetOpenAIRefreshToken() string {
	if !a.IsOpenAIOAuth() {
		return ""
	}
	return a.GetCredential("refresh_token")
}

func (a *Record) GetGrokAccessToken() string {
	if !a.IsGrok() {
		return ""
	}
	return a.GetCredential("access_token")
}

func (a *Record) GetGrokRefreshToken() string {
	if !a.IsGrokOAuth() {
		return ""
	}
	return a.GetCredential("refresh_token")
}

func (a *Record) GetOpenAIApiKey() string {
	if !a.IsOpenAIApiKey() {
		return ""
	}
	return a.GetCredential("api_key")
}

// GetOpenAIProtocolAPIKey 返回 OpenAI、Kimi、Zhipu 和 DeepSeek API Key 提供商的密钥。
// 转发鉴权和模型同步共用此方法，调度倍率与 WS 准入通过 IsOpenAIApiKey 判断 OpenAI 平台资格。
func (a *Record) GetOpenAIProtocolAPIKey() string {
	if a == nil {
		return ""
	}
	if a.IsCNProvider() {
		if a.Type != ProviderTypeAPIKey {
			return ""
		}
		return a.GetCredential("api_key")
	}
	return a.GetOpenAIApiKey()
}

func (a *Record) GetOpenAIUserAgent() string {
	if !a.IsOpenAI() {
		return ""
	}
	return a.GetCredential("user_agent")
}

func (a *Record) GetChatGPTAccountID() string {
	if !a.IsOpenAIOAuthLike() {
		return ""
	}
	return a.GetCredential("chatgpt_account_id")
}

// IsChatGPTAccountFedRAMP 读取 ChatGPT 提供商是否为 FedRAMP 环境。
func (a *Record) IsChatGPTAccountFedRAMP() bool {
	if !a.IsOpenAIOAuthLike() || a.Credentials == nil {
		return false
	}
	v, ok := a.Credentials["chatgpt_account_is_fedramp"]
	if !ok || v == nil {
		return false
	}
	switch value := v.(type) {
	case bool:
		return value
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(value))
		return err == nil && parsed
	case json.Number:
		parsed, err := strconv.ParseBool(value.String())
		return err == nil && parsed
	case float64:
		return value != 0
	case int:
		return value != 0
	case int64:
		return value != 0
	default:
		return false
	}
}

func (a *Record) GetOpenAIDeviceID() string {
	if !a.IsOpenAIOAuth() {
		return ""
	}
	return strings.TrimSpace(a.GetExtraString("openai_device_id"))
}

func GrokMediaEligibilityOverride(extra map[string]any) (bool, bool) {
	if extra == nil {
		return false, false
	}
	raw, exists := extra[GrokMediaEligibleExtraKey]
	if !exists || raw == nil {
		return false, false
	}
	value, ok := raw.(bool)
	return value, ok
}

func (a *Record) OpenAIWorkloadCapabilitySet() (map[string]bool, bool) {
	if a == nil || a.Credentials == nil {
		return nil, false
	}
	raw, found := a.Credentials[OpenAIWorkloadCapabilitiesCredentialKey]
	if !found || raw == nil {
		return nil, false
	}

	result := make(map[string]bool)
	add := func(value string) {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			return
		}
		result[value] = true
	}

	// OAuth 和 SetupToken 的空容器按未配置处理，API Key 空集合表示禁用。
	switch capabilities := raw.(type) {
	case []any:
		if len(capabilities) == 0 && a.IsOpenAIOAuthLike() {
			return nil, false
		}
		for _, item := range capabilities {
			if value, ok := item.(string); ok {
				add(value)
			}
		}
	case []string:
		if len(capabilities) == 0 && a.IsOpenAIOAuthLike() {
			return nil, false
		}
		for _, value := range capabilities {
			add(value)
		}
	case map[string]any:
		if len(capabilities) == 0 && a.IsOpenAIOAuthLike() {
			return nil, false
		}
		for key, value := range capabilities {
			enabled, ok := value.(bool)
			if ok && enabled {
				add(key)
			}
		}
	case map[string]bool:
		if len(capabilities) == 0 && a.IsOpenAIOAuthLike() {
			return nil, false
		}
		for key, enabled := range capabilities {
			if enabled {
				add(key)
			}
		}
	}

	return result, true
}

func (a *Record) SupportsOpenAIImageCapability(capability OpenAIImagesCapability) bool {
	if capability == "" {
		return true
	}
	if !a.IsOpenAI() {
		return false
	}
	switch capability {
	case OpenAIImagesCapabilityBasic, OpenAIImagesCapabilityNative:
		return a.Type == ProviderTypeOAuth || a.Type == ProviderTypeSetupToken || a.Type == ProviderTypeAPIKey
	default:
		return true
	}
}

// IsOveragesEnabled 检查 Antigravity 提供商是否启用 AI Credits 超量请求。
func (a *Record) IsOveragesEnabled() bool {
	if a.Platform != PlatformAntigravity {
		return false
	}
	if a.Extra == nil {
		return false
	}
	if v, ok := a.Extra["allow_overages"]; ok {
		if enabled, ok := v.(bool); ok {
			return enabled
		}
	}
	return false
}

// IsOpenAIPassthroughEnabled 返回 OpenAI 提供商是否启用"自动透传（仅替换认证）"。
//
// 新字段：providers.extra.openai_passthrough。
// 兼容字段：providers.extra.openai_oauth_passthrough（历史 OAuth 开关）。
// 字段缺失或类型不正确时，按 false（关闭）处理。
func (a *Record) IsOpenAIPassthroughEnabled() bool {
	if a == nil || !a.IsOpenAI() || a.Extra == nil {
		return false
	}
	if enabled, ok := a.Extra["openai_passthrough"].(bool); ok {
		return enabled
	}
	if enabled, ok := a.Extra["openai_oauth_passthrough"].(bool); ok {
		return enabled
	}
	return false
}

// IsOpenAIResponsesFlattenNamespacesEnabled 返回提供商级 Codex namespace 工具摊平开关。
// 字段 providers.extra.openai_responses_flatten_namespaces 缺省为 false，即原样保留。
// 该兼容开关仅对 OpenAI OAuth 提供商生效，用于仍不支持 namespace 的中转上游。
func (a *Record) IsOpenAIResponsesFlattenNamespacesEnabled() bool {
	if a == nil || !a.IsOpenAIOAuth() || a.Extra == nil {
		return false
	}
	enabled, ok := a.Extra["openai_responses_flatten_namespaces"].(bool)
	return ok && enabled
}

// IsOpenAIResponsesWebSocketV2Enabled 返回 OpenAI 提供商是否开启 Responses WebSocket v2。
//
// 分类型新字段：
// - OAuth 提供商：providers.extra.openai_oauth_responses_websockets_v2_enabled
// - API Key 提供商：providers.extra.openai_apikey_responses_websockets_v2_enabled
//
// 兼容字段：
// - providers.extra.responses_websockets_v2_enabled
// - providers.extra.openai_ws_enabled（历史开关）
//
// 优先级：
// 1. 按提供商类型读取分类型字段
// 2. 分类型字段缺失时，回退兼容字段
func (a *Record) IsOpenAIResponsesWebSocketV2Enabled() bool {
	if a == nil || !a.IsOpenAI() || a.Extra == nil {
		return false
	}
	if a.IsOpenAIOAuthLike() {
		if enabled, ok := a.Extra["openai_oauth_responses_websockets_v2_enabled"].(bool); ok {
			return enabled
		}
	}
	if a.IsOpenAIApiKey() {
		if enabled, ok := a.Extra["openai_apikey_responses_websockets_v2_enabled"].(bool); ok {
			return enabled
		}
	}
	if enabled, ok := a.Extra["responses_websockets_v2_enabled"].(bool); ok {
		return enabled
	}
	if enabled, ok := a.Extra["openai_ws_enabled"].(bool); ok {
		return enabled
	}
	return false
}

func NormalizeOpenAIWSIngressMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case OpenAIWSIngressModeOff:
		return OpenAIWSIngressModeOff
	case OpenAIWSIngressModeCtxPool:
		return OpenAIWSIngressModeCtxPool
	case OpenAIWSIngressModePassthrough:
		return OpenAIWSIngressModePassthrough
	case OpenAIWSIngressModeHTTPBridge:
		return OpenAIWSIngressModeHTTPBridge
	case OpenAIWSIngressModeShared:
		return OpenAIWSIngressModeShared
	case OpenAIWSIngressModeDedicated:
		return OpenAIWSIngressModeDedicated
	default:
		return ""
	}
}

func NormalizeOpenAIWSIngressDefaultMode(mode string) string {
	if normalized := NormalizeOpenAIWSIngressMode(mode); normalized != "" {
		if normalized == OpenAIWSIngressModeShared || normalized == OpenAIWSIngressModeDedicated {
			return OpenAIWSIngressModeCtxPool
		}
		return normalized
	}
	return OpenAIWSIngressModeCtxPool
}

// ResolveOpenAIResponsesWebSocketV2Mode 返回提供商在 WSv2 ingress 下的有效模式（off/ctx_pool/passthrough/http_bridge）。
//
// 优先级：
// 1. 分类型 mode 新字段（string）
// 2. 分类型 enabled 旧字段（bool）
// 3. 兼容 enabled 旧字段（bool）
// 4. defaultMode（非法时回退 ctx_pool）
func (a *Record) ResolveOpenAIResponsesWebSocketV2Mode(defaultMode string) string {
	resolvedDefault := NormalizeOpenAIWSIngressDefaultMode(defaultMode)
	if a == nil || !a.IsOpenAI() {
		return OpenAIWSIngressModeOff
	}
	if a.Extra == nil {
		return resolvedDefault
	}

	resolveModeString := func(key string) (string, bool) {
		raw, ok := a.Extra[key]
		if !ok {
			return "", false
		}
		mode, ok := raw.(string)
		if !ok {
			return "", false
		}
		normalized := NormalizeOpenAIWSIngressMode(mode)
		if normalized == "" {
			return "", false
		}
		return normalized, true
	}
	resolveBoolMode := func(key string) (string, bool) {
		raw, ok := a.Extra[key]
		if !ok {
			return "", false
		}
		enabled, ok := raw.(bool)
		if !ok {
			return "", false
		}
		if enabled {
			return OpenAIWSIngressModeCtxPool, true
		}
		return OpenAIWSIngressModeOff, true
	}

	if a.IsOpenAIOAuthLike() {
		if mode, ok := resolveModeString("openai_oauth_responses_websockets_v2_mode"); ok {
			return mode
		}
		if mode, ok := resolveBoolMode("openai_oauth_responses_websockets_v2_enabled"); ok {
			return mode
		}
	}
	if a.IsOpenAIApiKey() {
		if mode, ok := resolveModeString("openai_apikey_responses_websockets_v2_mode"); ok {
			return mode
		}
		if mode, ok := resolveBoolMode("openai_apikey_responses_websockets_v2_enabled"); ok {
			return mode
		}
	}
	if mode, ok := resolveBoolMode("responses_websockets_v2_enabled"); ok {
		return mode
	}
	if mode, ok := resolveBoolMode("openai_ws_enabled"); ok {
		return mode
	}
	// 兼容值 shared 和 dedicated 都转换为 ctx_pool。
	if resolvedDefault == OpenAIWSIngressModeShared || resolvedDefault == OpenAIWSIngressModeDedicated {
		return OpenAIWSIngressModeCtxPool
	}
	return resolvedDefault
}

// IsOpenAIWSForceHTTPEnabled 返回提供商级"强制 HTTP"开关。
// 字段：providers.extra.openai_ws_force_http。
func (a *Record) IsOpenAIWSForceHTTPEnabled() bool {
	if a == nil || !a.IsOpenAI() || a.Extra == nil {
		return false
	}
	enabled, ok := a.Extra["openai_ws_force_http"].(bool)
	return ok && enabled
}

// IsOpenAIWSAllowStoreRecoveryEnabled 返回提供商级 store 恢复开关。
// 字段：providers.extra.openai_ws_allow_store_recovery。
func (a *Record) IsOpenAIWSAllowStoreRecoveryEnabled() bool {
	if a == nil || !a.IsOpenAI() || a.Extra == nil {
		return false
	}
	enabled, ok := a.Extra["openai_ws_allow_store_recovery"].(bool)
	return ok && enabled
}

// IsOpenAIOAuthPassthroughEnabled 兼容旧接口，等价于 OAuth 提供商的 IsOpenAIPassthroughEnabled。
func (a *Record) IsOpenAIOAuthPassthroughEnabled() bool {
	return a != nil && a.IsOpenAIOAuth() && a.IsOpenAIPassthroughEnabled()
}

// IsAnthropicAPIKeyPassthroughEnabled 返回 Anthropic API Key 提供商是否启用"自动透传（仅替换认证）"。
// 字段：providers.extra.anthropic_passthrough。
// 字段缺失或类型不正确时，按 false（关闭）处理。
func (a *Record) IsAnthropicAPIKeyPassthroughEnabled() bool {
	if a == nil || a.Platform != PlatformAnthropic || a.Type != ProviderTypeAPIKey || a.Extra == nil {
		return false
	}
	enabled, ok := a.Extra["anthropic_passthrough"].(bool)
	return ok && enabled
}

// IsCodexCLIOnlyEnabled 返回 OpenAI OAuth 提供商是否启用"仅允许 Codex 官方客户端"。
// 新字段 openai_oauth_client_policy 优先；旧字段 providers.extra.codex_cli_only 仅用于兼容。
func (a *Record) IsCodexCLIOnlyEnabled() bool {
	return a.GetOpenAIOAuthClientPolicy() == OpenAIOAuthClientPolicyCodexOnly
}

// GetOpenAIOAuthClientPolicy 返回 OpenAI OAuth 提供商的客户端访问策略。
func (a *Record) GetOpenAIOAuthClientPolicy() string {
	if a == nil || !a.IsOpenAIOAuth() || a.Extra == nil {
		return OpenAIOAuthClientPolicyAny
	}
	if policy, ok := a.Extra["openai_oauth_client_policy"].(string); ok {
		switch strings.TrimSpace(policy) {
		case OpenAIOAuthClientPolicyCodexOnly:
			return OpenAIOAuthClientPolicyCodexOnly
		case OpenAIOAuthClientPolicyTLSRouterMatchedOnly:
			return OpenAIOAuthClientPolicyTLSRouterMatchedOnly
		case OpenAIOAuthClientPolicyAny:
			return OpenAIOAuthClientPolicyAny
		}
	}
	enabled, ok := a.Extra["codex_cli_only"].(bool)
	if ok && enabled {
		return OpenAIOAuthClientPolicyCodexOnly
	}
	return OpenAIOAuthClientPolicyAny
}

// IsOpenAIOAuthTLSRouterMatchedOnly 返回提供商是否仅允许 TLS 路由器命中的客户端。
func (a *Record) IsOpenAIOAuthTLSRouterMatchedOnly() bool {
	return a.GetOpenAIOAuthClientPolicy() == OpenAIOAuthClientPolicyTLSRouterMatchedOnly
}

// GetCodexCLIOnlyAllowedClients 返回 codex_cli_only 之上额外放行的命名客户端预设 ID 列表。
// 仅 OpenAI OAuth 提供商生效；缺失或类型不符时返回空。预设 ID 的具体匹配规则由
// openai 包的 registry 固化，配置只能引用预设键、不能自定义规则。
func (a *Record) GetCodexCLIOnlyAllowedClients() []string {
	if a == nil || !a.IsOpenAIOAuth() || a.Extra == nil {
		return nil
	}
	raw, ok := a.Extra["codex_cli_only_allowed_clients"]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		result := make([]string, 0, len(v))
		for _, s := range v {
			if strings.TrimSpace(s) != "" {
				result = append(result, s)
			}
		}
		return result
	case []any:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				result = append(result, s)
			}
		}
		return result
	}
	return nil
}

// IsAnthropicOAuthOrSetupToken 判断是否为 Anthropic OAuth 或 SetupToken 类型提供商
// 仅这两类提供商支持 5h 窗口额度控制和会话数量控制
func (a *Record) IsAnthropicOAuthOrSetupToken() bool {
	if a == nil {
		return false
	}
	return a.Platform == PlatformAnthropic && (a.Type == ProviderTypeOAuth || a.Type == ProviderTypeSetupToken)
}

// SupportsTLSFingerprint 返回提供商是否支持 TLS 指纹伪装。
// 当前支持 Anthropic OAuth/SetupToken、OpenAI OAuth 与 Qoder COSY。
func (a *Record) SupportsTLSFingerprint() bool {
	if a == nil {
		return false
	}
	if a.IsAnthropicOAuthOrSetupToken() {
		return true
	}
	return (a.Platform == PlatformOpenAI && a.Type == ProviderTypeOAuth) ||
		(a.IsCNProvider() && a.Type == ProviderTypeAPIKey) ||
		a.IsQoderCosy()
}

// IsTLSFingerprintEnabled 检查是否启用 TLS 指纹伪装
// 仅适用于支持 TLS 指纹伪装的提供商，启用后模拟 Node.js/Claude Code/Codex CLI 客户端握手特征。
func (a *Record) IsTLSFingerprintEnabled() bool {
	if !a.SupportsTLSFingerprint() {
		return false
	}
	if a.Extra == nil {
		return false
	}
	if v, ok := a.Extra["enable_tls_fingerprint"]; ok {
		if enabled, ok := v.(bool); ok {
			return enabled
		}
	}
	return false
}

// GetTLSFingerprintProfileID 获取提供商绑定的 TLS 指纹模板 ID
// 返回 0 表示未绑定（使用内置默认 profile）
func (a *Record) GetTLSFingerprintProfileID() int64 {
	if a.Extra == nil {
		return 0
	}
	v, ok := a.Extra["tls_fingerprint_profile_id"]
	if !ok {
		return 0
	}
	switch id := v.(type) {
	case float64:
		return int64(id)
	case int64:
		return id
	case int:
		return int64(id)
	case json.Number:
		if i, err := id.Int64(); err == nil {
			return i
		}
	}
	return 0
}

// GetTLSFingerprintRouterID 获取提供商绑定的 TLS 路由器 ID。
// 返回 0 表示未绑定路由器。
func (a *Record) GetTLSFingerprintRouterID() int64 {
	if a.Extra == nil {
		return 0
	}
	v, ok := a.Extra["tls_fingerprint_router_id"]
	if !ok {
		return 0
	}
	switch id := v.(type) {
	case float64:
		return int64(id)
	case int64:
		return id
	case int:
		return int64(id)
	case json.Number:
		if i, err := id.Int64(); err == nil {
			return i
		}
	}
	return 0
}

// IsSessionIDMaskingEnabled 检查是否启用会话ID伪装
// 仅适用于 Anthropic OAuth/SetupToken 类型提供商
// 启用后将在一段时间内（15分钟）固定 metadata.user_id 中的 session ID，
// 使上游认为请求来自同一个会话
func (a *Record) IsSessionIDMaskingEnabled() bool {
	if !a.IsAnthropicOAuthOrSetupToken() {
		return false
	}
	if a.Extra == nil {
		return false
	}
	if v, ok := a.Extra["session_id_masking_enabled"]; ok {
		if enabled, ok := v.(bool); ok {
			return enabled
		}
	}
	return false
}

// IsCustomBaseURLEnabled 检查是否启用自定义 base URL 中继转发
// 仅适用于 Anthropic OAuth/SetupToken 类型提供商
func (a *Record) IsCustomBaseURLEnabled() bool {
	if !a.IsAnthropicOAuthOrSetupToken() {
		return false
	}
	if a.Extra == nil {
		return false
	}
	if v, ok := a.Extra["custom_base_url_enabled"]; ok {
		if enabled, ok := v.(bool); ok {
			return enabled
		}
	}
	return false
}

// GetCustomBaseURL 返回自定义中继服务的 base URL
func (a *Record) GetCustomBaseURL() string {
	return a.GetExtraString("custom_base_url")
}

// IsCacheTTLOverrideEnabled 检查是否启用缓存 TTL 强制替换
// 仅适用于 Anthropic OAuth/SetupToken 类型提供商
// 启用后将所有 cache creation tokens 归入指定的 TTL 类型（5m 或 1h）
func (a *Record) IsCacheTTLOverrideEnabled() bool {
	if !a.IsAnthropicOAuthOrSetupToken() {
		return false
	}
	if a.Extra == nil {
		return false
	}
	if v, ok := a.Extra["cache_ttl_override_enabled"]; ok {
		if enabled, ok := v.(bool); ok {
			return enabled
		}
	}
	return false
}

// GetCacheTTLOverrideTarget 获取缓存 TTL 强制替换的目标类型
// 返回 "5m" 或 "1h"，默认 "5m"
func (a *Record) GetCacheTTLOverrideTarget() string {
	if a.Extra == nil {
		return "5m"
	}
	if v, ok := a.Extra["cache_ttl_override_target"]; ok {
		if target, ok := v.(string); ok && (target == "5m" || target == "1h") {
			return target
		}
	}
	return "5m"
}

// GetQuotaLimit 获取 API Key 提供商的配额限制（美元）
// 返回 0 表示未启用
func (a *Record) GetQuotaLimit() float64 {
	return a.GetExtraFloat64("quota_limit")
}

// GetQuotaUsed 获取 API Key 提供商的已用配额（美元）
func (a *Record) GetQuotaUsed() float64 {
	return a.GetExtraFloat64("quota_used")
}

// GetQuotaDailyLimit 获取日额度限制（美元），0 表示未启用
func (a *Record) GetQuotaDailyLimit() float64 {
	return a.GetExtraFloat64("quota_daily_limit")
}

// GetQuotaDailyUsed 获取当日已用额度（美元）
func (a *Record) GetQuotaDailyUsed() float64 {
	return a.GetExtraFloat64("quota_daily_used")
}

// GetQuotaWeeklyLimit 获取周额度限制（美元），0 表示未启用
func (a *Record) GetQuotaWeeklyLimit() float64 {
	return a.GetExtraFloat64("quota_weekly_limit")
}

// GetQuotaWeeklyUsed 获取本周已用额度（美元）
func (a *Record) GetQuotaWeeklyUsed() float64 {
	return a.GetExtraFloat64("quota_weekly_used")
}

// GetExtraFloat64 从 Extra 中读取指定 key 的 float64 值
func (a *Record) GetExtraFloat64(key string) float64 {
	if a.Extra == nil {
		return 0
	}
	if v, ok := a.Extra[key]; ok {
		return ParseExtraFloat64(v)
	}
	return 0
}

// GetExtraTime 从 Extra 中读取 RFC3339 时间戳
func (a *Record) GetExtraTime(key string) time.Time {
	if a.Extra == nil {
		return time.Time{}
	}
	if v, ok := a.Extra[key]; ok {
		return ParseExtraTime(v)
	}
	return time.Time{}
}

// GetExtraBool 从 Extra 中读取指定 key 的 bool 值
func (a *Record) GetExtraBool(key string) bool {
	if a.Extra == nil {
		return false
	}
	if v, ok := a.Extra[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

// GetExtraStringDefault 读取 Extra 中的字符串值，缺失或为空时返回 defaultVal。
func (a *Record) GetExtraStringDefault(key, defaultVal string) string {
	if v := a.GetExtraString(key); v != "" {
		return v
	}
	return defaultVal
}

// GetExtraInt 从 Extra 中读取指定 key 的 int 值
func (a *Record) GetExtraInt(key string) int {
	if a.Extra == nil {
		return 0
	}
	if v, ok := a.Extra[key]; ok {
		return int(ParseExtraFloat64(v))
	}
	return 0
}

// GetQuotaDailyResetMode 获取日额度重置模式："rolling"（默认）或 "fixed"
func (a *Record) GetQuotaDailyResetMode() string {
	if m := a.GetExtraString("quota_daily_reset_mode"); m == "fixed" {
		return "fixed"
	}
	return "rolling"
}

// GetQuotaDailyResetHour 获取固定重置的小时（0-23），默认 0
func (a *Record) GetQuotaDailyResetHour() int {
	return a.GetExtraInt("quota_daily_reset_hour")
}

// GetQuotaWeeklyResetMode 获取周额度重置模式："rolling"（默认）或 "fixed"
func (a *Record) GetQuotaWeeklyResetMode() string {
	if m := a.GetExtraString("quota_weekly_reset_mode"); m == "fixed" {
		return "fixed"
	}
	return "rolling"
}

// GetQuotaWeeklyResetDay 获取固定重置的星期几（0=周日, 1=周一, ..., 6=周六），默认 1（周一）
func (a *Record) GetQuotaWeeklyResetDay() int {
	if a.Extra == nil {
		return 1
	}
	if _, ok := a.Extra["quota_weekly_reset_day"]; !ok {
		return 1
	}
	return a.GetExtraInt("quota_weekly_reset_day")
}

// GetQuotaWeeklyResetHour 获取周配额固定重置的小时（0-23），默认 0
func (a *Record) GetQuotaWeeklyResetHour() int {
	return a.GetExtraInt("quota_weekly_reset_hour")
}

// GetQuotaResetTimezone 获取固定重置的时区名（IANA），默认 "UTC"
func (a *Record) GetQuotaResetTimezone() string {
	if tz := a.GetExtraString("quota_reset_timezone"); tz != "" {
		return tz
	}
	return "UTC"
}

// QuotaNotifyConfig returns the notify configuration for a given quota dimension.
// dim must be one of quotaDimDaily, quotaDimWeekly, quotaDimTotal.
func (a *Record) QuotaNotifyConfig(dim string) (enabled bool, threshold float64, thresholdType string) {
	enabled = a.GetExtraBool("quota_notify_" + dim + "_enabled")
	threshold = a.GetExtraFloat64("quota_notify_" + dim + "_threshold")
	thresholdType = a.GetExtraStringDefault("quota_notify_"+dim+"_threshold_type", thresholdTypeFixed)
	return
}

func (a *Record) GetQuotaNotifyDailyEnabled() bool {
	e, _, _ := a.QuotaNotifyConfig(quotaDimDaily)
	return e
}

func (a *Record) GetQuotaNotifyDailyThreshold() float64 {
	_, t, _ := a.QuotaNotifyConfig(quotaDimDaily)
	return t
}

func (a *Record) GetQuotaNotifyDailyThresholdType() string {
	_, _, tt := a.QuotaNotifyConfig(quotaDimDaily)
	return tt
}

func (a *Record) GetQuotaNotifyWeeklyEnabled() bool {
	e, _, _ := a.QuotaNotifyConfig(quotaDimWeekly)
	return e
}

func (a *Record) GetQuotaNotifyWeeklyThreshold() float64 {
	_, t, _ := a.QuotaNotifyConfig(quotaDimWeekly)
	return t
}

func (a *Record) GetQuotaNotifyWeeklyThresholdType() string {
	_, _, tt := a.QuotaNotifyConfig(quotaDimWeekly)
	return tt
}

func (a *Record) GetQuotaNotifyTotalEnabled() bool {
	e, _, _ := a.QuotaNotifyConfig(quotaDimTotal)
	return e
}

func (a *Record) GetQuotaNotifyTotalThreshold() float64 {
	_, t, _ := a.QuotaNotifyConfig(quotaDimTotal)
	return t
}

func (a *Record) GetQuotaNotifyTotalThresholdType() string {
	_, _, tt := a.QuotaNotifyConfig(quotaDimTotal)
	return tt
}

func LastFixedDailyReset(hour int, tz *time.Location, now time.Time) time.Time {
	return billing.LastFixedDailyReset(hour, tz, now)
}

func LastFixedWeeklyReset(day, hour int, tz *time.Location, now time.Time) time.Time {
	return billing.LastFixedWeeklyReset(day, hour, tz, now)
}

// IsFixedDailyPeriodExpired 检查日配额是否在固定时间模式下已过期
func (a *Record) IsFixedDailyPeriodExpired(periodStart time.Time) bool {
	if periodStart.IsZero() {
		return true
	}
	tz, err := a.LoadLocation(a.GetQuotaResetTimezone())
	if err != nil {
		tz = time.UTC
	}
	lastReset := LastFixedDailyReset(a.GetQuotaDailyResetHour(), tz, a.now())
	return periodStart.Before(lastReset)
}

// IsFixedWeeklyPeriodExpired 检查周配额是否在固定时间模式下已过期
func (a *Record) IsFixedWeeklyPeriodExpired(periodStart time.Time) bool {
	if periodStart.IsZero() {
		return true
	}
	tz, err := a.LoadLocation(a.GetQuotaResetTimezone())
	if err != nil {
		tz = time.UTC
	}
	lastReset := LastFixedWeeklyReset(a.GetQuotaWeeklyResetDay(), a.GetQuotaWeeklyResetHour(), tz, a.now())
	return periodStart.Before(lastReset)
}

// ValidateQuotaResetConfig 校验配额固定重置时间配置的合法性
func ValidateQuotaResetConfig(extra map[string]any, loadLocation func(string) (*time.Location, error)) error {
	if extra == nil {
		return nil
	}
	// 校验时区
	if tz, ok := extra["quota_reset_timezone"].(string); ok && tz != "" {
		if _, err := loadLocation(tz); err != nil {
			return errors.New("invalid quota_reset_timezone: must be a valid IANA timezone name")
		}
	}
	// 日配额重置模式
	if mode, ok := extra["quota_daily_reset_mode"].(string); ok {
		if mode != "rolling" && mode != "fixed" {
			return errors.New("quota_daily_reset_mode must be 'rolling' or 'fixed'")
		}
	}
	// 日配额重置小时
	if v, ok := extra["quota_daily_reset_hour"]; ok {
		hour := int(ParseExtraFloat64(v))
		if hour < 0 || hour > 23 {
			return errors.New("quota_daily_reset_hour must be between 0 and 23")
		}
	}
	// 周配额重置模式
	if mode, ok := extra["quota_weekly_reset_mode"].(string); ok {
		if mode != "rolling" && mode != "fixed" {
			return errors.New("quota_weekly_reset_mode must be 'rolling' or 'fixed'")
		}
	}
	// 周配额重置星期几
	if v, ok := extra["quota_weekly_reset_day"]; ok {
		day := int(ParseExtraFloat64(v))
		if day < 0 || day > 6 {
			return errors.New("quota_weekly_reset_day must be between 0 (Sunday) and 6 (Saturday)")
		}
	}
	// 周配额重置小时
	if v, ok := extra["quota_weekly_reset_hour"]; ok {
		hour := int(ParseExtraFloat64(v))
		if hour < 0 || hour > 23 {
			return errors.New("quota_weekly_reset_hour must be between 0 and 23")
		}
	}
	return nil
}

// HasAnyQuotaLimit 检查是否配置了任一维度的配额限制
func (a *Record) HasAnyQuotaLimit() bool {
	return a.GetQuotaLimit() > 0 || a.GetQuotaDailyLimit() > 0 || a.GetQuotaWeeklyLimit() > 0
}

// IsPeriodExpired 检查指定周期（自 periodStart 起经过 dur）是否已过期
func IsPeriodExpired(periodStart time.Time, dur time.Duration) bool {
	if periodStart.IsZero() {
		return true // 从未使用过，视为过期（下次 increment 会初始化）
	}
	return time.Since(periodStart) >= dur
}

// IsDailyQuotaPeriodExpired 检查日配额周期是否已过期（用于显示层判断是否需要将 used 归零）
func (a *Record) IsDailyQuotaPeriodExpired() bool {
	start := a.GetExtraTime("quota_daily_start")
	if a.GetQuotaDailyResetMode() == "fixed" {
		return a.IsFixedDailyPeriodExpired(start)
	}
	return IsPeriodExpired(start, 24*time.Hour)
}

// IsWeeklyQuotaPeriodExpired 检查周配额周期是否已过期（用于显示层判断是否需要将 used 归零）
func (a *Record) IsWeeklyQuotaPeriodExpired() bool {
	start := a.GetExtraTime("quota_weekly_start")
	if a.GetQuotaWeeklyResetMode() == "fixed" {
		return a.IsFixedWeeklyPeriodExpired(start)
	}
	return IsPeriodExpired(start, 7*24*time.Hour)
}

// IsQuotaExceeded 检查 API Key 提供商配额是否已超限（任一维度超限即返回 true）
func (a *Record) IsQuotaExceeded() bool {
	// 总额度
	if limit := a.GetQuotaLimit(); limit > 0 && a.GetQuotaUsed() >= limit {
		return true
	}
	// 日额度（周期过期视为未超限，下次 increment 会重置）
	if limit := a.GetQuotaDailyLimit(); limit > 0 {
		start := a.GetExtraTime("quota_daily_start")
		var expired bool
		if a.GetQuotaDailyResetMode() == "fixed" {
			expired = a.IsFixedDailyPeriodExpired(start)
		} else {
			expired = IsPeriodExpired(start, 24*time.Hour)
		}
		if !expired && a.GetQuotaDailyUsed() >= limit {
			return true
		}
	}
	// 周额度
	if limit := a.GetQuotaWeeklyLimit(); limit > 0 {
		start := a.GetExtraTime("quota_weekly_start")
		var expired bool
		if a.GetQuotaWeeklyResetMode() == "fixed" {
			expired = a.IsFixedWeeklyPeriodExpired(start)
		} else {
			expired = IsPeriodExpired(start, 7*24*time.Hour)
		}
		if !expired && a.GetQuotaWeeklyUsed() >= limit {
			return true
		}
	}
	return false
}

// IsShadow 报告提供商是否为影子提供商（parent_provider_id 非空；当前唯一预设是 spark 维度）。
func (a *Record) IsShadow() bool { return a != nil && a.ParentProviderID != nil }

// IsCredentialShadow 判断提供商是否为凭据影子，管理和后台授权据此跳过独立凭据操作。
func (a *Record) IsCredentialShadow() bool { return a.IsShadow() }

// QuotaDimensionOrDefault 返回提供商的用量维度，未设置时回退 "global"。
func (a *Record) QuotaDimensionOrDefault() string {
	if a == nil || strings.TrimSpace(a.QuotaDimension) == "" {
		return QuotaDimensionGlobal
	}
	return a.QuotaDimension
}

// IsCNProvider 判断平台是否属于国产供应商集合。
func IsCNProvider(platform string) bool {
	switch platform {
	case PlatformKimi, PlatformZhipu, PlatformDeepseek:
		return true
	default:
		return false
	}
}

const (
	thresholdTypeFixed = "fixed"
	quotaDimDaily      = "daily"
	quotaDimWeekly     = "weekly"
	quotaDimTotal      = "total"
)
