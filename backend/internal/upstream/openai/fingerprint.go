package openai

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// FingerprintIDs 保存统一指纹使用的 ID。
// resolveCodexFingerprintIDs 生成的实例供请求头和请求体共用，使 turn_id 等随机字段一致。
// 请求体改写时记录客户端的 client_metadata.session_id，用于识别根级 prompt_cache_key 的默认值。
// 字段用于同次尝试，并通过 json 标签排除序列化。
type FingerprintIDs struct {
	ProviderID                    int64  `json:"-"`
	Mode                          string `json:"-"`
	InstallationID                string `json:"-"`
	SessionID                     string `json:"-"`
	ThreadID                      string `json:"-"`
	TurnID                        string `json:"-"`
	WindowID                      string `json:"-"`
	TurnStartedAtUnixMs           int64  `json:"-"`
	OriginalBodySessionID         string `json:"-"`
	OriginalBodySessionIDCaptured bool   `json:"-"`
}

// ResolveFingerprintIDs 在原取时和随机 ID 时点建立本次尝试的唯一状态。
func ResolveFingerprintIDs(providerID int64, seed, clientSessionID, mode string, installation func(string) string, now func() time.Time, newTurnID func() string) *FingerprintIDs {
	ids := &FingerprintIDs{ProviderID: providerID, Mode: mode, TurnStartedAtUnixMs: now().UnixMilli()}
	ids.InstallationID = installation(seed)
	if ids.InstallationID == "" {
		return nil
	}
	switch mode {
	case "device":
		return ids
	case "session":
		ids.SessionID = ResolveConvergedSessionID(seed)
		ids.ThreadID = ResolveConvergedThreadID(seed, clientSessionID)
		if ids.ThreadID == "" {
			ids.ThreadID = ids.SessionID
		}
		ids.TurnID = newTurnID()
		ids.WindowID = ids.ThreadID + ":0"
		return ids
	case "full":
		ids.SessionID = ResolveConvergedSessionID(seed)
		ids.ThreadID = ids.SessionID
		ids.TurnID = newTurnID()
		ids.WindowID = ids.ThreadID + ":0"
		return ids
	}
	return nil
}

// ResolveConvergedSessionID 返回提供商级恒定的 session_id。
func ResolveConvergedSessionID(seed string) string {
	if seed == "" {
		return ""
	}
	// session_id 由固定前缀和 seed 派生，升级时需要保持前缀稳定。
	return DeriveStableUUIDv4("sub2api:codex-session-id:v2:" + seed)
}

// ResolveConvergedThreadID 按客户端原始 session-id 确定性派生 thread_id。
// 每个真实 Codex 会话（不同客户端启动实例）获得一个独立线程，
// 模拟正常用户 spawn 子代理或开多窗口的模式。
func ResolveConvergedThreadID(seed, clientSessionID string) string {
	if seed == "" || clientSessionID == "" {
		return ""
	}
	// thread_id 由固定前缀、seed 和客户端会话标识派生，升级时需要保持前缀稳定。
	return DeriveStableUUIDv4("sub2api:codex-thread-id:v2:" + seed + ":" + clientSessionID)
}

// ExtractClientSessionID 从请求头中提取客户端原始的会话标识。
// 优先取 session-id（连字符形式，Codex CLI 标准），回退到 session_id（下划线形式）。
// 返回的值尚未被 isolateOpenAISessionID 改写，是客户端的真实标识。
func ExtractClientSessionID(h http.Header) string {
	if v := strings.TrimSpace(h.Get("session-id")); v != "" {
		return v
	}
	return strings.TrimSpace(h.Get("session_id"))
}

// ApplyCodexFingerprintHeaders 按预计算的收敛 ID 改写出站 HTTP 头中的设备指纹。
// 在 buildUpstreamRequest 的白名单透传之后、enforceCodexIdentityHeaders 之前调用。
func ApplyCodexFingerprintHeaders(h http.Header, ids *FingerprintIDs) {
	if h == nil || ids == nil {
		return
	}

	// 所有非 off 模式都收敛 installation_id
	h.Set("x-codex-installation-id", ids.InstallationID)

	if ids.Mode == "device" {
		RewriteCodexTurnMetadataFields(h, map[string]any{
			"installation_id": ids.InstallationID,
		})
		return
	}

	// session / full 模式：改写所有相关头
	h.Set("x-codex-window-id", ids.WindowID)
	h.Set("x-client-request-id", ids.ThreadID)
	// 连字符形式和下划线形式都改写，保证一致
	h.Set("session-id", ids.SessionID)
	h.Set("session_id", ids.SessionID)
	h.Set("thread-id", ids.ThreadID)

	RewriteCodexTurnMetadataFields(h, map[string]any{
		"installation_id":         ids.InstallationID,
		"session_id":              ids.SessionID,
		"thread_id":               ids.ThreadID,
		"turn_id":                 ids.TurnID,
		"window_id":               ids.WindowID,
		"turn_started_at_unix_ms": ids.TurnStartedAtUnixMs,
	})
}

