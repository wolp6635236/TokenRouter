package logredact

import (
	"regexp"
	"strings"
)

// upstreamQueryPattern 匹配查询参数中的密钥和令牌值。
var upstreamQueryPattern = regexp.MustCompile(`(?i)([?&](?:key|client_secret|access_token|refresh_token)=)[^&"\s]+`)

// SafeUpstreamURL 去掉首尾空白、查询参数和片段，返回用于观测记录的 URL。
func SafeUpstreamURL(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	if idx := strings.IndexByte(rawURL, '?'); idx >= 0 {
		rawURL = rawURL[:idx]
	}
	if idx := strings.IndexByte(rawURL, '#'); idx >= 0 {
		rawURL = rawURL[:idx]
	}
	return rawURL
}

// SanitizeUpstreamQueries 将消息内查询参数中的密钥和令牌值替换为 ***。
func SanitizeUpstreamQueries(msg string) string {
	if msg == "" {
		return msg
	}
	return upstreamQueryPattern.ReplaceAllString(msg, `$1***`)
}
