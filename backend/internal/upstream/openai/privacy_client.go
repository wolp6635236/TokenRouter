package openai

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/imroc/req/v3"

	wire "github.com/TokenFlux/TokenRouter/internal/protocol/openai"
)

const (
	PrivacyModeTrainingOff = "training_off"
	PrivacyModeFailed      = "training_set_failed"
	PrivacyModeCFBlocked   = "training_set_cf_blocked"
)

type (
	PrivacyClientFactory func(string) (*req.Client, error)
	ChatGPTAccountInfo   = wire.ChatGPTAccountInfo
	PrivacyEndpoints     struct{ Settings, Providers, Subscriptions string }
	PrivacyClient        struct{ Endpoints PrivacyEndpoints }
)

// DisableOpenAITraining 调用 ChatGPT 设置接口关闭训练数据共享。
// 返回 privacy_mode 值：成功时为 training_off，失败时为对应失败原因。
func (p PrivacyClient) DisableOpenAITraining(ctx context.Context, clientFactory PrivacyClientFactory, accessToken, proxyURL string) string {
	if accessToken == "" || clientFactory == nil {
		return ""
	}

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	client, err := clientFactory(proxyURL)
	if err != nil {
		slog.Warn("openai_privacy_client_error", "error", err.Error())
		return PrivacyModeFailed
	}

	resp, err := client.R().
		SetContext(ctx).
		SetHeader("Authorization", "Bearer "+accessToken).
		SetHeader("Origin", "https://chatgpt.com").
		SetHeader("Referer", "https://chatgpt.com/").
		SetHeader("Accept", "application/json").
		SetHeader("sec-fetch-mode", "cors").
		SetHeader("sec-fetch-site", "same-origin").
		SetHeader("sec-fetch-dest", "empty").
		SetQueryParam("feature", "training_allowed").
		SetQueryParam("value", "false").
		Patch(p.Endpoints.Settings)
	if err != nil {
		slog.Warn("openai_privacy_request_error", "error", err.Error())
		return PrivacyModeFailed
	}

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusServiceUnavailable {
		body := resp.String()
		if strings.Contains(body, "cloudflare") || strings.Contains(body, "cf-") || strings.Contains(body, "Just a moment") {
			slog.Warn("openai_privacy_cf_blocked", "status", resp.StatusCode)
			return PrivacyModeCFBlocked
		}
	}

	if !resp.IsSuccessState() {
		slog.Warn("openai_privacy_failed", "status", resp.StatusCode, "body", Truncate(resp.String(), 200))
		return PrivacyModeFailed
	}

	slog.Info("openai_privacy_training_disabled")
	return PrivacyModeTrainingOff
}