// RewriteCodexTurnMetadataFields 解析 x-codex-turn-metadata 头中的 JSON，
// 替换指定字段后回写。合法对象保留未指定字段（如 sandbox、thread_source）；
// 非法/非对象值重建为最小合法 metadata，避免 flat 与 embedded identity 分裂。
func RewriteCodexTurnMetadataFields(h http.Header, fields map[string]any) {
	raw := strings.TrimSpace(h.Get("x-codex-turn-metadata"))
	if raw == "" {
		return
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil || metadata == nil {
		metadata = make(map[string]any, len(fields))
	}
	maps.Copy(metadata, fields)
	rebuilt, err := json.Marshal(metadata)
	if err != nil {
		return
	}
	h.Set("x-codex-turn-metadata", string(rebuilt))
}

// ApplyCodexFingerprintClientMetadata 按预计算的收敛 ID 改写请求体中的 client_metadata。
// 使用与头改写相同的 ids 实例，确保 turn_id 等随机字段一致。
func ApplyCodexFingerprintClientMetadata(reqBody map[string]any, ids *FingerprintIDs) bool {
	if reqBody == nil || ids == nil {
		return false
	}

	CaptureCodexFingerprintOriginalBodySessionID(ids, reqBody["client_metadata"])
	existing, _ := reqBody["client_metadata"].(map[string]any)
	if existing == nil {
		existing = make(map[string]any)
	}

	modified := false
	if ApplyCodexFingerprintToClientMetadataMap(existing, ids) {
		reqBody["client_metadata"] = existing
		modified = true
	}
	if ApplyCodexFingerprintPromptCacheKey(reqBody, ids) {
		modified = true
	}
	return modified
}

// ApplyCodexFingerprintToClientMetadataMap 改写 client_metadata 的指纹字段。
// 已解码的 map 和透传 JSON 字节都调用此函数。
func ApplyCodexFingerprintToClientMetadataMap(existing map[string]any, ids *FingerprintIDs) bool {
	if existing == nil || ids == nil {
		return false
	}

	modified := false

	if ids.InstallationID != "" {
		existing["x-codex-installation-id"] = ids.InstallationID
		modified = true
	}

	if ids.Mode == "device" {
		RewriteClientMetadataEmbeddedTurnMetadata(existing, map[string]any{
			"installation_id": ids.InstallationID,
		})
		return modified
	}

	// session / full 模式
	existing["session_id"] = ids.SessionID
	existing["thread_id"] = ids.ThreadID
	existing["turn_id"] = ids.TurnID
	existing["x-codex-window-id"] = ids.WindowID

	RewriteClientMetadataEmbeddedTurnMetadata(existing, map[string]any{
		"installation_id":         ids.InstallationID,
		"session_id":              ids.SessionID,
		"thread_id":               ids.ThreadID,
		"turn_id":                 ids.TurnID,
		"window_id":               ids.WindowID,
		"turn_started_at_unix_ms": ids.TurnStartedAtUnixMs,
	})
	return true
}

func CaptureCodexFingerprintOriginalBodySessionID(ids *FingerprintIDs, clientMetadata any) {
	if ids == nil || ids.OriginalBodySessionIDCaptured {
		return
	}
	ids.OriginalBodySessionIDCaptured = true
	if clientMetadata == nil {
		return
	}
	switch metadata := clientMetadata.(type) {
	case map[string]any:
		if sessionID, ok := metadata["session_id"].(string); ok {
			ids.OriginalBodySessionID = strings.TrimSpace(sessionID)
		}
	case map[string]string:
		ids.OriginalBodySessionID = strings.TrimSpace(metadata["session_id"])
	}
}

func CaptureCodexFingerprintOriginalBodySessionIDRaw(ids *FingerprintIDs, value gjson.Result) {
	if ids == nil || ids.OriginalBodySessionIDCaptured {
		return
	}
	ids.OriginalBodySessionIDCaptured = true
	if value.Exists() && value.Type == gjson.String {
		ids.OriginalBodySessionID = strings.TrimSpace(value.String())
	}
}

func ShouldRewriteCodexFingerprintPromptCacheKey(ids *FingerprintIDs, promptCacheKey string) bool {
	if ids == nil || !ids.OriginalBodySessionIDCaptured || ids.OriginalBodySessionID == "" || ids.SessionID == "" {
		return false
	}
	if ids.Mode != "session" && ids.Mode != "full" {
		return false
	}
	return promptCacheKey == ids.OriginalBodySessionID
}

func ApplyCodexFingerprintPromptCacheKey(reqBody map[string]any, ids *FingerprintIDs) bool {
	if reqBody == nil {
		return false
	}
	promptCacheKey, ok := reqBody["prompt_cache_key"].(string)
	if !ok || strings.TrimSpace(promptCacheKey) == "" || !ShouldRewriteCodexFingerprintPromptCacheKey(ids, promptCacheKey) {
		return false
	}
	if promptCacheKey == ids.SessionID {
		return false
	}
	reqBody["prompt_cache_key"] = ids.SessionID
	return true
}

// ApplyCodexFingerprintClientMetadataRaw 在原始 JSON 字节上改写 client_metadata，
// 供透传路径使用。为节省数十 MB 请求体的解码开销，此处跳过全量
// Unmarshal（见 forwardOpenAIPassthrough 的轻量提取注释）。实现为：gjson 提取
// client_metadata 小对象单独解码，经共享核心改写后 sjson 一次性拼回，body
// 其余字节原样保留；root prompt_cache_key 仅在可证明是 body session 默认值时
// 做标量改写。处理规则与 ApplyCodexFingerprintClientMetadata 相同，
// 非对象值整体替换为指纹字段集合。
func ApplyCodexFingerprintClientMetadataRaw(body []byte, ids *FingerprintIDs) ([]byte, bool, error) {
	if len(body) == 0 || ids == nil {
		return body, false, nil
	}
	// 非 JSON 对象的 body（数组、标量或畸形输入）无法按字段读取 client_metadata，
	// sjson 在这类根上写字段会改写整体结构，直接放行保持原样。
	root := gjson.ParseBytes(body)
	if !root.IsObject() {
		CaptureCodexFingerprintOriginalBodySessionIDRaw(ids, gjson.Result{})
		return body, false, nil
	}

	existing := map[string]any{}
	if cm := gjson.GetBytes(body, "client_metadata"); cm.IsObject() {
		CaptureCodexFingerprintOriginalBodySessionIDRaw(ids, gjson.GetBytes(body, "client_metadata.session_id"))
		if err := json.Unmarshal([]byte(cm.Raw), &existing); err != nil {
			return body, false, fmt.Errorf("decode client_metadata for fingerprint: %w", err)
		}
	} else {
		CaptureCodexFingerprintOriginalBodySessionIDRaw(ids, gjson.Result{})
	}

	next := body
	modified := false
	if ApplyCodexFingerprintToClientMetadataMap(existing, ids) {
		raw, err := json.Marshal(existing)
		if err != nil {
			return body, false, fmt.Errorf("encode converged client_metadata: %w", err)
		}
		var setErr error
		next, setErr = sjson.SetRawBytes(body, "client_metadata", raw)
		if setErr != nil {
			return body, false, fmt.Errorf("splice converged client_metadata: %w", setErr)
		}
		modified = true
	}
	promptCacheKey := gjson.GetBytes(body, "prompt_cache_key")
	if promptCacheKey.Exists() && promptCacheKey.Type == gjson.String && strings.TrimSpace(promptCacheKey.String()) != "" && ShouldRewriteCodexFingerprintPromptCacheKey(ids, promptCacheKey.String()) {
		rewritten, err := sjson.SetBytes(next, "prompt_cache_key", ids.SessionID)
		if err != nil {
			return body, false, fmt.Errorf("splice converged prompt_cache_key: %w", err)
		}
		next = rewritten
		modified = true
	}
	return next, modified, nil
}

// RewriteClientMetadataEmbeddedTurnMetadata 改写 client_metadata 中内嵌的
// x-codex-turn-metadata JSON 字符串里的指定字段。非法/非对象值会重建，
// 避免 flat client_metadata 与 embedded metadata 暴露两套身份。
func RewriteClientMetadataEmbeddedTurnMetadata(clientMetadata map[string]any, fields map[string]any) {
	raw, ok := clientMetadata["x-codex-turn-metadata"].(string)
	if !ok || raw == "" {
		return
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil || metadata == nil {
		metadata = make(map[string]any, len(fields))
	}
	maps.Copy(metadata, fields)
	if rebuilt, err := json.Marshal(metadata); err == nil {
		clientMetadata["x-codex-turn-metadata"] = string(rebuilt)
	}
}
