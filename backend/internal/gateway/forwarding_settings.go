package gateway

import (
	"context"
	"log/slog"
	"strings"
	"time"
)

const (
	// 转发设置的持久化键和 TTFT 模式值如下。
	SettingKeyClaudeOAuthSystemPrompt                = "claude_oauth_system_prompt"
	SettingKeyClaudeOAuthSystemPromptBlocks          = "claude_oauth_system_prompt_blocks"
	SettingKeyEnableAnthropicCacheTTL1hInjection     = "enable_anthropic_cache_ttl_1h_injection"
	SettingKeyEnableCCHSigning                       = "enable_cch_signing"
	SettingKeyEnableClaudeOAuthSystemPromptInjection = "enable_claude_oauth_system_prompt_injection"
	SettingKeyEnableClientDatelineNormalization      = "enable_client_dateline_normalization"
	SettingKeyEnableFingerprintUnification           = "enable_fingerprint_unification"
	SettingKeyEnableMetadataPassthrough              = "enable_metadata_passthrough"
	SettingKeyMaxClaudeCodeVersion                   = "max_claude_code_version"
	SettingKeyMinClaudeCodeVersion                   = "min_claude_code_version"
	SettingKeyOpenAITTFTMode                         = "openai_ttft_mode"
	SettingKeyRewriteMessageCacheControl             = "rewrite_message_cache_control"
	OpenAITTFTModeSemantic                           = "semantic"
	OpenAITTFTModeVisible                            = "visible"

	// versionBoundsCacheTTL 缓存有效期。
	versionBoundsCacheTTL = 60 * time.Second

	// versionBoundsErrorTTL DB 错误时的短缓存，快速重试。
	versionBoundsErrorTTL = 5 * time.Second

	// versionBoundsDBTimeout singleflight 内 DB 查询超时，独立于请求 context。
	versionBoundsDBTimeout = 5 * time.Second

	gatewayForwardingCacheTTL = 60 * time.Second
	gatewayForwardingErrorTTL = 5 * time.Second

	// ForwardingSettingsReadTimeout 限制转发设置读取的回源时长。
	ForwardingSettingsReadTimeout = 5 * time.Second
	gatewayForwardingDBTimeout    = ForwardingSettingsReadTimeout
)

// cachedVersionBounds 缓存 Claude Code 版本号上下限（进程内缓存，60s TTL）。
type cachedVersionBounds struct {
	min       string // 空字符串 = 不检查
	max       string // 空字符串 = 不检查
	expiresAt int64  // unix nano
}

// cachedGatewayForwardingSettings 缓存网关转发行为设置（进程内缓存，60s TTL）。
type cachedGatewayForwardingSettings struct {
	openAITTFTMode                   string
	fingerprintUnification           bool
	metadataPassthrough              bool
	cchSigning                       bool
	claudeOAuthSystemPromptInjection bool
	claudeOAuthSystemPrompt          string
	claudeOAuthSystemPromptBlocks    string
	anthropicCacheTTL1hInjection     bool
	rewriteMessageCacheControl       bool
	clientDatelineNormalization      bool
	expiresAt                        int64 // unix nano
}

type gatewayForwardingSettingsResult struct {
	openAITTFTMode                                                                        string
	fp, mp, cch, claudeOAuthSystemPromptInjection, cacheTTL1h, rewriteMessageCacheControl bool
	clientDatelineNormalization                                                           bool
	claudeOAuthSystemPrompt, claudeOAuthSystemPromptBlocks                                string
}

// ForwardingSnapshot 是综合设置提交后的只读发布输入。
type ForwardingSnapshot struct {
	OpenAITTFTMode                   string
	FingerprintUnification           bool
	MetadataPassthrough              bool
	CCHSigning                       bool
	ClaudeOAuthSystemPromptInjection bool
	ClaudeOAuthSystemPrompt          string
	ClaudeOAuthSystemPromptBlocks    string
	AnthropicCacheTTL1hInjection     bool
	RewriteMessageCacheControl       bool
	ClientDatelineNormalization      bool
}

// GetGrokDefaultBaseURLMode 在读取预算内查询 Grok Base URL 模式，缺省使用 CLI。
func (s *RuntimeSettings) GetGrokDefaultBaseURLMode(ctx context.Context) string {
	if s == nil || s.settingRepo == nil {
		return "cli"
	}
	dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), gatewayForwardingDBTimeout)
	defer cancel()
	raw, err := s.settingRepo.GetValue(dbCtx, SettingKeyGrokDefaultBaseURLMode)
	if err != nil {
		return "cli"
	}
	return NormalizeGrokDefaultBaseURLMode(raw)
}

