package locale

import (
	"context"
	_ "embed"
	"encoding/json"
	"strings"

	"golang.org/x/text/language"
)

// manifestJSON 是前后端共用的语言目录。
//
//go:embed manifest.json
var manifestJSON []byte

// Definition 包含界面名称、文字方向和兼容语言代码。
type Definition struct {
	Code      string   `json:"code"`
	Name      string   `json:"name"`
	Direction string   `json:"direction"`
	Aliases   []string `json:"aliases"`
	Fallbacks []string `json:"fallbacks"`
	Catalog   string   `json:"catalog"`
}

// Catalog 是随产品发布的语言目录。
type Catalog struct {
	Default string       `json:"default"`
	Locales []Definition `json:"locales"`
}

var catalog = func() Catalog {
	var result Catalog
	if err := json.Unmarshal(manifestJSON, &result); err != nil {
		panic(err)
	}
	return result
}()

type contextKey struct{}

// Default 返回站点未配置语言时使用的语言。
func Default() string { return catalog.Default }

// Definitions 返回目录副本，调用方修改切片不会影响后续请求。
func Definitions() []Definition {
	var result Catalog
	_ = json.Unmarshal(manifestJSON, &result)
	return result.Locales
}

// Normalize 将已支持的别名转换为规范代码，未知语言返回空字符串。
func Normalize(raw string) string {
	raw = strings.ReplaceAll(strings.TrimSpace(raw), "_", "-")
	if len(raw) > 35 || strings.ContainsAny(raw, ",; \t\r\n") {
		return ""
	}
	for _, segment := range strings.Split(raw, "-") {
		if segment == "" {
			return ""
		}
		for _, char := range segment {
			valid := char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9'
			if !valid {
				return ""
			}
		}
	}
	for _, item := range catalog.Locales {
		if strings.EqualFold(raw, item.Code) || strings.HasPrefix(strings.ToLower(raw), strings.ToLower(item.Code)+"-") {
			return item.Code
		}
		for _, alias := range item.Aliases {
			if strings.EqualFold(raw, alias) {
				return item.Code
			}
		}
	}
	return ""
}

// Negotiate 按请求权重选择支持的语言；不支持的语言使用站点默认值。
func Negotiate(header, fallback string) string {
	if direct := Normalize(header); direct != "" {
		return direct
	}
	tags, _, err := language.ParseAcceptLanguage(header)
	if err == nil {
		for _, tag := range tags {
			if value := Normalize(tag.String()); value != "" {
				return value
			}
		}
	}
	if value := Normalize(fallback); value != "" {
		return value
	}
	return Default()
}

// Candidates 返回内容匹配顺序，原文由内容解析器最后选择。
func Candidates(code string) []string {
	code = Normalize(code)
	for _, item := range catalog.Locales {
		if code == item.Code {
			return append([]string{code}, item.Fallbacks...)
		}
	}
	return nil
}

// WithLanguage 将已解析的语言附在请求上下文中。
func WithLanguage(ctx context.Context, code string) context.Context {
	return context.WithValue(ctx, contextKey{}, Negotiate(code, Default()))
}

// FromContext 返回请求语言，后台任务未指定时使用默认值。
func FromContext(ctx context.Context) string {
	if ctx == nil {
		return Default()
	}
	if value, ok := ctx.Value(contextKey{}).(string); ok {
		return value
	}
	return Default()
}

// Direction 返回语言的文字方向。
func Direction(code string) string {
	for _, item := range catalog.Locales {
		if item.Code == Normalize(code) {
			return item.Direction
		}
	}
	return "ltr"
}

// Explicit 返回调用方是否已指定语言，供正文引用的图片选择使用。
func Explicit(ctx context.Context) (string, bool) {
	value, ok := ctx.Value(contextKey{}).(string)
	return value, ok && value != ""
}

// WithoutLanguage 让原文的图片使用公共目录。
func WithoutLanguage(ctx context.Context) context.Context {
	return context.WithValue(ctx, contextKey{}, "")
}

type presentationKey struct{}

// WithUserPresentation 标记普通用户展示查询，内部管理查询继续读取业务名称。
func WithUserPresentation(ctx context.Context, enabled bool) context.Context {
	return context.WithValue(ctx, presentationKey{}, enabled)
}

// UserPresentation 返回当前查询是否面向普通用户。
func UserPresentation(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	value, _ := ctx.Value(presentationKey{}).(bool)
	return value
}
