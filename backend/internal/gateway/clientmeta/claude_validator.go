package clientmeta

import (
	"regexp"
	"strings"

	wire "github.com/TokenFlux/TokenRouter/internal/protocol/anthropic"
)

const (
	// 安全监视器请求按以下标记识别，其余提示词措辞可变化。
	claudeCodeSecurityMonitorPromptPrefix = "You are a security monitor for autonomous AI coding agents."
	claudeCodeSecurityMonitorPromptMinLen = 10_000

	// claudeCodeBillingHeaderPrefix 是 Claude Code 在 system 首块注入的计费归因前缀。
	// 多数 CLI 请求携带该块，包括部分缺少身份文本的子请求；缺少该块的固定辅助请求由单独规则识别。
	// 生成位置见 gateway_billing_block.go，同类识别见 protocol/bridge/anthropic_to_responses.go。
	claudeCodeBillingHeaderPrefix = wire.ClaudeCodeBillingHeaderPrefix
	// claudeCodeEntrypointMarker 标识计费块的入口字段，检查字段存在即可。
	// cli、claude-vscode、jetbrains、sdk 等入口值随客户端扩展，且该值可由请求方填写。
	claudeCodeEntrypointMarker = wire.ClaudeCodeEntrypointMarker

	// ClaudeCodeSystemPromptThreshold 保留既有相似度门槛。
	ClaudeCodeSystemPromptThreshold = 0.5

	// ClaudeCodeSecurityMonitorPrefix 供兼容测试与请求识别复用同一协议文本。
	ClaudeCodeSecurityMonitorPrefix = claudeCodeSecurityMonitorPromptPrefix
)

var (
	// User-Agent 匹配: claude-cli/x.x.x (仅支持官方 CLI，大小写不敏感)。
	claudeCodeUAPattern = regexp.MustCompile(`(?i)^claude-cli/\d+\.\d+\.\d+`)

	// 带捕获组的版本提取正则。

	// System prompt 相似度阈值（默认 0.5，和 claude-relay-service 一致）。
	systemPromptThreshold = ClaudeCodeSystemPromptThreshold

	// Claude Code 官方 System Prompt 模板
	// 从 claude-relay-service/src/utils/contents.js 提取。
	claudeCodeSystemPrompts = []string{
		// claudeOtherSystemPrompt1 - Primary
		"You are Claude Code, Anthropic's official CLI for Claude.",

		// claudeOtherSystemPrompt3 - Agent SDK
		"You are a Claude agent, built on Anthropic's Claude Agent SDK.",

		// claudeOtherSystemPrompt4 - Compact Agent SDK
		"You are Claude Code, Anthropic's official CLI for Claude, running within the Claude Agent SDK.",

		// exploreAgentSystemPrompt
		"You are a file search specialist for Claude Code, Anthropic's official CLI for Claude.",

		// claudeOtherSystemPromptCompact - Compact (用于对话摘要)
		"You are a helpful AI assistant tasked with summarizing conversations.",

		// claudeOtherSystemPrompt2 是长系统提示词中的辅助识别片段。
		"You are an interactive CLI tool that helps users",
	}

	// claudeCodeSecurityMonitorMarkers 与固定前缀、长度下限共同构成安全监视器提示词的判别条件。
	claudeCodeSecurityMonitorMarkers = []string{
		"## Threat Model",
		"- `<transcript>`:",
		"## HARD BLOCK",
		"## SOFT BLOCK",
		"## Classification Process",
		"## Output Format",
		"<block>yes</block><reason>",
		"<block>no</block>",
	}
)

// ClaudeCodeValidator 验证请求是否来自 Claude Code 客户端
// 完全学习自 claude-relay-service 项目的验证逻辑。
type ClaudeCodeValidator struct{}

// ClaudeCodeValidationInput 保存 HTTP 层同步提取的客户端识别数据。
type ClaudeCodeValidationInput struct {
	Path              string
	UserAgent         string
	XApp              string
	AnthropicBeta     string
	AnthropicVersion  string
	MaxTokensOneHaiku bool
}

