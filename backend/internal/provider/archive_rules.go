package provider

import (
	"errors"
	"fmt"
	"strings"

	"github.com/TokenFlux/TokenRouter/internal/provider/transfer"
)

// ArchiveIdentityHints 保存供应商解码出的导入提示，身份认证使用单独的验证流程。
type ArchiveIdentityHints struct{ Email, PlanType, ChatGPTAccountID, ChatGPTUserID, OrganizationID string }

func ValidateArchiveProvider(item transfer.DataProvider) error {
	if strings.TrimSpace(item.Name) == "" {
		return errors.New("provider name is required")
	}
	if strings.TrimSpace(item.Platform) == "" {
		return errors.New("provider platform is required")
	}
	if strings.TrimSpace(item.Type) == "" {
		return errors.New("provider type is required")
	}
	if len(item.Credentials) == 0 {
		return errors.New("provider credentials is required")
	}
	switch item.Type {
	case ProviderTypeOAuth, ProviderTypeSetupToken, ProviderTypeAPIKey, ProviderTypeUpstream,
		ProviderTypeBedrock, ProviderTypeServiceAccount, ProviderTypeCosy:
	default:
		return fmt.Errorf("provider type is invalid: %s", item.Type)
	}
	platform := strings.ToLower(strings.TrimSpace(item.Platform))
	if platform == PlatformQoder && item.Type != ProviderTypeCosy {
		return fmt.Errorf("qoder providers require %s provider type", ProviderTypeCosy)
	}
	if platform != PlatformQoder && item.Type == ProviderTypeCosy {
		return fmt.Errorf("%s provider type requires %s platform", ProviderTypeCosy, PlatformQoder)
	}
	if item.RateMultiplier != nil && *item.RateMultiplier < 0 {
		return errors.New("rate_multiplier must be >= 0")
	}
	if item.Concurrency != nil && *item.Concurrency < 0 {
		return errors.New("concurrency must be >= 0")
	}
	if item.Priority != nil && *item.Priority < 0 {
		return errors.New("priority must be >= 0")
	}
	return nil
}

// ArchiveIDToken 提取 OpenAI OAuth 导入的可选身份提示。
func ArchiveIDToken(item *transfer.DataProvider) string {
	if item == nil || item.Credentials == nil || strings.ToLower(strings.TrimSpace(item.Platform)) != PlatformOpenAI || strings.ToLower(strings.TrimSpace(item.Type)) != ProviderTypeOAuth {
		return ""
	}
	token, _ := item.Credentials["id_token"].(string)
	if strings.TrimSpace(token) == "" {
		return ""
	}
	return token
}

// FillArchiveIdentity 补齐缺失的字符串字段，已有非空值保持不变。
func FillArchiveIdentity(item *transfer.DataProvider, hints *ArchiveIdentityHints) {
	if item == nil || hints == nil || item.Credentials == nil {
		return
	}
	set := func(key, value string) {
		if value == "" {
			return
		}
		if existing, _ := item.Credentials[key].(string); existing == "" {
			item.Credentials[key] = value
		}
	}
	set("email", hints.Email)
	set("plan_type", hints.PlanType)
	set("chatgpt_account_id", hints.ChatGPTAccountID)
	set("chatgpt_user_id", hints.ChatGPTUserID)
	set("organization_id", hints.OrganizationID)
}
