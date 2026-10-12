package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"github.com/TokenFlux/TokenRouter/internal/pkg/logredact"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

// ForbiddenObservation 将平台分类与提供商处罚权限分开。
type ForbiddenObservation struct {
	Message                  string
	Body                     []byte
	HTML                     bool
	Kind                     string
	ValidationURL            string
	ConcurrentRequestLimited bool
	ConcurrencyReason        string
}

func buildForbiddenErrorMessage(prefix string, upstreamMsg string, responseBody []byte, fallback string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix != "" && !strings.HasSuffix(prefix, " ") {
		prefix += " "
	}

	if msg := strings.TrimSpace(upstreamMsg); msg != "" {
		return prefix + msg
	}

	rawBody := bytes.TrimSpace(responseBody)
	if len(rawBody) > 0 {
		if json.Valid(rawBody) {
			var compact bytes.Buffer
			if err := json.Compact(&compact, rawBody); err == nil {
				return prefix + logredact.TruncateLine(compact.Bytes(), 512)
			}
		}
		return prefix + logredact.TruncateLine(rawBody, 512)
	}

	return prefix + fallback
}

// ApplyForbiddenObservation handle403 处理 403 Forbidden 错误
// Antigravity 平台区分 validation/violation/generic 三种类型，均 SetError 永久禁用；
// OpenAI 与国产平台提供商的 403 使用 HTML 豁免和累计冷却；
// 其他平台调用 SetError。
func (s *HealthService) ApplyForbiddenObservation(ctx context.Context, provider *Record, observation ForbiddenObservation) (shouldDisable bool) {
	if provider.Platform == capability.PlatformAntigravity {
		return s.handleAntigravity403(ctx, provider, observation)
	}
	// Kimi 将提供商级并发/业务限制返回为 403；保留切号信号，但不把精确文案
	// 送入会永久禁用提供商的累计 403 计数器。
	if provider.Platform == capability.PlatformKimi && observation.ConcurrentRequestLimited {
		s.ApplyCNConcurrencyLimit(ctx, provider, observation.ConcurrencyReason)
		return true
	}
	if provider.Platform == capability.PlatformOpenAI || provider.IsCNProvider() {
		return s.handleOpenAI403(ctx, provider, observation)
	}
	// 非 Antigravity 平台调用 SetError。
	msg := buildForbiddenErrorMessage(
		"Access forbidden (403):",
		observation.Message,
		observation.Body,
		"provider may be suspended or lack permissions",
	)
	s.ApplyAuthenticationFailure(ctx, provider, msg)
	return true
}

func (s *HealthService) handleOpenAI403(ctx context.Context, provider *Record, observation ForbiddenObservation) (shouldDisable bool) {
	// 上游代理或 CDN 在请求到达 OpenAI API 前拦截时，可能返回 HTML 403，
	// 响应表明当前请求被代理或端点拦截，提供商凭据和权限状态保持不变。
	// 若继续计数或写提供商状态，同一个错误请求会在 failover 中逐个处罚提供商，
	// 最终把整组提供商错误地下线。这里只跳过提供商处罚，保留调用方既有切号行为。
	if observation.HTML {
		s.options.Warn(
			"openai_403_html_body_skips_provider_penalty",
			"provider_id", provider.ID,
			"upstream_message", observation.Message,
		)
		return false
	}

	msg := buildForbiddenErrorMessage(
		"Access forbidden (403):",
		observation.Message,
		observation.Body,
		"provider may be suspended or lack permissions",
	)
	return s.ApplyForbidden(ctx, provider, msg)
}

// handleAntigravity403 处理 Antigravity 平台的 403 错误
// validation（需要验证）→ 永久 SetError（需人工去 Google 验证后恢复）
// violation（违规封号）→ 永久 SetError（需人工处理）
// generic（通用禁止）→ 永久 SetError。
func (s *HealthService) handleAntigravity403(ctx context.Context, provider *Record, observation ForbiddenObservation) (shouldDisable bool) {
	fbType := observation.Kind

	switch fbType {
	case ForbiddenTypeValidation:
		// VALIDATION_REQUIRED: 永久禁用，需人工去 Google 验证后手动恢复
		msg := buildForbiddenErrorMessage(
			"Validation required (403):",
			observation.Message,
			observation.Body,
			"provider needs Google verification",
		)
		if validationURL := observation.ValidationURL; validationURL != "" {
			msg += " | validation_url: " + validationURL
		}
		s.ApplyAuthenticationFailure(ctx, provider, msg)
		return true

	case ForbiddenTypeViolation:
		// 违规封号: 永久禁用，需人工处理
		msg := buildForbiddenErrorMessage(
			"Provider violation (403):",
			observation.Message,
			observation.Body,
			"terms of service violation",
		)
		s.ApplyAuthenticationFailure(ctx, provider, msg)
		return true

	default:
		// 通用 403 调用 SetError。
		msg := buildForbiddenErrorMessage(
			"Access forbidden (403):",
			observation.Message,
			observation.Body,
			"provider may be suspended or lack permissions",
		)
		s.ApplyAuthenticationFailure(ctx, provider, msg)
		return true
	}
}