// NormalizeOpenAITTFTMode 接受 visible，其他值使用 semantic。
func NormalizeOpenAITTFTMode(mode string) string {
	if strings.EqualFold(strings.TrimSpace(mode), OpenAITTFTModeVisible) {
		return OpenAITTFTModeVisible
	}
	return OpenAITTFTModeSemantic
}

// getGatewayForwardingSettingsCached 读取实例内的转发设置缓存，过期后合并并发查询。
func (s *RuntimeSettings) getGatewayForwardingSettingsCached(ctx context.Context) gatewayForwardingSettingsResult {
	if cached, ok := s.gatewayForwardingCache.Load().(*cachedGatewayForwardingSettings); ok && cached != nil {
		if time.Now().UnixNano() < cached.expiresAt {
			return gatewayForwardingSettingsResult{
				openAITTFTMode:                   cached.openAITTFTMode,
				fp:                               cached.fingerprintUnification,
				mp:                               cached.metadataPassthrough,
				cch:                              cached.cchSigning,
				claudeOAuthSystemPromptInjection: cached.claudeOAuthSystemPromptInjection,
				claudeOAuthSystemPrompt:          cached.claudeOAuthSystemPrompt,
				claudeOAuthSystemPromptBlocks:    cached.claudeOAuthSystemPromptBlocks,
				cacheTTL1h:                       cached.anthropicCacheTTL1hInjection,
				rewriteMessageCacheControl:       cached.rewriteMessageCacheControl,
				clientDatelineNormalization:      cached.clientDatelineNormalization,
			}
		}
	}
	val, _, _ := s.gatewayForwardingSF.Do("gateway_forwarding", func() (any, error) {
		if cached, ok := s.gatewayForwardingCache.Load().(*cachedGatewayForwardingSettings); ok && cached != nil {
			if time.Now().UnixNano() < cached.expiresAt {
				return gatewayForwardingSettingsResult{
					openAITTFTMode:                   cached.openAITTFTMode,
					fp:                               cached.fingerprintUnification,
					mp:                               cached.metadataPassthrough,
					cch:                              cached.cchSigning,
					claudeOAuthSystemPromptInjection: cached.claudeOAuthSystemPromptInjection,
					claudeOAuthSystemPrompt:          cached.claudeOAuthSystemPrompt,
					claudeOAuthSystemPromptBlocks:    cached.claudeOAuthSystemPromptBlocks,
					cacheTTL1h:                       cached.anthropicCacheTTL1hInjection,
					rewriteMessageCacheControl:       cached.rewriteMessageCacheControl,
					clientDatelineNormalization:      cached.clientDatelineNormalization,
				}, nil
			}
		}
		dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), gatewayForwardingDBTimeout)
		defer cancel()
		values, err := s.settingRepo.GetMultiple(dbCtx, []string{
			SettingKeyOpenAITTFTMode,
			SettingKeyEnableFingerprintUnification,
			SettingKeyEnableMetadataPassthrough,
			SettingKeyEnableCCHSigning,
			SettingKeyEnableClaudeOAuthSystemPromptInjection,
			SettingKeyClaudeOAuthSystemPrompt,
			SettingKeyClaudeOAuthSystemPromptBlocks,
			SettingKeyEnableAnthropicCacheTTL1hInjection,
			SettingKeyRewriteMessageCacheControl,
			SettingKeyEnableClientDatelineNormalization,
		})
		if err != nil {
			slog.Warn("failed to get gateway forwarding settings", "error", err)
			s.gatewayForwardingCache.Store(&cachedGatewayForwardingSettings{
				openAITTFTMode:                   OpenAITTFTModeSemantic,
				fingerprintUnification:           true,
				metadataPassthrough:              false,
				cchSigning:                       false,
				claudeOAuthSystemPromptInjection: true,
				anthropicCacheTTL1hInjection:     false,
				rewriteMessageCacheControl:       false,
				clientDatelineNormalization:      true,
				expiresAt:                        time.Now().Add(gatewayForwardingErrorTTL).UnixNano(),
			})
			return gatewayForwardingSettingsResult{
				openAITTFTMode:                   OpenAITTFTModeSemantic,
				fp:                               true,
				claudeOAuthSystemPromptInjection: true,
				rewriteMessageCacheControl:       false,
				clientDatelineNormalization:      true,
			}, nil
		}
		ttftMode := NormalizeOpenAITTFTMode(values[SettingKeyOpenAITTFTMode])
		fp := true
		if v, ok := values[SettingKeyEnableFingerprintUnification]; ok && v != "" {
			fp = v == "true"
		}
		mp := values[SettingKeyEnableMetadataPassthrough] == "true"
		cch := values[SettingKeyEnableCCHSigning] == "true"
		claudeOAuthSystemPromptInjection := true
		if v, ok := values[SettingKeyEnableClaudeOAuthSystemPromptInjection]; ok && v != "" {
			claudeOAuthSystemPromptInjection = v == "true"
		}
		claudeOAuthSystemPrompt := values[SettingKeyClaudeOAuthSystemPrompt]
		claudeOAuthSystemPromptBlocks := values[SettingKeyClaudeOAuthSystemPromptBlocks]
		cacheTTL1h := values[SettingKeyEnableAnthropicCacheTTL1hInjection] == "true"
		rewriteMessageCacheControl := false
		if v, ok := values[SettingKeyRewriteMessageCacheControl]; ok && v != "" {
			rewriteMessageCacheControl = v == "true"
		}
		clientDatelineNormalization := true
		if v, ok := values[SettingKeyEnableClientDatelineNormalization]; ok && v != "" {
			clientDatelineNormalization = v == "true"
		}
		s.gatewayForwardingCache.Store(&cachedGatewayForwardingSettings{
			openAITTFTMode:                   ttftMode,
			fingerprintUnification:           fp,
			metadataPassthrough:              mp,
			cchSigning:                       cch,
			claudeOAuthSystemPromptInjection: claudeOAuthSystemPromptInjection,
			claudeOAuthSystemPrompt:          claudeOAuthSystemPrompt,
			claudeOAuthSystemPromptBlocks:    claudeOAuthSystemPromptBlocks,
			anthropicCacheTTL1hInjection:     cacheTTL1h,
			rewriteMessageCacheControl:       rewriteMessageCacheControl,
			clientDatelineNormalization:      clientDatelineNormalization,
			expiresAt:                        time.Now().Add(gatewayForwardingCacheTTL).UnixNano(),
		})
		return gatewayForwardingSettingsResult{
			openAITTFTMode:                   ttftMode,
			fp:                               fp,
			mp:                               mp,
			cch:                              cch,
			claudeOAuthSystemPromptInjection: claudeOAuthSystemPromptInjection,
			claudeOAuthSystemPrompt:          claudeOAuthSystemPrompt,
			claudeOAuthSystemPromptBlocks:    claudeOAuthSystemPromptBlocks,
			cacheTTL1h:                       cacheTTL1h,
			rewriteMessageCacheControl:       rewriteMessageCacheControl,
			clientDatelineNormalization:      clientDatelineNormalization,
		}, nil
	})
	if r, ok := val.(gatewayForwardingSettingsResult); ok {
		return r
	}
	return gatewayForwardingSettingsResult{openAITTFTMode: OpenAITTFTModeSemantic, fp: true, claudeOAuthSystemPromptInjection: true, clientDatelineNormalization: true}
}

