package provider

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/TokenFlux/TokenRouter/internal/infra/telemetry/logging"
	"github.com/TokenFlux/TokenRouter/internal/provider"
)

// TransportHealth 复用提供商运行阻断与存储，处理已经分类的持久传输故障。
type TransportHealth struct {
	Runtime  *provider.RuntimeBlockState
	Deferred *provider.DeferredService
	Store    interface {
		SetTempUnschedulable(context.Context, int64, time.Time, string) error
	}
}

// Attempt 只记录实际进入网络传输的 Ollama Cloud 活动。
func (s *TransportHealth) Attempt(value *provider.Record) {
	if s == nil || s.Deferred == nil || value == nil || !provider.IsOllamaCloudUsageProvider(value) {
		return
	}
	s.Deferred.ScheduleLastUsedUpdate(value.ID)
}

// Persistent 先阻断进程内调度，再用原五秒独立预算保存十分钟暂停。
func (s *TransportHealth) Persistent(ctx context.Context, value *provider.Record, safeError string) {
	if s == nil || value == nil {
		return
	}
	until := time.Now().Add(10 * time.Minute)
	reason := "upstream transport error (proxy/network): " + safeError
	s.Runtime.BlockProviderScheduling(value, until, "transport_error")
	if s.Store == nil {
		logging.L().With(zap.String("component", "service.openai_gateway")).Warn("openai.provider_temp_unscheduled_transport_memory_only", zap.Int64("provider_id", value.ID), zap.String("provider_name", value.Name), zap.String("platform", value.Platform), zap.Time("until", until), zap.String("reason", reason))
		return
	}
	stateCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.Store.SetTempUnschedulable(stateCtx, value.ID, until, reason); err != nil {
		logging.L().With(zap.String("component", "service.openai_gateway")).Warn("openai.provider_temp_unscheduled_transport_failed", zap.Int64("provider_id", value.ID), zap.Error(err))
		return
	}
	logging.L().With(zap.String("component", "service.openai_gateway")).Warn("openai.provider_temp_unscheduled_transport", zap.Int64("provider_id", value.ID), zap.String("provider_name", value.Name), zap.String("platform", value.Platform), zap.Time("until", until), zap.String("reason", reason))
}
