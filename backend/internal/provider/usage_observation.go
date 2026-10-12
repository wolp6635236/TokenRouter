package provider

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	ErrUsageObservationChanged       = errors.New("provider identity changed during usage query")
	ErrUsageObservationWriterMissing = errors.New("usage observation conditional writer is not configured")
)

// UsageObservationVersion 限定上游观测可以写入的提供商身份与原健康窗口，不进入公开输出。
type UsageObservationVersion struct {
	CredentialVersion
	ParentProviderID                               *int64
	QuotaDimension                                 string
	RateLimitedAt, RateLimitResetAt, OverloadUntil *time.Time
}

// UsageObservationWriter 为快照写入、设置限流和清除限流分别提交并发布通知。
type UsageExtraWriter interface {
	UpdateUsageExtraIfUnchanged(context.Context, UsageObservationVersion, map[string]any) (bool, error)
}
type UsageObservationWriter interface {
	UsageExtraWriter
	SetUsageRateLimitIfUnchanged(context.Context, UsageObservationVersion, time.Time) (bool, error)
	ClearUsageRateLimitIfUnchanged(context.Context, UsageObservationVersion) (bool, error)
}

func ObserveUsageVersion(value *Record) UsageObservationVersion {
	if value == nil {
		return UsageObservationVersion{}
	}
	return UsageObservationVersion{
		CredentialVersion: FailureVersion(value).CredentialVersion,
		ParentProviderID:  clonePointer(value.ParentProviderID), QuotaDimension: value.QuotaDimension,
		RateLimitedAt: clonePointer(value.RateLimitedAt), RateLimitResetAt: clonePointer(value.RateLimitResetAt), OverloadUntil: clonePointer(value.OverloadUntil),
	}
}

// UsageCacheIdentity 仅在进程内校验缓存来源，不改变 Redis 协议、提供商 key 或 TTL。
func UsageCacheIdentity(value *Record) string {
	if value == nil {
		return ""
	}
	parent := int64(0)
	if value.ParentProviderID != nil {
		parent = *value.ParentProviderID
	}
	return fmt.Sprintf("%s/%s/%d/%s", RefreshCredentialIdentity(value), value.Status, parent, value.QuotaDimension)
}