// GetOpenAITTFTMode 从转发设置缓存读取并规范化首 token 计时模式。
func (s *RuntimeSettings) GetOpenAITTFTMode(ctx context.Context) string {
	return NormalizeOpenAITTFTMode(s.getGatewayForwardingSettingsCached(ctx).openAITTFTMode)
}

// GetGatewayForwardingSettings 返回指纹统一、元数据透传和 CCH 签名开关。
func (s *RuntimeSettings) GetGatewayForwardingSettings(ctx context.Context) (fingerprintUnification, metadataPassthrough, cchSigning bool) {
	result := s.getGatewayForwardingSettingsCached(ctx)
	return result.fp, result.mp, result.cch
}

// IsAnthropicCacheTTL1hInjectionEnabled 读取 Anthropic 一小时缓存 TTL 注入开关。
func (s *RuntimeSettings) IsAnthropicCacheTTL1hInjectionEnabled(ctx context.Context) bool {
	return s.getGatewayForwardingSettingsCached(ctx).cacheTTL1h
}

// IsRewriteMessageCacheControlEnabled 读取消息 cache_control 改写开关。
func (s *RuntimeSettings) IsRewriteMessageCacheControlEnabled(ctx context.Context) bool {
	return s.getGatewayForwardingSettingsCached(ctx).rewriteMessageCacheControl
}

// IsClientDatelineNormalizationEnabled 读取客户端日期行规范化开关。
func (s *RuntimeSettings) IsClientDatelineNormalizationEnabled(ctx context.Context) bool {
	return s.getGatewayForwardingSettingsCached(ctx).clientDatelineNormalization
}

// GetClaudeOAuthSystemPromptInjectionSettings 返回 Claude OAuth 提示词注入开关、文本和内容块。
func (s *RuntimeSettings) GetClaudeOAuthSystemPromptInjectionSettings(ctx context.Context) (enabled bool, prompt string, blocks string) {
	result := s.getGatewayForwardingSettingsCached(ctx)
	return result.claudeOAuthSystemPromptInjection, result.claudeOAuthSystemPrompt, result.claudeOAuthSystemPromptBlocks
}

