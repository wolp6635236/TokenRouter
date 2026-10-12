package provider

import (
	"strings"
)

const (
	// CodexFingerprintOff 不做任何收敛，原样透传客户端标识。
	// 指纹模式默认关闭，管理员开启后生效。
	CodexFingerprintOff CodexFingerprintMode = "off"
	// CodexFingerprintDevice 仅收敛 installation_id 为提供商级恒定值。
	// 上游看到 1 台设备 + 多会话（每用户各自的 session）。
	CodexFingerprintDevice CodexFingerprintMode = "device"
	// CodexFingerprintSession 收敛 installation_id + session_id，
	// thread_id 从客户端 session-id 派生，每个 Codex 会话对应独立线程。
	// 上游看到 1 台设备 + 1 会话 + N 线程，最接近正常用户 spawn 子代理的模式。
	CodexFingerprintSession CodexFingerprintMode = "session"
	// CodexFingerprintFull 收敛所有标识：installation_id + session_id + thread_id。
	// 上游看到 1 台设备 + 1 会话 + 1 线程，最激进。
	CodexFingerprintFull CodexFingerprintMode = "full"

	CodexFingerprintModeExtraKey = "codex_fingerprint_mode"
	CodexFingerprintSeedExtraKey = "codex_fingerprint_seed"
)

// CodexFingerprintMode 控制 OAuth 提供商出站请求的设备指纹收敛强度。
// 多人共享同一 OAuth 提供商时，每个用户的 Codex 客户端会携带各自不同的
// installation_id / session_id / thread_id，上游据此判定设备数和会话数。
// 收敛模式将这些标识改写为提供商级恒定值，减少上游可见的设备/会话指纹。
type CodexFingerprintMode string

func CodexFingerprintModeFromExtra(extra map[string]any) CodexFingerprintMode {
	if extra == nil {
		return CodexFingerprintOff
	}
	raw, _ := extra[CodexFingerprintModeExtraKey].(string)
	switch CodexFingerprintMode(strings.TrimSpace(raw)) {
	case CodexFingerprintOff, CodexFingerprintDevice, CodexFingerprintSession, CodexFingerprintFull:
		return CodexFingerprintMode(strings.TrimSpace(raw))
	default:
		return CodexFingerprintOff
	}
}

func CodexFingerprintModeRequiresSeed(mode CodexFingerprintMode) bool {
	switch mode {
	case CodexFingerprintDevice, CodexFingerprintSession, CodexFingerprintFull:
		return true
	default:
		return false
	}
}

// ShouldEnsureCodexFingerprintSeedForExtraUpdates 判断 Extra 增量是否开启了
// Codex 指纹收敛；开启时仓储必须原子保留或生成系统管理的提供商 seed。
func ShouldEnsureCodexFingerprintSeedForExtraUpdates(updates map[string]any) bool {
	if updates == nil {
		return false
	}
	return CodexFingerprintModeRequiresSeed(CodexFingerprintModeFromExtra(updates))
}

// GetCodexFingerprintMode 返回 OAuth 提供商的指纹模式，其他类型返回关闭。
func (r *Record) GetCodexFingerprintMode() CodexFingerprintMode {
	if r == nil || !r.IsOpenAIOAuthLike() {
		return CodexFingerprintOff
	}
	return CodexFingerprintModeFromExtra(r.Extra)
}