// IsClaudeCodeClient 同时检查 CLI User-Agent 与 metadata 身份格式。
// 仅有伪装的 User-Agent 或非空 user_id 不足以跳过请求伪装。
func IsClaudeCodeClient(userAgent, metadataUserID string) bool {
	return claudeCodeUAPattern.MatchString(userAgent) && wire.ParseMetadataUserID(metadataUserID) != nil
}

// NewClaudeCodeValidator 创建验证器实例。
func NewClaudeCodeValidator() *ClaudeCodeValidator {
	return &ClaudeCodeValidator{}
}

// Validate 验证请求是否来自 Claude Code CLI
// 采用与 claude-relay-service 完全一致的验证策略：
//
//	Step 1: User-Agent 检查 (必需) - 必须是 claude-cli/x.x.x
//	Step 2: 对于非 messages 路径和 /messages/count_tokens，只要 UA 匹配就通过
//	Step 3: 检查 max_tokens=1 + haiku 探测请求绕过（UA 已验证）
//	Step 4: 对于 messages 路径，进行严格验证：
//	        - System prompt 相似度检查
//	        - X-App header 检查
//	        - anthropic-beta header 检查
//	        - anthropic-version header 检查
//	        - metadata.user_id 格式验证
func (v *ClaudeCodeValidator) Validate(input ClaudeCodeValidationInput, body map[string]any) bool {
	// Step 1: User-Agent 检查
	ua := input.UserAgent
	if !claudeCodeUAPattern.MatchString(ua) {
		return false
	}

	// Step 2: 非 messages 路径只要 UA 匹配就通过
	path := input.Path
	if !strings.Contains(path, "messages") {
		return true
	}

	// count_tokens 是 Claude Code 官方辅助请求，通常不携带完整 messages system prompt。
	if isMessagesCountTokensPath(path) {
		return true
	}

	// Step 3: 检查 max_tokens=1 + haiku 探测请求绕过
	// 这类请求用于 Claude Code 验证 API 连通性，不携带 system prompt
	if input.MaxTokensOneHaiku {
		return true // 绕过 system prompt 检查，UA 已在 Step 1 验证
	}

	// Step 4: messages 路径，进行严格验证

	// 4.1 检查 system prompt 相似度
	if !v.hasClaudeCodeSystemPrompt(body) {
		return false
	}

	// 4.2 检查必需的 headers（值不为空即可）
	xApp := input.XApp
	if xApp == "" {
		return false
	}

	anthropicBeta := input.AnthropicBeta
	if anthropicBeta == "" {
		return false
	}

	anthropicVersion := input.AnthropicVersion
	if anthropicVersion == "" {
		return false
	}

	// 4.3 验证 metadata.user_id
	if body == nil {
		return false
	}

	metadata, ok := body["metadata"].(map[string]any)
	if !ok {
		return false
	}

	userID, ok := metadata["user_id"].(string)
	if !ok || userID == "" {
		return false
	}

	if wire.ParseMetadataUserID(userID) == nil {
		return false
	}

	return true
}

func isMessagesCountTokensPath(path string) bool {
	return strings.HasSuffix(path, "/messages/count_tokens")
}

// hasClaudeCodeSystemPrompt 检查请求是否包含 Claude Code 系统提示词
// 使用字符串相似度匹配（Dice coefficient）。
func (v *ClaudeCodeValidator) hasClaudeCodeSystemPrompt(body map[string]any) bool {
	if body == nil {
		return false
	}

	// 检查 model 字段
	if _, ok := body["model"].(string); !ok {
		return false
	}

	// 获取 system 字段
	systemEntries, ok := body["system"].([]any)
	if !ok {
		return false
	}

	if isClaudeCodeSecurityMonitorPrompt(systemEntries) {
		return true
	}

	// 检查每个 system entry
	for _, entry := range systemEntries {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			continue
		}

		text, ok := entryMap["text"].(string)
		if !ok || text == "" {
			continue
		}

		// 计费归因块识别（原因见 claudeCodeBillingHeaderPrefix 注释）。先于 Dice 检查，
		// 大小写敏感：该块由 gateway_billing_block.go 固定小写生成。
		if strings.HasPrefix(text, claudeCodeBillingHeaderPrefix) &&
			strings.Contains(text, claudeCodeEntrypointMarker) {
			return true
		}

		// 计算与所有模板的最佳相似度
		bestScore := v.BestSimilarityScore(text)
		if bestScore >= systemPromptThreshold {
			return true
		}
	}

	return false
}

