package ops

import "time"

// UserErrorRequest 是用户错误请求的脱敏视图，字段按白名单提供。
// message 是网关标准化描述，key_name 是用户在 KeysView 中可见的自有 API Key 名称。
// client_ip、user_agent、group_name、request_type 和 stream 是用户自己的请求属性，
// 根据 2026-07-03 的产品决定开放，与用量明细展示的字段一致。
// error_body 在详情接口 GetUserErrorRequestDetail 校验记录归属后返回。
// provider、api_key_prefix、upstream_endpoint 和 user_email 属于内部敏感字段，输出时需要移除。
type UserErrorRequest struct {
	RequestID       string    `json:"request_id"`
	ID              int64     `json:"id"`
	CreatedAt       time.Time `json:"created_at"`
	Model           string    `json:"model"`
	InboundEndpoint string    `json:"inbound_endpoint"`
	StatusCode      int       `json:"status_code"`
	Category        string    `json:"category"`
	Platform        string    `json:"platform"`
	Message         string    `json:"message"`
	KeyName         string    `json:"key_name"`
	KeyDeleted      bool      `json:"key_deleted"`
	ClientIP        string    `json:"client_ip,omitempty"`
	GroupName       string    `json:"group_name,omitempty"`
	RequestType     *int16    `json:"request_type,omitempty"`
	Stream          bool      `json:"stream"`
	UserAgent       string    `json:"user_agent,omitempty"`
}

// UserErrorRequestList 是用户错误请求分页结果。
type UserErrorRequestList struct {
	Items    []*UserErrorRequest `json:"items"`
	Total    int                 `json:"total"`
	Page     int                 `json:"page"`
	PageSize int                 `json:"page_size"`
}

// UserErrorRequestDetail 是点击请求列表单行后展示的脱敏详情。
// 它包含 UserErrorRequest 字段、上游错误正文 error_body 和 upstream_status_code。
type UserErrorRequestDetail struct {
	UserErrorRequest
	ErrorBody          string `json:"error_body"`
	UpstreamStatusCode *int   `json:"upstream_status_code,omitempty"`
}

// MapUserErrorCategory 把后端 error_phase + error_type 映射为用户侧粗分类码。
// 返回稳定的分类码，前端通过 i18n 显示文案。
func MapUserErrorCategory(phase, errType string) string {
	switch phase {
	case "auth":
		return "auth"
	case "routing":
		return "service_unavailable"
	case "provider_auth", "upstream", "network":
		return "upstream"
	case "internal":
		return "internal"
	case "request":
		switch errType {
		case "rate_limit_error":
			return "rate_limit"
		case "billing_error", "subscription_error":
			return "quota"
		case "invalid_request_error":
			return "invalid_request"
		case "cyber_policy", "cyber_policy_session_blocked":
			return "cyber"
		}
	}
	return "other"
}

// CategoryToFilter 将用户分类转换为 phase 和 type 的 ANY 过滤条件。
// other 和未知分类返回空切片，表示查询全部分类。
func CategoryToFilter(category string) (phases []string, errorTypes []string) {
	switch category {
	case "auth":
		return []string{"auth"}, nil
	case "service_unavailable":
		return []string{"routing"}, nil
	case "upstream":
		return []string{"provider_auth", "upstream", "network"}, nil
	case "internal":
		return []string{"internal"}, nil
	case "rate_limit":
		return nil, []string{"rate_limit_error"}
	case "quota":
		return nil, []string{"billing_error", "subscription_error"}
	case "invalid_request":
		return nil, []string{"invalid_request_error"}
	case "cyber":
		return []string{"request"}, []string{"cyber_policy", "cyber_policy_session_blocked"}
	default:
		return nil, nil
	}
}

// ToUserErrorRequest 把内部 OpsErrorLog 裁剪为用户安全视图。
func ToUserErrorRequest(e *OpsErrorLog) *UserErrorRequest {
	if e == nil {
		return nil
	}
	model := e.RequestedModel
	if model == "" {
		model = e.Model
	}
	clientIP := ""
	if e.ClientIP != nil {
		clientIP = *e.ClientIP
	}
	return &UserErrorRequest{
		ID:              e.ID,
		RequestID:       e.RequestID,
		CreatedAt:       e.CreatedAt,
		Model:           model,
		InboundEndpoint: e.InboundEndpoint,
		StatusCode:      e.StatusCode,
		Category:        MapUserErrorCategory(e.Phase, e.Type),
		Platform:        e.Platform,
		Message:         e.Message,
		KeyName:         e.APIKeyName,
		KeyDeleted:      e.APIKeyDeleted,
		ClientIP:        clientIP,
		GroupName:       e.GroupName,
		RequestType:     e.RequestType,
		Stream:          e.Stream,
		UserAgent:       e.UserAgent,
	}
}

// ToUserErrorRequestDetail 把内部 OpsErrorLogDetail 裁剪为用户安全详情视图。
func ToUserErrorRequestDetail(e *OpsErrorLogDetail) *UserErrorRequestDetail {
	if e == nil {
		return nil
	}
	base := ToUserErrorRequest(&e.OpsErrorLog)
	return &UserErrorRequestDetail{
		UserErrorRequest:   *base,
		ErrorBody:          e.ErrorBody,
		UpstreamStatusCode: e.UpstreamStatusCode,
	}
}
