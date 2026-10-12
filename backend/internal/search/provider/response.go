package provider

const (
	maxResponseSize   = 1 << 20 // 1 MB
	errorBodyTruncLen = 200
)

// truncateBody 截断错误消息中的响应内容。
func truncateBody(body []byte) string {
	if len(body) <= errorBodyTruncLen {
		return string(body)
	}
	return string(body[:errorBodyTruncLen]) + "...(truncated)"
}
