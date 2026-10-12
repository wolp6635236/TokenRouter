package media

import (
	"fmt"
	"time"
)

// ErrorResponse 是输出意图；状态和 envelope 由 HTTP Adapter 写出。
type ErrorResponse struct {
	Status        int
	Type, Message string
}
type GrokRetry struct {
	Retryable, PolicyRetryable bool
	Delay                      time.Duration
	Deadline                   time.Time
	Maximum                    int
}
type GrokFailurePorts struct {
	ContentRejection func() (bool, string)
	Generic          func() bool
	Failover         func() bool
	Retry            func() GrokRetry
	Observe          func(string, string)
	Rewrite          func() (ErrorResponse, bool)
	Write            func(ErrorResponse)
	NewFailover      func(GrokRetry, bool) error
}

// EmbeddingFailurePorts 按原顺序读取纯分类、应用提供商策略及写出响应。
type EmbeddingFailurePorts struct {
	InvalidRequest func() bool
	ApplyPolicy    func()
	Generic        func() bool
	Failover       func() bool
	RecordFailover func()
	NewFailover    func() error
	Forward        func()
	Write          func(ErrorResponse)
}

// AlphaFailurePorts 提供搜索端点的故障切换和健康状态处理函数。
type AlphaFailurePorts struct {
	Prepare             func()
	Failover            func() bool
	EndpointUnsupported func() bool
	ApplySideEffects    func() bool
	NewFailover         func(bool) error
}

// ImageFailurePorts 分别提供授权恢复、失败观测、提供商策略和错误转换函数。
type ImageFailurePorts struct {
	Recover     func() (bool, error)
	Failover    func() bool
	Observe     func()
	ApplyPolicy func() bool
	NewFailover func() error
	Handle      func() error
}

// ImageResponseFailurePorts 只在原生请求不能切换时应用客户端展示策略。
type ImageResponseFailurePorts struct {
	CyberMessage    func() (string, bool)
	Observe         func(string, string)
	Write           func(ErrorResponse) error
	WrapCyber       func(error) error
	ApplyPolicy     func()
	Generic         func() bool
	Failover        func() bool
	NewFailover     func() error
	Rewrite         func() (ErrorResponse, bool)
	DefaultResponse func() error
}

// ResolveGrokFailure 依次检查内容拒绝、提供商策略和切换条件，再改写最终错误。
// Rewrite 在确定结束提供商切换后应用错误展示规则，健康处理由调用方完成。
func ResolveGrokFailure(status int, message string, p GrokFailurePorts) error {
	if rejected, client := p.ContentRejection(); rejected {
		p.Observe("http_error", client)
		p.Write(ErrorResponse{Status: 403, Type: "invalid_request_error", Message: client})
		return fmt.Errorf("grok content policy rejection: %s", client)
	}
	if p.Generic() {
		p.Observe("http_error", message)
		p.Write(ErrorResponse{Status: 500, Type: "upstream_error", Message: "Upstream gateway error"})
		return fmt.Errorf("upstream error: %d (not in custom error codes) message=%s", status, message)
	}
	if p.Failover() {
		p.Observe("failover", message)
		retry := p.Retry()
		return p.NewFailover(retry, retry.Retryable && status == 429)
	}
	p.Observe("http_error", message)
	if response, matched := p.Rewrite(); matched {
		p.Write(response)
		return fmt.Errorf("upstream error: %d (passthrough rule matched) message=%s", status, message)
	}
	typ := "upstream_error"
	switch status {
	case 400:
		typ = "invalid_request_error"
	case 404:
		typ = "not_found_error"
	case 429:
		typ = "rate_limit_error"
	}
	p.Write(ErrorResponse{Status: status, Type: typ, Message: message})
	return fmt.Errorf("upstream error: %d %s", status, message)
}

func ResolveEmbeddingFailure(status int, p EmbeddingFailurePorts) error {
	if p.InvalidRequest() {
		p.Forward()
		return fmt.Errorf("upstream invalid request: %d", status)
	}
	p.ApplyPolicy()
	if p.Generic() {
		p.Write(ErrorResponse{Status: 500, Type: "upstream_error", Message: "Upstream gateway error"})
		return fmt.Errorf("upstream error: %d (not in custom error codes)", status)
	}
	if p.Failover() {
		p.RecordFailover()
		return p.NewFailover()
	}
	p.Forward()
	return fmt.Errorf("upstream returned status %d", status)
}

func ResolveAlphaFailure(status int, p AlphaFailurePorts) error {
	if !p.Failover() && !p.EndpointUnsupported() {
		return nil
	}
	if p.Prepare != nil {
		p.Prepare()
	}
	disabled := false
	if AlphaProviderErrorSideEffects(status) {
		disabled = p.ApplySideEffects()
	}
	return p.NewFailover(disabled)
}

func ResolveImageFailure(p ImageFailurePorts) (bool, error) {
	if p.Recover != nil {
		matched, err := p.Recover()
		if matched {
			if err != nil {
				return false, fmt.Errorf("agent identity task recovery failed: %w", err)
			}
			return true, nil
		}
	}
	if p.Failover() {
		p.Observe()
		if p.ApplyPolicy() {
			return false, p.Handle()
		}
		return false, p.NewFailover()
	}
	return false, p.Handle()
}

func ResolveImageResponseFailure(status int, message string, p ImageResponseFailurePorts) error {
	if client, matched := p.CyberMessage(); matched {
		p.Observe("http_error", client)
		_ = p.Write(ErrorResponse{Status: status, Type: "invalid_request_error", Message: client})
		return p.WrapCyber(fmt.Errorf("upstream error: %d message=%s", status, client))
	}
	p.ApplyPolicy()
	if p.Generic() {
		p.Observe("http_error", message)
		return p.Write(ErrorResponse{Status: 500, Type: "upstream_error", Message: "Upstream gateway error"})
	}
	if p.Failover() {
		p.Observe("failover", message)
		return p.NewFailover()
	}
	p.Observe("http_error", message)
	if response, matched := p.Rewrite(); matched {
		return p.Write(response)
	}
	return p.DefaultResponse()
}