// FetchChatGPTAccountInfo 调用 ChatGPT backend-api 获取账户信息。
// 当 id_token 不包含这些字段时（例如 Mobile RT）作为兜底来源。
// orgID 用于在个人账户和团队账户并存时匹配正确账户。
// 任意失败都返回 nil，保持 best-effort 且不阻塞主流程。
func (p PrivacyClient) FetchChatGPTAccountInfo(ctx context.Context, clientFactory PrivacyClientFactory, accessToken, proxyURL, orgID string) *ChatGPTAccountInfo {
	if accessToken == "" || clientFactory == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	client, err := clientFactory(proxyURL)
	if err != nil {
		slog.Debug("chatgpt_account_check_client_error", "error", err.Error())
		return nil
	}

	var result map[string]any
	resp, err := client.R().
		SetContext(ctx).
		SetHeader("Authorization", "Bearer "+accessToken).
		SetHeader("Origin", "https://chatgpt.com").
		SetHeader("Referer", "https://chatgpt.com/").
		SetHeader("Accept", "application/json").
		SetSuccessResult(&result).
		Get(p.Endpoints.Providers)
	if err != nil {
		slog.Debug("chatgpt_account_check_request_error", "error", err.Error())
		return nil
	}

	if !resp.IsSuccessState() {
		slog.Debug("chatgpt_account_check_failed", "status", resp.StatusCode, "body", Truncate(resp.String(), 200))
		return nil
	}

	info := &ChatGPTAccountInfo{}

	providers, ok := result["accounts"].(map[string]any)
	if !ok {
		slog.Debug("chatgpt_account_check_no_accounts", "body", Truncate(resp.String(), 300))
		return nil
	}

	// 优先匹配 orgID 对应的账户（access_token JWT 中的 poid）
	if orgID != "" {
		if acctRaw, ok := providers[orgID]; ok {
			if acct, ok := acctRaw.(map[string]any); ok {
				if IsUsableChatGPTAccountCandidate(acct, time.Now()) {
					FillProviderInfo(info, acct, orgID)
				}
			}
		}
	}

	// 未匹配到时，遍历所有账户：优先 is_default，次选非 free
	if info.PlanType == "" {
		type candidate struct {
			planType   string
			expiresAt  string
			providerID string
		}
		var defaultC, paidC, anyC candidate
		for key, acctRaw := range providers {
			acct, ok := acctRaw.(map[string]any)
			if !ok {
				continue
			}
			if !IsUsableChatGPTAccountCandidate(acct, time.Now()) {
				continue
			}
			planType := ExtractPlanType(acct)
			if planType == "" {
				continue
			}
			ea := ExtractEntitlementExpiresAt(acct)
			id := ChatGPTAccountObjectID(acct, key)
			if anyC.planType == "" {
				anyC = candidate{planType, ea, id}
			}
			if provider, ok := acct["account"].(map[string]any); ok {
				if isDefault, _ := provider["is_default"].(bool); isDefault {
					defaultC = candidate{planType, ea, id}
				}
			}
			if !strings.EqualFold(planType, "free") && paidC.planType == "" {
				paidC = candidate{planType, ea, id}
			}
		}
		// 优先级：default > 非 free > 任意
		switch {
		case defaultC.planType != "":
			info.PlanType, info.SubscriptionExpiresAt, info.ProviderID = defaultC.planType, defaultC.expiresAt, defaultC.providerID
		case paidC.planType != "":
			info.PlanType, info.SubscriptionExpiresAt, info.ProviderID = paidC.planType, paidC.expiresAt, paidC.providerID
		default:
			info.PlanType, info.SubscriptionExpiresAt, info.ProviderID = anyC.planType, anyC.expiresAt, anyC.providerID
		}
	}

	if info.PlanType == "" {
		slog.Debug("chatgpt_account_check_no_plan_type", "body", Truncate(resp.String(), 300))
		return nil
	}

	slog.Info("chatgpt_account_check_success", "plan_type", info.PlanType, "subscription_expires_at", info.SubscriptionExpiresAt, "org_id", orgID)
	return info
}

// FetchChatGPTSubscriptionExpiresAt 读取 ChatGPT/Codex 客户端使用的轻量订阅接口。
// 部分 Plus 账户已不在 accounts/check 暴露 entitlement.expires_at，
// 但该接口仍会返回 active_until。
func (p PrivacyClient) FetchChatGPTSubscriptionExpiresAt(ctx context.Context, clientFactory PrivacyClientFactory, accessToken, proxyURL, providerID string) string {
	providerID = strings.TrimSpace(providerID)
	if accessToken == "" || providerID == "" || clientFactory == nil {
		return ""
	}

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	client, err := clientFactory(proxyURL)
	if err != nil {
		slog.Debug("chatgpt_subscription_client_error", "error", err.Error())
		return ""
	}

	var result struct {
		PlanType    string `json:"plan_type"`
		ActiveUntil string `json:"active_until"`
		WillRenew   bool   `json:"will_renew"`
		ID          string `json:"id"`
	}
	resp, err := client.R().
		SetContext(ctx).
		SetHeader("Authorization", "Bearer "+accessToken).
		SetHeader("Origin", "https://chatgpt.com").
		SetHeader("Referer", "https://chatgpt.com/").
		SetHeader("Accept", "application/json").
		SetSuccessResult(&result).
		SetQueryParam("account_id", providerID).
		Get(p.Endpoints.Subscriptions)
	if err != nil {
		slog.Debug("chatgpt_subscription_request_error", "error", err.Error())
		return ""
	}
	if !resp.IsSuccessState() {
		slog.Debug("chatgpt_subscription_failed", "status", resp.StatusCode, "body", Truncate(resp.String(), 200))
		return ""
	}

	activeUntil := strings.TrimSpace(result.ActiveUntil)
	if activeUntil == "" {
		slog.Debug("chatgpt_subscription_no_active_until", "plan_type", result.PlanType, "has_subscription_id", strings.TrimSpace(result.ID) != "", "will_renew", result.WillRenew)
		return ""
	}
	if _, err := time.Parse(time.RFC3339, activeUntil); err != nil {
		slog.Debug("chatgpt_subscription_bad_active_until", "active_until", activeUntil, "error", err.Error())
		return ""
	}

	slog.Info("chatgpt_subscription_success", "plan_type", result.PlanType, "subscription_expires_at", activeUntil, "account_id", providerID)
	return activeUntil
}