// GetClaudeCodeVersionBounds 读取版本范围缓存，过期后查询 Claude Code 的最低和最高允许版本。
func (s *RuntimeSettings) GetClaudeCodeVersionBounds(ctx context.Context) (min, max string) {
	if cached, ok := s.versionBoundsCache.Load().(*cachedVersionBounds); ok {
		if time.Now().UnixNano() < cached.expiresAt {
			return cached.min, cached.max
		}
	}
	// singleflight: 同一时刻只有一个 goroutine 查询 DB，其余复用结果
	type bounds struct{ min, max string }
	result, err, _ := s.versionBoundsSF.Do("version_bounds", func() (any, error) {
		// 进入合并查询后再次检查缓存，前一个查询可能已填充结果。
		if cached, ok := s.versionBoundsCache.Load().(*cachedVersionBounds); ok {
			if time.Now().UnixNano() < cached.expiresAt {
				return bounds{cached.min, cached.max}, nil
			}
		}
		// 使用独立 context 完成配置查询，客户端断开后仍可取得有效值并填充缓存。
		dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), versionBoundsDBTimeout)
		defer cancel()
		values, err := s.settingRepo.GetMultiple(dbCtx, []string{
			SettingKeyMinClaudeCodeVersion,
			SettingKeyMaxClaudeCodeVersion,
		})
		if err != nil {
			// fail-open: DB 错误时不阻塞请求，但记录日志并使用短 TTL 快速重试
			slog.Warn("failed to get claude code version bounds setting, skipping version check", "error", err)
			s.versionBoundsCache.Store(&cachedVersionBounds{
				min:       "",
				max:       "",
				expiresAt: time.Now().Add(versionBoundsErrorTTL).UnixNano(),
			})
			return bounds{"", ""}, nil
		}
		b := bounds{
			min: values[SettingKeyMinClaudeCodeVersion],
			max: values[SettingKeyMaxClaudeCodeVersion],
		}
		s.versionBoundsCache.Store(&cachedVersionBounds{
			min:       b.min,
			max:       b.max,
			expiresAt: time.Now().Add(versionBoundsCacheTTL).UnixNano(),
		})
		return b, nil
	})
	if err != nil {
		return "", ""
	}
	b, ok := result.(bounds)
	if !ok {
		return "", ""
	}
	return b.min, b.max
}

// PublishForwarding 按原更新顺序发布已持久化值，TTL 和 singleflight 行为保持。
func (s *RuntimeSettings) PublishForwarding(minVersion, maxVersion string, value ForwardingSnapshot) {
	s.versionBoundsSF.Forget("version_bounds")
	s.versionBoundsCache.Store(&cachedVersionBounds{min: minVersion, max: maxVersion, expiresAt: time.Now().Add(versionBoundsCacheTTL).UnixNano()})
	s.gatewayForwardingSF.Forget("gateway_forwarding")
	s.gatewayForwardingCache.Store(&cachedGatewayForwardingSettings{
		openAITTFTMode:                   value.OpenAITTFTMode,
		fingerprintUnification:           value.FingerprintUnification,
		metadataPassthrough:              value.MetadataPassthrough,
		cchSigning:                       value.CCHSigning,
		claudeOAuthSystemPromptInjection: value.ClaudeOAuthSystemPromptInjection,
		claudeOAuthSystemPrompt:          value.ClaudeOAuthSystemPrompt,
		claudeOAuthSystemPromptBlocks:    value.ClaudeOAuthSystemPromptBlocks,
		anthropicCacheTTL1hInjection:     value.AnthropicCacheTTL1hInjection,
		rewriteMessageCacheControl:       value.RewriteMessageCacheControl,
		clientDatelineNormalization:      value.ClientDatelineNormalization,
		expiresAt:                        time.Now().Add(gatewayForwardingCacheTTL).UnixNano(),
	})
}

// InvalidateForwarding 使当前实例的缓存失效，下次读取重新查询存储。
func (s *RuntimeSettings) InvalidateForwarding() {
	s.gatewayForwardingSF.Forget("gateway_forwarding")
	s.gatewayForwardingCache.Store(&cachedGatewayForwardingSettings{})
}

// IsIdentityPatchEnabled 在请求时读取身份修补开关，读取失败时返回 true。
func (s *RuntimeSettings) IsIdentityPatchEnabled(ctx context.Context) bool {
	value, err := s.settingRepo.GetValue(ctx, SettingKeyEnableIdentityPatch)
	if err != nil {
		return true
	}
	return value == "true"
}

// GetIdentityPatchPrompt 读取身份修补提示词，读取失败时返回空字符串。
func (s *RuntimeSettings) GetIdentityPatchPrompt(ctx context.Context) string {
	value, err := s.settingRepo.GetValue(ctx, SettingKeyIdentityPatchPrompt)
	if err != nil {
		return ""
	}
	return value
}
