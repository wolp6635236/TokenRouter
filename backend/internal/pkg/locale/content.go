package locale

import (
	"encoding/json"
	"reflect"
	"slices"

	"github.com/TokenFlux/TokenRouter/internal/pkg/apperror"
)

// ErrConflict 表示提交的内容版本已被其他编辑覆盖。
var ErrConflict = apperror.Conflict("LOCALIZATION_CONFLICT", "Content changed. Reload before saving.")

// Translation 保存译文及其已经核对的原文版本。
type Translation[T any] struct {
	Value          T     `json:"value"`
	SourceRevision int64 `json:"source_revision"`
}

// Content 保存一份原文及各语言的完整译文。
// @project-doc docs/interfaces/user_localization.md#localized_content
type Content[T any] struct {
	SourceLocale   *string                   `json:"source_locale"`
	Source         T                         `json:"source"`
	Translations   map[string]Translation[T] `json:"translations"`
	Revision       int64                     `json:"revision"`
	SourceRevision int64                     `json:"source_revision"`
}

// Update 包含用户读取时的版本和本次已经核对的语言。
type Update[T any] struct {
	Content[T]
	ReviewedLocales []string `json:"reviewed_locales,omitempty"`
	DeletedLocales  []string `json:"deleted_locales,omitempty"`
}

// Resolution 记录内容实际使用的语言，未知原文语言保持为空。
type Resolution struct {
	Locale   *string `json:"locale"`
	Fallback bool    `json:"fallback"`
}

// Original 将尚未标注语言的历史内容包装为原文。
func Original[T any](value T) Content[T] {
	return Content[T]{Source: value, Translations: map[string]Translation[T]{}}
}

// Resolve 选择已核对的译文，缺失或过期时返回原文。
func (c Content[T]) Resolve(code string) (T, Resolution) {
	for _, candidate := range Candidates(code) {
		if c.SourceLocale != nil && Normalize(*c.SourceLocale) == candidate {
			return c.Source, Resolution{Locale: c.SourceLocale, Fallback: candidate != Normalize(code)}
		}
		if translated, ok := c.Translations[candidate]; ok && translated.SourceRevision == c.SourceRevision {
			return translated.Value, Resolution{Locale: &candidate, Fallback: candidate != Normalize(code)}
		}
	}
	return c.Source, Resolution{Locale: c.SourceLocale, Fallback: c.SourceLocale == nil || Normalize(*c.SourceLocale) != Normalize(code)}
}

// Prepare 校验完整内容并推进版本，持久化时仍需原子比较旧 Revision。
func Prepare[T any](current Content[T], input Update[T], validate func(T) error) (Content[T], error) {
	if current.Revision != input.Revision {
		return current, ErrConflict
	}
	next := input.Content
	if next.SourceLocale != nil {
		code := Normalize(*next.SourceLocale)
		if code == "" {
			return current, apperror.BadRequest("INVALID_LOCALE", "Language is not supported.")
		}
		next.SourceLocale = &code
	}
	changed := !reflect.DeepEqual(current.Source, next.Source) || !reflect.DeepEqual(current.SourceLocale, next.SourceLocale)
	if next.SourceLocale == nil {
		if changed || current.Revision == 0 {
			return current, apperror.BadRequest("SOURCE_LOCALE_REQUIRED", "Choose the original language.")
		}
	}
	if err := validate(next.Source); err != nil {
		return current, err
	}
	next.SourceRevision = current.SourceRevision
	if changed || next.SourceRevision == 0 {
		next.SourceRevision++
	}
	next.Translations = make(map[string]Translation[T], len(current.Translations)+len(input.Translations))
	deleted := map[string]bool{}
	for _, raw := range input.DeletedLocales {
		code := Normalize(raw)
		if code == "" {
			return current, apperror.BadRequest("INVALID_LOCALE", "Language is not supported.")
		}
		deleted[code] = true
	}
	// 省略译文表示保留，删除操作单独列出语言。
	for code, translation := range current.Translations {
		if !deleted[code] {
			next.Translations[code] = translation
		}
	}
	seen := map[string]bool{}
	reviewed := map[string]bool{}
	for _, code := range input.ReviewedLocales {
		code = Normalize(code)
		if code == "" {
			return current, apperror.BadRequest("INVALID_LOCALE", "Language is not supported.")
		}
		reviewed[code] = true
	}
	for raw, translation := range input.Translations {
		code := Normalize(raw)
		if code == "" || next.SourceLocale != nil && code == *next.SourceLocale {
			return current, apperror.BadRequest("INVALID_TRANSLATION_LOCALE", "Translation language must differ from the original.")
		}
		if seen[code] || deleted[code] {
			return current, apperror.BadRequest("DUPLICATE_LOCALE", "Translation language is duplicated.")
		}
		seen[code] = true
		if err := validate(translation.Value); err != nil {
			return current, err
		}
		previous, existed := current.Translations[code]
		translation.SourceRevision = previous.SourceRevision
		if !existed || !reflect.DeepEqual(previous.Value, translation.Value) {
			translation.SourceRevision = -1
		}
		if reviewed[code] {
			translation.SourceRevision = next.SourceRevision
		}
		next.Translations[code] = translation
	}
	for code := range next.Translations {
		if next.SourceLocale != nil && code == *next.SourceLocale {
			return current, apperror.BadRequest("INVALID_TRANSLATION_LOCALE", "Translation language must differ from the original.")
		}
	}
	next.Revision = current.Revision + 1
	// JSON 往返使源对象和嵌套数组脱离请求方持有的引用。
	data, err := json.Marshal(next)
	if err != nil {
		return current, err
	}
	var result Content[T]
	if err := json.Unmarshal(data, &result); err != nil {
		return current, err
	}
	return result, nil
}

// TextUpdates 是独立短文案的编辑集合。
type TextUpdates map[string]Update[string]

// TextContent 为 schema 生成器提供具名的字符串内容类型。
type TextContent Content[string]

// SearchTexts 收集原文和当前已核对译文，供用户搜索使用。
func SearchTexts[T any](content Content[T], fields func(T) []string) []string {
	seen := map[string]bool{}
	for _, value := range fields(content.Source) {
		if value != "" {
			seen[value] = true
		}
	}
	for _, translation := range content.Translations {
		if translation.SourceRevision != content.SourceRevision {
			continue
		}
		for _, value := range fields(translation.Value) {
			if value != "" {
				seen[value] = true
			}
		}
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	slices.Sort(result)
	return result
}
