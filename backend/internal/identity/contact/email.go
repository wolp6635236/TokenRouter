package contact

import (
	"encoding/json"
	"strings"
)

// Entry 保留权益展示中的通知邮箱值，不提供身份管理操作。
type Entry struct {
	Email    string `json:"email"`
	Disabled bool   `json:"disabled"`
	Verified bool   `json:"verified"`
}

// ParseNotifyEmails 将通知邮箱 JSON 解析为 Entry 列表。
// 旧字符串数组中的邮箱视为已验证且未禁用，新对象数组保留 disabled 和 verified 字段。
// 输入为空或无效时返回 nil。
func ParseNotifyEmails(raw string) []Entry {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return nil
	}

	// Try parsing as new format first (array of objects)
	var entries []Entry
	if err := json.Unmarshal([]byte(raw), &entries); err == nil && len(entries) > 0 {
		// Verify it's actually the new format by checking the first element
		// json.Unmarshal into []Entry succeeds even for ["string"]
		// because it tries to fit "string" into Entry and gets zero values.
		// We need to detect old format explicitly.
		if !isOldStringArrayFormat(raw) {
			return entries
		}
	}

	// Try parsing as old format (array of strings)
	var emails []string
	if err := json.Unmarshal([]byte(raw), &emails); err == nil {
		result := make([]Entry, 0, len(emails))
		for _, e := range emails {
			e = strings.TrimSpace(e)
			if e != "" {
				result = append(result, Entry{
					Email:    e,
					Disabled: false,
					Verified: false, // Old format emails default to unverified
				})
			}
		}
		return result
	}

	return nil
}

// isOldStringArrayFormat checks if the JSON is a string array like ["email1","email2"].
func isOldStringArrayFormat(raw string) bool {
	var arr []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &arr); err != nil || len(arr) == 0 {
		return false
	}
	// Check if first element starts with a quote (string) vs { (object)
	first := strings.TrimSpace(string(arr[0]))
	return first != "" && first[0] == '"'
}

// MarshalNotifyEmails 将通知邮箱 Entry 列表序列化为 JSON。
func MarshalNotifyEmails(entries []Entry) string {
	if len(entries) == 0 {
		return "[]"
	}
	data, err := json.Marshal(entries)
	if err != nil {
		return "[]"
	}
	return string(data)
}
