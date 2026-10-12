package text

import (
	"context"
	"errors"

	"github.com/TokenFlux/TokenRouter/internal/gateway/execution"
	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	"github.com/TokenFlux/TokenRouter/internal/upstream"
)

// ErrExecutionRejected 表示前置之后的同步操作已拒绝请求，具体 HTTP 错误已由输出适配器写出。
var ErrExecutionRejected = errors.New("text execution rejected by request operation")

// MessageRuntime 在构造时绑定执行依赖，Open 创建本次请求状态。
// 适配会话实现 MessagePorts，RunMessages 执行提供商尝试循环。
type MessageRuntime interface {
	Open(context.Context, execution.Request, upstream.OutputSink) (MessagePorts, error)
}
type MessagesExecutor struct {
	runtime          MessageRuntime
	messages, gemini MessageOptions
}

type messageExecutionObservation struct {
	selected Selection
	MessagePorts
	result execution.ExecutionResult
	err    error
}

func NewMessagesExecutor(runtime MessageRuntime, messages, gemini MessageOptions) *MessagesExecutor {
	return &MessagesExecutor{runtime: runtime, messages: messages, gemini: gemini}
}

// Execute 执行本次文本请求并记录结果观测，供 HTTP 入口调用。
func (e *MessagesExecutor) Execute(ctx context.Context, in execution.Request, sink upstream.OutputSink) (execution.ExecutionResult, error) {
	ctx = requeststate.WithExecutionHints(ctx, in.Hints)
	ctx = requeststate.WithRoutingState(ctx, in.Routing)
	session, err := e.runtime.Open(ctx, in, sink)
	if err != nil {
		return execution.ExecutionResult{}, err
	}
	options := e.messages
	if in.Text.Kind == execution.TextGeminiMessages || in.Text.AlternateBudget {
		options = e.gemini
	}
	options.HasBoundSession = in.Text.HasBoundSession
	observed := &messageExecutionObservation{MessagePorts: session}
	RunMessages(options, observed)
	return observed.result, observed.err
}

func (o *messageExecutionObservation) PrepareAttempt() bool {
	ok := o.MessagePorts.PrepareAttempt()
	if !ok {
		o.err = ErrExecutionRejected
	}
	return ok
}

func (o *messageExecutionObservation) Select(excluded map[int64]struct{}) (Selection, error) {
	s, err := o.MessagePorts.Select(excluded)
	if err != nil {
		o.err = err
	} else {
		o.selected = s
		o.err = nil
	}
	return s, err
}

func (o *messageExecutionObservation) Acquire() bool {
	ok := o.MessagePorts.Acquire()
	if !ok {
		o.err = ErrExecutionRejected
	}
	return ok
}

func (o *messageExecutionObservation) Forward(state AttemptState) Outcome {
	out := o.MessagePorts.Forward(state)
	o.result.Provider = o.selected.Provider
	o.result.Plan = o.selected.Plan
	o.result.PlanProvided = o.selected.PlanProvided
	o.result.Attempts++
	o.result.Attempt = out.Attempt
	o.err = out.Err
	if out.Stop && o.err == nil {
		o.err = ErrExecutionRejected
	}
	return out
}

func (o *messageExecutionObservation) Canceled() {
	o.err = o.Context().Err()
	o.MessagePorts.Canceled()
}
