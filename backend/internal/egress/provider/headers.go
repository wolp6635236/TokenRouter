package provider

import (
	"net/http"
	"strings"

	"github.com/TokenFlux/TokenRouter/internal/egress"
)

// ApplyRequestHeaders 覆写策略允许的请求头，并使用调用方指定的名称大小写。
func ApplyRequestHeaders(headers http.Header, policy egress.EgressPolicy, wireCasing func(string) string) {
	if headers == nil {
		return
	}
	for name, value := range policy.Headers {
		for existing := range headers {
			if strings.EqualFold(existing, name) {
				delete(headers, existing)
			}
		}
		headers[wireCasing(name)] = []string{value}
	}
}

// FilterHeaders 复制筛选规则允许的响应头。
func FilterHeaders(src http.Header, filter *egress.CompiledHeaderFilter) http.Header {
	filtered := make(http.Header, len(src))
	for key, values := range src {
		if !filter.Allows(key) {
			continue
		}
		for _, value := range values {
			filtered.Add(key, value)
		}
	}
	return filtered
}

// WriteFilteredHeaders 将筛选规则允许的响应头追加到目标。
func WriteFilteredHeaders(dst http.Header, src http.Header, filter *egress.CompiledHeaderFilter) {
	filtered := FilterHeaders(src, filter)
	for key, values := range filtered {
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}
