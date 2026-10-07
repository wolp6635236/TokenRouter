package locale

import (
	"context"
	"encoding/json"
)

// TextSettingKey 返回独立于语言数量的译文存储键。
func TextSettingKey(key string) string { return key + "_localized" }

// ResolveSettingText 从同一次查询的设置中选择译文，未配置时使用现有原文。
func ResolveSettingText(values map[string]string, key, fallback, language string) string {
	if raw := values[TextSettingKey(key)]; raw != "" {
		var content Content[string]
		if json.Unmarshal([]byte(raw), &content) == nil {
			value, _ := content.Resolve(language)
			return value
		}
	}
	if value := values[key]; value != "" {
		return value
	}
	return fallback
}

// ReadSettingText 为按键读取的后台通知选择对应语言。
func ReadSettingText(ctx context.Context, store interface {
	GetValue(context.Context, string) (string, error)
}, key, fallback string,
) string {
	original, _ := store.GetValue(ctx, key)
	translated, _ := store.GetValue(ctx, TextSettingKey(key))
	return ResolveSettingText(map[string]string{key: original, TextSettingKey(key): translated}, key, fallback, FromContext(ctx))
}

// ReadGroupedText 从一组独立文案中读取后台通知所需的站点名称。
func ReadGroupedText(ctx context.Context, store interface {
	GetValue(context.Context, string) (string, error)
}, group, key, fallback string,
) string {
	values := map[string]Content[string]{}
	raw, _ := store.GetValue(ctx, group)
	if json.Unmarshal([]byte(raw), &values) == nil {
		if content, exists := values[key]; exists {
			value, _ := content.Resolve(FromContext(ctx))
			return value
		}
	}
	if original, err := store.GetValue(ctx, key); err == nil && original != "" {
		return original
	}
	return fallback
}
