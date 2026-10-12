package provider

import "maps"

var (
	// SensitiveCredentialKeys 列出向前端返回凭据时需要移除的敏感字段。
	// dto 响应脱敏和服务更新合并共用此清单，添加凭据类型时需要同步更新。
	SensitiveCredentialKeys = []string{
		// OAuth
		"access_token", "refresh_token", "id_token", "agent_private_key",
		// Qoder COSY 凭据
		"pat", "security_oauth_token", "machine_token",
		// API Key 类及 New API 用户钱包查询凭据
		"api_key", "session_key", "cookie", "new_api_user_access_token",
		// Grok Web SSO 与密码在兑换 Build OAuth 后不得持久化或回显。
		"password", "sso_token", "sso", "sso-rw", "clearTextPassword",
		// 云服务凭据
		"aws_secret_access_key", "aws_session_token",
		"service_account_json", "service_account", "private_key",
	}

	sensitiveCredentialKeySet = func() map[string]struct{} {
		m := make(map[string]struct{}, len(SensitiveCredentialKeys))
		for _, k := range SensitiveCredentialKeys {
			m[k] = struct{}{}
		}
		return m
	}()
)

// IsSensitiveCredentialKey 判断指定键是否为敏感凭证子键。
func IsSensitiveCredentialKey(key string) bool {
	_, ok := sensitiveCredentialKeySet[key]
	return ok
}

// MergePreservingSensitiveCreds 将 incoming 复制为新 map，并从 existing 补齐省略的敏感字段。
// 前端提交脱敏后的完整对象时，已有 token 通过此规则保留。
// 非敏感字段以 incoming 为准，敏感字段在 incoming 携带该键时按输入覆盖。
func MergePreservingSensitiveCreds(existing, incoming map[string]any) map[string]any {
	out := make(map[string]any, len(incoming)+len(SensitiveCredentialKeys))
	maps.Copy(out, incoming)
	for _, key := range SensitiveCredentialKeys {
		if _, hasIncoming := incoming[key]; hasIncoming {
			continue
		}
		if existingVal, ok := existing[key]; ok {
			out[key] = existingVal
		}
	}
	return out
}

// SanitizeStoredCredentials 移除 OAuth 兑换后不得写入提供商凭据的 Grok SSO、密码和 cookie。
// 批量操作可能缺少平台标识，所有平台都清理 cookie。
func SanitizeStoredCredentials(platform string, creds map[string]any) map[string]any {
	if creds == nil {
		return nil
	}
	_ = platform
	for _, key := range []string{
		"password", "sso_token", "sso", "sso-rw", "clearTextPassword", "cookie",
	} {
		delete(creds, key)
	}
	return creds
}
