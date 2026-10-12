package provider

import (
	"context"
	"time"
)

const (
	RefreshFailurePermanent RefreshFailureKind = iota
	RefreshFailureCooldown
)

// RefreshFailureVersion 记录交换失败时的身份，后续写入按身份和调度状态比较。
type RefreshFailureVersion struct {
	CredentialVersion
	Schedulable bool
}
type RefreshFailureKind uint8

type RefreshFailure struct {
	Kind    RefreshFailureKind
	Message string
	Until   time.Time
}

// RefreshFailureWriter 返回身份是否仍匹配；同身份已有更长 cooldown 时保持原值但仍算匹配。
// 未匹配不得触发内存阻断、成功失效或新的交换。
type RefreshFailureWriter interface {
	ApplyOAuthRefreshFailure(context.Context, RefreshFailureVersion, RefreshFailure) (bool, error)
}

func FailureVersion(value *Record) RefreshFailureVersion {
	if value == nil {
		return RefreshFailureVersion{}
	}
	credentials := CloneValues(value.Credentials)
	if credentials == nil {
		credentials = map[string]any{}
	}
	return RefreshFailureVersion{CredentialVersion: CredentialVersion{ID: value.ID, Platform: value.Platform, Type: value.Type, Status: value.Status, Credentials: credentials, ProxyID: clonePointer(value.ProxyID)}, Schedulable: value.Schedulable}
}
