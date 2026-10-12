package creative

import (
	"errors"
	"fmt"
)

// CreativeUpstreamError 是执行器向上抛出的上游调用错误，携带可重试判定。
type CreativeUpstreamError struct {
	Code       string
	StatusCode int
	Message    string
	Retryable  bool
}

// CreativeImageResultError 标记结果读取失败，禁止 worker 重新请求生图。
func CreativeImageResultError(code, message string) *CreativeUpstreamError {
	return &CreativeUpstreamError{Code: code, Message: message, Retryable: false}
}

func (e *CreativeUpstreamError) Error() string {
	if e == nil {
		return "creative upstream error"
	}
	return fmt.Sprintf("creative upstream error status=%d: %s", e.StatusCode, e.Message)
}

// CreativeNonRetryableError 生成不可重试的执行错误（如平台不支持该操作）。
func CreativeNonRetryableError(format string, args ...any) *CreativeUpstreamError {
	return &CreativeUpstreamError{StatusCode: 0, Message: fmt.Sprintf(format, args...), Retryable: false}
}

// CreativeHTTPStatusError 把上游 HTTP 状态码映射为可重试性：网络层错误、429 与 5xx 可重试，其余 4xx 不可重试。
func CreativeHTTPStatusError(statusCode int, message string) *CreativeUpstreamError {
	retryable := statusCode == 0 || statusCode == 429 || statusCode >= 500
	// 上游 body 可能回显 prompt、内部 URL 或认证信息；对外只保留稳定的状态类消息。
	publicMessage := "creative platform request failed"
	switch {
	case statusCode == 0:
		publicMessage = "creative platform connection failed"
	case statusCode == 429:
		publicMessage = "creative platform rate limited"
	case statusCode >= 500:
		publicMessage = "creative platform unavailable"
	case statusCode >= 400:
		publicMessage = "creative platform rejected request"
	}
	return &CreativeUpstreamError{StatusCode: statusCode, Message: publicMessage, Retryable: retryable}
}

// IsRetryableCreativeError 判断错误是否值得 worker 有限重试。
func IsRetryableCreativeError(err error) bool {
	if upstreamErr, ok := errors.AsType[*CreativeUpstreamError](err); ok {
		return upstreamErr.Retryable
	}
	// 未知错误（网络层、序列化等）保守视为可重试，由最大次数兜底。
	return err != nil
}
