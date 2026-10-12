package provider

import (
	"strings"

	"github.com/TokenFlux/TokenRouter/internal/egress"
	infraerrors "github.com/TokenFlux/TokenRouter/internal/pkg/apperror"
)

const (
	maxUpstreamRequestIDHeaderNameLen = 64

	// ProviderExtraUpstreamRequestIDHeader 是提供商 extra 中的键，值为直接上游声明请求标识的响应头名。
	// 未指定时不记录上游请求标识。
	ProviderExtraUpstreamRequestIDHeader = "upstream_request_id_header"
)

// UpstreamRequestIDHeaderName 返回提供商指定的上游请求标识头名，未指定时为空串。
func UpstreamRequestIDHeaderName(provider *Record) string {
	if provider == nil {
		return ""
	}
	return strings.TrimSpace(provider.GetExtraString(ProviderExtraUpstreamRequestIDHeader))
}

// ValidateUpstreamRequestIDHeaderExtra 校验并规范化 extra 中的上游请求标识头名：
// 必须是合法的 HTTP 头字段名且不超过 64 字节；空白值视为未指定并从 extra 中移除。
func ValidateUpstreamRequestIDHeaderExtra(extra map[string]any) error {
	if extra == nil {
		return nil
	}
	raw, ok := extra[ProviderExtraUpstreamRequestIDHeader]
	if !ok || raw == nil {
		return nil
	}
	name, ok := raw.(string)
	if !ok {
		return infraerrors.BadRequest("INVALID_UPSTREAM_REQUEST_ID_HEADER",
			"upstream_request_id_header must be a string")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		delete(extra, ProviderExtraUpstreamRequestIDHeader)
		return nil
	}
	if len(name) > maxUpstreamRequestIDHeaderNameLen || !egress.ValidHeaderName(name) {
		return infraerrors.BadRequest("INVALID_UPSTREAM_REQUEST_ID_HEADER",
			"upstream_request_id_header must be a valid HTTP header name of at most 64 bytes")
	}
	extra[ProviderExtraUpstreamRequestIDHeader] = name
	return nil
}
