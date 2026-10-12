package httpapi

import (
	"context"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/TokenFlux/TokenRouter/internal/gateway/completion"
	"github.com/TokenFlux/TokenRouter/internal/infra/telemetry/logging"
)

// CompletionSubmission 在 HTTP 请求线程中捕获完成任务的数据。
type CompletionSubmission struct {
	pool    *completion.UsageRecordWorkerPool
	options completion.SubmissionOptions
}

// NewCompletionSubmission 为 Messages 与 OpenAI 入口配置日志来源。
func NewCompletionSubmission(pool *completion.UsageRecordWorkerPool, openAI bool) CompletionSubmission {
	prefix, component := "gateway", "handler.gateway.messages"
	if openAI {
		prefix, component = "openai", "handler.openai_gateway.responses"
	}
	return CompletionSubmission{pool: pool, options: completion.SubmissionOptions{
		FallbackWhenStopped: true,
		RecoverPanic:        true,
		Observe: func(event completion.SubmissionEvent) {
			name, source := prefix+".usage_record_task_stopped_sync_fallback", component
			if event.Mandatory {
				name, source = prefix+".usage_record_task_mandatory_sync_fallback", "handler.gateway.usage"
			}
			if event.Mandatory && openAI {
				source = "handler.openai_gateway.usage"
			}
			logger := logging.L().With(zap.String("component", source))
			if event.Panic != nil {
				logger.With(zap.Any("panic", event.Panic)).Error(prefix + ".usage_record_task_panic_recovered")
				return
			}
			logger.Warn(name)
		},
	}}
}

// NewQoderCompletionSubmission 保留池拒绝不内联、无池时脱离取消的原独立策略。
func NewQoderCompletionSubmission(pool *completion.UsageRecordWorkerPool) CompletionSubmission {
	return CompletionSubmission{pool: pool, options: completion.SubmissionOptions{PreserveSourceValues: true}}
}

// CompletionContext 捕获关联字段和模型映射链，供完成任务使用。
func CompletionContext(c *gin.Context) context.Context {
	return completion.SnapshotContext(completionSource(c))
}

func completionSource(c *gin.Context) context.Context {
	if c == nil || c.Request == nil {
		return context.Background()
	}
	return c.Request.Context()
}

func (s CompletionSubmission) Submit(c *gin.Context, task completion.UsageRecordTask) {
	completion.SubmitTask(s.pool, completionSource(c), task, false, s.options)
}

func (s CompletionSubmission) SubmitMandatory(c *gin.Context, task completion.UsageRecordTask) {
	completion.SubmitTask(s.pool, completionSource(c), task, true, s.options)
}

// SubmitImages 使用已确认的图片产出数量提交完成任务。
func (s CompletionSubmission) SubmitImages(c *gin.Context, images int, task completion.UsageRecordTask) {
	completion.SubmitTask(s.pool, completionSource(c), task, images > 0, s.options)
}
