package provider

import (
	"context"
	"time"
)

const (
	googleConfigErrorCooldown = time.Minute
	emptyResponseCooldown     = time.Minute
)

// TemporaryFailureStore 独立写入临时停调字段。
type TemporaryFailureStore interface {
	SetTempUnschedulable(context.Context, int64, time.Time, string) error
}

// TempUnscheduleGoogleConfigError 对服务端配置类 400 错误触发临时封禁，
// 停调期间，后续请求会跳过该提供商。
func TempUnscheduleGoogleConfigError(ctx context.Context, repo TemporaryFailureStore, providerID int64, logPrefix string, logf func(string, ...any)) {
	until := time.Now().Add(googleConfigErrorCooldown)
	reason := "400: invalid project resource name (auto temp-unschedule 1m)"
	if err := repo.SetTempUnschedulable(ctx, providerID, until, reason); err != nil {
		logf("%s temp_unschedule_failed provider=%d error=%v", logPrefix, providerID, err)
	} else {
		logf("%s temp_unscheduled provider=%d until=%v reason=%q", logPrefix, providerID, until.Format("15:04:05"), reason)
	}
}

// TempUnscheduleEmptyResponse 对空流式响应触发临时封禁，
// 停调期间，后续请求会跳过这个返回空响应的提供商。
func TempUnscheduleEmptyResponse(ctx context.Context, repo TemporaryFailureStore, providerID int64, logPrefix string, logf func(string, ...any)) {
	until := time.Now().Add(emptyResponseCooldown)
	reason := "empty stream response (auto temp-unschedule 1m)"
	if err := repo.SetTempUnschedulable(ctx, providerID, until, reason); err != nil {
		logf("%s temp_unschedule_failed provider=%d error=%v", logPrefix, providerID, err)
	} else {
		logf("%s temp_unscheduled provider=%d until=%v reason=%q", logPrefix, providerID, until.Format("15:04:05"), reason)
	}
}
