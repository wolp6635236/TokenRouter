package upstream

import (
	"fmt"
	"strings"
)

// 网关会把 Responses 子路径、Gemini 模型名等客户端输入拼进上游 URL。
// 校验采用字符允许清单，清单外的字符拒绝转发，防止输入改变路径结构。
// c.Request.URL.Path 已经过百分号解码，需要在拼接上游路径前校验。
// 不合规输入返回错误；自动修正路径会改变请求目标并掩盖调用方错误。

const (
	// MaxUpstreamPathSegmentLen 单个路径片段长度上限。真实的 response id、模型名
	// 都远短于此，留足余量只为拒绝异常输入。
	MaxUpstreamPathSegmentLen = 128
	// MaxUpstreamPathSegments 后缀允许的片段数上限（如 /{id}/cancel 为 2）。
	MaxUpstreamPathSegments = 8
)

// IsSafeUpstreamPathSegmentByte 是闭集允许清单：只放行 `\w`（即 [A-Za-z0-9_]）
// 以及真实取值必需的 `-` 与 `.`。其余字符（含控制字符与非 ASCII）一律拒绝。
func IsSafeUpstreamPathSegmentByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	case b == '_', b == '-', b == '.':
		return true
	default:
		return false
	}
}

// IsSafeUpstreamPathSegment 判断 segment 能否原样拼进上游 URL 的一个 path 片段。
//
// 允许清单中的 `.` 有路径含义。各类实现对纯点片段的解释不同，因此拒绝这类片段。
func IsSafeUpstreamPathSegment(segment string) bool {
	if segment == "" || len(segment) > MaxUpstreamPathSegmentLen {
		return false
	}
	dotsOnly := true
	for i := range len(segment) {
		if !IsSafeUpstreamPathSegmentByte(segment[i]) {
			return false
		}
		if segment[i] != '.' {
			dotsOnly = false
		}
	}
	return !dotsOnly
}

// SanitizedUpstreamPathSuffix 校验 "/a/b" 形态的路径后缀。
// ok=false 时调用方需要拒绝请求，替换为空后缀会改变 /responses/compact 等请求的目标。
// 空后缀合法，表示没有子路径。
func SanitizedUpstreamPathSuffix(raw string) (string, bool) {
	suffix := strings.TrimSpace(raw)
	if suffix == "" {
		return "", true
	}
	if !strings.HasPrefix(suffix, "/") {
		return "", false
	}
	segments := strings.Split(strings.TrimPrefix(suffix, "/"), "/")
	if len(segments) > MaxUpstreamPathSegments {
		return "", false
	}
	for _, segment := range segments {
		if !IsSafeUpstreamPathSegment(segment) {
			return "", false
		}
	}
	return suffix, true
}

// ValidateUpstreamPathSegment 供 URL 构造点使用：不合规的片段直接变成显式错误，
// 不再继续构造与发出上游请求。
func ValidateUpstreamPathSegment(kind, segment string) error {
	if IsSafeUpstreamPathSegment(strings.TrimSpace(segment)) {
		return nil
	}
	// 不回显原始输入，避免把它写进日志与错误响应。
	return fmt.Errorf("invalid %s for upstream url path", kind)
}