// FillProviderInfo 从单个 account 对象中提取套餐、到期时间和来源账户。
func FillProviderInfo(info *ChatGPTAccountInfo, acct map[string]any, fallbackID string) {
	info.PlanType = ExtractPlanType(acct)
	info.SubscriptionExpiresAt = ExtractEntitlementExpiresAt(acct)
	info.ProviderID = ChatGPTAccountObjectID(acct, fallbackID)
}

// ChatGPTAccountObjectID 优先读取对象内的真实提供商 ID；map key 仅用于缺失时兜底。
func ChatGPTAccountObjectID(acct map[string]any, fallbackID string) string {
	if provider, ok := acct["account"].(map[string]any); ok {
		if id, ok := provider["account_id"].(string); ok && strings.TrimSpace(id) != "" {
			return strings.TrimSpace(id)
		}
	}
	return strings.TrimSpace(fallbackID)
}

// ExtractPlanType 从单个 provider 对象中提取 plan_type。
func ExtractPlanType(acct map[string]any) string {
	if provider, ok := acct["account"].(map[string]any); ok {
		if planType, ok := provider["plan_type"].(string); ok && planType != "" {
			return planType
		}
	}
	if entitlement, ok := acct["entitlement"].(map[string]any); ok {
		if subPlan, ok := entitlement["subscription_plan"].(string); ok && subPlan != "" {
			return subPlan
		}
	}
	return ""
}

func IsUsableChatGPTAccountCandidate(acct map[string]any, now time.Time) bool {
	if acct == nil || HasChatGPTAccountDeactivatedMarker(acct) {
		return false
	}
	if provider, ok := acct["account"].(map[string]any); ok && HasChatGPTAccountDeactivatedMarker(provider) {
		return false
	}

	expiresAt := ExtractEntitlementExpiresAt(acct)
	if expiresAt == "" {
		return true
	}
	expiry, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		return true
	}
	return expiry.After(now)
}

func HasChatGPTAccountDeactivatedMarker(obj map[string]any) bool {
	for _, key := range []string{"deactivated", "is_deactivated", "disabled", "is_disabled"} {
		if value, ok := obj[key].(bool); ok && value {
			return true
		}
	}
	for _, key := range []string{"deactivated_at", "disabled_at", "deleted_at"} {
		if value, ok := obj[key].(string); ok && strings.TrimSpace(value) != "" {
			return true
		}
	}
	for _, key := range []string{"status", "state"} {
		value, _ := obj[key].(string)
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "deactivated", "disabled", "deleted", "inactive", "suspended":
			return true
		}
	}
	return false
}

// ExtractEntitlementExpiresAt 从 entitlement 中提取 expires_at。
// 预期为 RFC3339 字符串格式，如 "2026-05-02T20:32:12+00:00"。
func ExtractEntitlementExpiresAt(acct map[string]any) string {
	entitlement, ok := acct["entitlement"].(map[string]any)
	if !ok {
		return ""
	}
	ea, _ := entitlement["expires_at"].(string)
	return ea
}

func Truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + fmt.Sprintf("...(%d more)", len(s)-n)
}
