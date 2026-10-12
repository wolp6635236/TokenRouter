package completion

import (
	"context"
	"strings"

	"github.com/TokenFlux/TokenRouter/internal/gateway/modeltrace"
	"github.com/TokenFlux/TokenRouter/internal/infra/telemetry"
)

// SnapshotContext 将请求标识、模型映射和请求记录器传给后台完成任务。
func SnapshotContext(source context.Context) context.Context {
	return copyCompletionContext(context.Background(), source)
}

func copyCompletionContext(destination, source context.Context) context.Context {
	if destination == nil {
		destination = context.Background()
	}
	if source == nil {
		return destination
	}
	for _, key := range []telemetry.ContextKey{telemetry.RequestID, telemetry.ClientModel} {
		if value := source.Value(key); value != nil {
			destination = context.WithValue(destination, key, value)
		}
	}
	if trace, ok := modeltrace.FromContext(source); ok {
		snapshot := trace.Snapshot()
		destination = modeltrace.WithContext(destination, snapshot)
		if strings.TrimSpace(snapshot.ClientModel) != "" {
			destination = context.WithValue(destination, telemetry.ClientModel, snapshot.ClientModel)
		}
	}
	return telemetry.CopyRequestCapture(destination, source)
}

// WrapTaskContext 在提交时冻结来源，执行时保留 worker 自己的预算。
func WrapTaskContext(source context.Context, task func(context.Context)) func(context.Context) {
	if task == nil {
		return nil
	}
	snapshot := SnapshotContext(source)
	return func(ctx context.Context) { task(copyCompletionContext(ctx, snapshot)) }
}
