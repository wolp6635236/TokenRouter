package locale

import (
	_ "embed"
	"encoding/json"
	"strconv"
	"strings"
	"unicode"
)

var (
	// errorMessagesJSON 保存前后端共用的用户错误提示。
	//
	//go:embed error_messages.json
	errorMessagesJSON []byte

	// errorMessages 按语言代码和错误原因保存提示文字。
	errorMessages = loadErrorMessages()
)

// loadErrorMessages 解析内嵌的错误提示，文件格式错误时 panic。
func loadErrorMessages() map[string]map[string]string {
	var messages map[string]map[string]string
	if err := json.Unmarshal(errorMessagesJSON, &messages); err != nil {
		panic(err)
	}
	return messages
}

// ErrorText 根据业务原因生成用户提示，英文原文和第三方内容由调用方选择是否传入。
func ErrorText(language, reason string, status int, original string) string {
	code := Negotiate(language, Default())
	hasChinese := strings.ContainsFunc(original, func(char rune) bool { return unicode.Is(unicode.Han, char) })
	if code == "en" && strings.TrimSpace(original) != "" && !hasChinese {
		return original
	}
	messages := errorMessages[code]
	if value := messages[strings.ToUpper(reason)]; value != "" {
		return value
	}
	if value := messages["HTTP_"+strconv.Itoa(status)]; value != "" {
		return value
	}
	if value := messages["REQUEST_FAILED"]; value != "" {
		return value
	}
	if original != "" {
		return original
	}
	return errorMessages["en"]["REQUEST_FAILED"]
}
