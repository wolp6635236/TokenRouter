package forward

import (
	"errors"
)

// ErrCyberPolicyForwarded 表示拒绝响应已经输出，调用方应进入错误收尾。
var ErrCyberPolicyForwarded = errors.New("openai cyber_policy forwarded to client")

// UpstreamWarning 保存上游风控警告，完成器决定是否结算，HTTP 层决定响应方式。
type UpstreamWarning struct {
	StatusCode   int
	ResponseBody []byte
	Message      string
}

// UpstreamWarningCarrier 从错误链提供上游警告。
type UpstreamWarningCarrier interface {
	OpenAIUpstreamWarning() *UpstreamWarning
}

// WarningFromError 从转发错误链中提取上游 cyber 风控警告。
func WarningFromError(err error) (*UpstreamWarning, bool) {
	if err == nil {
		return nil, false
	}
	var carrier UpstreamWarningCarrier
	if !errors.As(err, &carrier) || carrier == nil {
		return nil, false
	}
	warning := carrier.OpenAIUpstreamWarning()
	if warning == nil {
		return nil, false
	}
	return cloneWarning(warning), true
}

func cloneWarning(warning *UpstreamWarning) *UpstreamWarning {
	if warning == nil {
		return nil
	}
	cloned := *warning
	if warning.ResponseBody != nil {
		cloned.ResponseBody = append([]byte(nil), warning.ResponseBody...)
	}
	return &cloned
}