// isClaudeCodeSecurityMonitorPrompt 逐块识别缺少计费块的安全监视器提示词，
// CLI 可以在提示词前后追加会话上下文块。
func isClaudeCodeSecurityMonitorPrompt(systemEntries []any) bool {
	for _, rawEntry := range systemEntries {
		entry, ok := rawEntry.(map[string]any)
		if !ok {
			continue
		}

		entryType, ok := entry["type"].(string)
		if !ok || entryType != "text" {
			continue
		}

		text, ok := entry["text"].(string)
		if !ok || len(text) < claudeCodeSecurityMonitorPromptMinLen ||
			!strings.HasPrefix(text, claudeCodeSecurityMonitorPromptPrefix) {
			continue
		}

		if hasAllClaudeCodeSecurityMonitorMarkers(text) {
			return true
		}
	}

	return false
}

func hasAllClaudeCodeSecurityMonitorMarkers(text string) bool {
	for _, marker := range claudeCodeSecurityMonitorMarkers {
		if !strings.Contains(text, marker) {
			return false
		}
	}

	return true
}

// BestSimilarityScore 计算文本与所有 Claude Code 模板的最佳相似度。
func (v *ClaudeCodeValidator) BestSimilarityScore(text string) float64 {
	normalizedText := normalizePrompt(text)
	bestScore := 0.0

	for _, template := range claudeCodeSystemPrompts {
		normalizedTemplate := normalizePrompt(template)
		score := DiceCoefficient(normalizedText, normalizedTemplate)
		if score > bestScore {
			bestScore = score
		}
	}

	return bestScore
}

// normalizePrompt 标准化提示词文本（去除多余空白）。
func normalizePrompt(text string) string {
	// 将所有空白字符替换为单个空格，并去除首尾空白
	return strings.Join(strings.Fields(text), " ")
}

// DiceCoefficient 计算两个字符串的 Dice 系数（Sørensen–Dice coefficient）
// 这是 string-similarity 库使用的算法
// 公式: 2 * |intersection| / (|bigrams(a)| + |bigrams(b)|)。
func DiceCoefficient(a, b string) float64 {
	if a == b {
		return 1.0
	}

	if len(a) < 2 || len(b) < 2 {
		return 0.0
	}

	// 生成 bigrams
	bigramsA := getBigrams(a)
	bigramsB := getBigrams(b)

	if len(bigramsA) == 0 || len(bigramsB) == 0 {
		return 0.0
	}

	// 计算交集大小
	intersection := 0
	for bigram, countA := range bigramsA {
		if countB, exists := bigramsB[bigram]; exists {
			if countA < countB {
				intersection += countA
			} else {
				intersection += countB
			}
		}
	}

	// 计算总 bigram 数量
	totalA := 0
	for _, count := range bigramsA {
		totalA += count
	}
	totalB := 0
	for _, count := range bigramsB {
		totalB += count
	}

	return float64(2*intersection) / float64(totalA+totalB)
}

// getBigrams 获取字符串的所有 bigrams（相邻字符对）。
func getBigrams(s string) map[string]int {
	bigrams := make(map[string]int)
	runes := []rune(strings.ToLower(s))

	for i := range len(runes) - 1 {
		bigram := string(runes[i : i+2])
		bigrams[bigram]++
	}

	return bigrams
}

// ValidateUserAgent 仅验证 User-Agent（用于不需要解析请求体的场景）。
func (v *ClaudeCodeValidator) ValidateUserAgent(ua string) bool {
	return claudeCodeUAPattern.MatchString(ua)
}

// ExtractVersion 从 User-Agent 中提取 Claude Code 版本号
// 返回 "2.1.22" 形式的版本号，如果不匹配返回空字符串。
func (v *ClaudeCodeValidator) ExtractVersion(ua string) string {
	return ExtractClaudeCLIVersion(ua)
}

// IsHaikuProbe 检查 Haiku 模型的单 token 连通性探测。
func IsHaikuProbe(model string, maxTokens int) bool {
	return maxTokens == 1 && strings.Contains(strings.ToLower(model), "haiku")
}
