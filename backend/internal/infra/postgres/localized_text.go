package postgres

import (
	"context"
	"slices"
	"strings"

	"github.com/lib/pq"

	"github.com/TokenFlux/TokenRouter/internal/pkg/locale"
)

// LocalizedTextExpression 为聚合查询生成与内容解析器相同的原文和有效译文选择。
// 列名和原文表达式由查询代码提供，语言和字段使用 SQL 字面值转义。
func LocalizedTextExpression(ctx context.Context, column, field, original string) string {
	if !locale.UserPresentation(ctx) {
		return original
	}
	parts := strings.Split(column, ".")
	for i := range parts {
		parts[i] = pq.QuoteIdentifier(parts[i])
	}
	content := strings.Join(parts, ".")
	source := content + "->>'source'"
	if field != "" {
		source = content + "->'source'->>" + pq.QuoteLiteral(field)
	}
	fallback := "COALESCE(" + source + ", " + original + ")"
	expression := fallback
	candidates := locale.Candidates(locale.FromContext(ctx))
	for _, candidate := range slices.Backward(candidates) {
		language := pq.QuoteLiteral(candidate)
		translation := content + "->'translations'->" + language
		value := translation + "->>'value'"
		if field != "" {
			value = translation + "->'value'->>" + pq.QuoteLiteral(field)
		}
		expression = "CASE WHEN " + content + "->>'source_locale'=" + language + " THEN " + fallback +
			" WHEN " + translation + "->>'source_revision'=" + content + "->>'source_revision' THEN COALESCE(" + value + ", " + fallback + ") ELSE " + expression + " END"
	}
	// 零版本内容尚未填写展示文案，使用业务字段。
	return "CASE WHEN COALESCE((" + content + "->>'revision')::bigint, 0) > 0 THEN " + expression + " ELSE " + original + " END"
}
