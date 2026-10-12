package provider

import (
	"strconv"
	"strings"
	"time"
)

func CanRefreshQoder(provider *Record) bool {
	return provider != nil && provider.Platform == PlatformQoder && provider.Type == ProviderTypeCosy
}

func NeedsRefreshQoder(provider *Record, _ time.Duration) bool {
	if !CanRefreshQoder(provider) {
		return false
	}
	if strings.TrimSpace(provider.GetCredential("pat")) != "" {
		return false
	}
	if strings.TrimSpace(provider.GetCredential("refresh_token")) == "" {
		return false
	}
	if expiresAt := provider.GetCredentialAsTime("expires_at"); expiresAt != nil {
		return time.Until(*expiresAt) < 15*time.Minute
	}
	// Qoder 的 RateLimitResetAt 也承载 upstream 月度 quota / agent-limit 调度信号，
	// 不能像 OpenAI 一样把通用限流状态当成后台 token refresh 触发器。
	return false
}

func QoderTokenCacheKey(provider *Record) string {
	if provider == nil {
		return "qoder:provider:0"
	}
	return "qoder:provider:" + strconv.FormatInt(provider.ID, 10)
}

func MergeQoderRefreshCredentials(oldCredentials, newCredentials map[string]any, site, refreshMode string, expiresAt time.Time) map[string]any {
	newCredentials = MergeCredentials(oldCredentials, newCredentials)
	newCredentials["site"] = site
	newCredentials["refresh_mode"] = refreshMode
	if site == "cn" {
		// 合并凭据后再次清理历史随机机器字段，使国内提供商使用对应站点的身份字段。
		delete(newCredentials, "machine_token")
		delete(newCredentials, "machine_type")
	}
	if !expiresAt.IsZero() {
		newCredentials["expires_at"] = expiresAt.UTC().Format(time.RFC3339)
	}
	refreshToken := strings.TrimSpace(qoderOldRefreshToken(oldCredentials))
	if strings.TrimSpace(stringFromCredentialValue(newCredentials["refresh_token"])) == "" {
		if refreshToken != "" {
			newCredentials["refresh_token"] = refreshToken
		}
	}
	// 目前观测到的 Qoder refresh 响应没有可靠的新过期时间。
	// 不保留导入时的旧 expires_at，否则 NeedsRefresh 会立刻把刚刷新的提供商
	// 判定为即将过期，并可能形成刷新循环。
	if expiresAt.IsZero() {
		delete(newCredentials, "expires_at")
	}
	return newCredentials
}

func stringFromCredentialValue(value any) string {
	switch v := value.(type) {
	case string:
		return v
	default:
		return ""
	}
}

func qoderOldRefreshToken(v map[string]any) string {
	return stringFromCredentialValue(v["refresh_token"])
}

// NeedsRefreshQoderAfterFailure 使用失败时的凭据身份判断请求期刷新。
// 已轮换凭据不重复消费刷新令牌；同一失败身份不受临近过期窗口限制。
func NeedsRefreshQoderAfterFailure(value *Record, failedCredentials string, ttl time.Duration) bool {
	if !CanRefreshQoder(value) {
		return false
	}
	if strings.TrimSpace(value.GetCredential("refresh_token")) == "" {
		return false
	}
	if failedCredentials != "" {
		return QoderRefreshCredentialsHash(value.Credentials) == failedCredentials
	}
	return NeedsRefreshQoder(value, ttl)
}

// QoderRefreshCredentialsHash 计算刷新身份字段的哈希，供凭据竞争检查使用。
func QoderRefreshCredentialsHash(credentials map[string]any) string {
	if len(credentials) == 0 {
		return QoderCredentialsHash(nil)
	}
	keys := []string{
		"pat",
		"security_oauth_token",
		"refresh_token",
		"machine_id",
		"machine_token",
		"machine_type",
		"uid",
		"aid",
		"organization_id",
		"organization_name",
		"name",
		"user_type",
		"site",
		"refresh_mode",
	}
	auth := make(map[string]any, len(keys))
	for _, key := range keys {
		if value, ok := credentials[key]; ok {
			auth[key] = value
		}
	}
	return QoderCredentialsHash(auth)
}
