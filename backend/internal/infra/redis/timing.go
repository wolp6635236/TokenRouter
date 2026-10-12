package redis

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"

	servertiming "github.com/TokenFlux/TokenRouter/internal/infra/telemetry/timing"
)

// TimingHook 为启用 Server-Timing 的请求记录 Redis 命令耗时。
type TimingHook struct{}

// NewClient 按给定选项创建 Redis 客户端，启用计时时添加 TimingHook。
func NewClient(options *redis.Options, enableTiming bool) *redis.Client {
	client := redis.NewClient(options)
	if enableTiming {
		client.AddHook(TimingHook{})
	}
	return client
}

// DialHook 返回客户端的连接钩子。
func (TimingHook) DialHook(next redis.DialHook) redis.DialHook {
	return next
}

// ProcessHook 记录单条命令的耗时和数量。
func (TimingHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if !servertiming.Active(ctx) {
			return next(ctx, cmd)
		}
		startedAt := time.Now()
		err := next(ctx, cmd)
		servertiming.Record(ctx, servertiming.MetricRedis, startedAt, time.Now(), 1)
		return err
	}
}

// ProcessPipelineHook 记录整个流水线的耗时和命令数量。
func (TimingHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		if !servertiming.Active(ctx) {
			return next(ctx, cmds)
		}
		startedAt := time.Now()
		err := next(ctx, cmds)
		servertiming.Record(ctx, servertiming.MetricRedis, startedAt, time.Now(), len(cmds))
		return err
	}
}
