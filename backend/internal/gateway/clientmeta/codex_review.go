package clientmeta

import (
	"strings"

	"github.com/tidwall/gjson"

	protocolopenai "github.com/TokenFlux/TokenRouter/internal/protocol/openai"
)

// Codex 审查线索记录请求声明，权限校验和提供商选择由各自的入口处理。
const (
	CodexAutoReviewModel      = "codex-auto-review"
	OpenAISubagentHeader      = "x-openai-subagent"
	CodexParentThreadIDHeader = "x-codex-parent-thread-id"
	CodexTurnMetadataHeader   = "x-codex-turn-metadata"
)

type CodexReviewInput struct {
	Model, Subagent, ParentThreadID, TurnMetadata string
	Body                                          []byte
}

// CodexReviewParent 检查请求模型和各类审查声明，返回匹配且无冲突的父线程标识。
func CodexReviewParent(input CodexReviewInput) string {
	if !IsCodexReviewModel(input.Model) {
		return ""
	}
	headerMetadata := input.TurnMetadata
	bodyMetadata := protocolopenai.RequestPayloadView(input.Body).Get("client_metadata.x-codex-turn-metadata").String()
	if !hasUnambiguousOpenAICodexReviewSubagent(
		input.Subagent,
		codexSubagentKindFromMetadata(headerMetadata),
		codexSubagentKindFromMetadata(bodyMetadata),
	) {
		return ""
	}

	parentID := ""
	for _, candidate := range []string{
		strings.TrimSpace(input.ParentThreadID),
		codexParentThreadIDFromMetadata(headerMetadata),
		codexParentThreadIDFromMetadata(bodyMetadata),
	} {
		if candidate == "" {
			continue
		}
		if parentID != "" && parentID != candidate {
			return ""
		}
		parentID = candidate
	}
	if parentID == "" {
		return ""
	}

	return parentID
}

func codexParentThreadIDFromMetadata(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || !gjson.Valid(raw) {
		return ""
	}
	return strings.TrimSpace(gjson.Get(raw, "parent_thread_id").String())
}

func codexSubagentKindFromMetadata(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || !gjson.Valid(raw) {
		return ""
	}
	return strings.TrimSpace(gjson.Get(raw, "subagent_kind").String())
}

func hasUnambiguousOpenAICodexReviewSubagent(candidates ...string) bool {
	subagent := ""
	for _, candidate := range candidates {
		candidate = strings.ToLower(strings.TrimSpace(candidate))
		if candidate == "" {
			continue
		}
		if subagent != "" && subagent != candidate {
			return false
		}
		subagent = candidate
	}
	return subagent == "guardian" || subagent == "review"
}

// IsCodexReviewModel 判断请求模型是否需要读取 Codex 审查线索。
func IsCodexReviewModel(model string) bool {
	return strings.EqualFold(strings.TrimSpace(model), CodexAutoReviewModel)
}
