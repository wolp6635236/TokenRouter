package logredact

import (
	"strings"
	"unicode/utf8"
)

// TruncateUTF8Value 去掉首尾空白后，按字节上限截断 UTF-8 文本。
func TruncateUTF8Value(value string, maxLen int) string {
	value = strings.TrimSpace(value)
	return TruncateUTF8(value, maxLen)
}

// TruncateUTF8 按字节上限截断，保留完整 UTF-8 字符及首尾空白。
func TruncateUTF8(value string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if len(value) <= maxLen {
		return value
	}
	value = value[:maxLen]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

// TruncateLine 按字节上限截断并转义换行，生成单行日志文本。
func TruncateLine(b []byte, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = 2048
	}
	if len(b) > maxBytes {
		b = b[:maxBytes]
	}
	s := string(b)
	// 转义换行符，使输出保持一行。
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	return s
}
