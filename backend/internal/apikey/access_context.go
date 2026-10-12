package apikey

import (
	"context"
)

type accessSnapshotContextKey struct{}

// WithAccessSnapshot 由认证成功的入口调用，将访问快照保存到 context。
// 保存结构体副本，后续 Fast 策略覆盖不会修改认证结果或其它请求。
func WithAccessSnapshot(ctx context.Context, access AccessSnapshot) context.Context {
	access.TeamID = clonePointer(access.TeamID)
	return context.WithValue(ctx, accessSnapshotContextKey{}, access)
}

func AccessSnapshotFromContext(ctx context.Context) (AccessSnapshot, bool) {
	if ctx == nil {
		return AccessSnapshot{}, false
	}
	access, ok := ctx.Value(accessSnapshotContextKey{}).(AccessSnapshot)
	access.TeamID = clonePointer(access.TeamID)
	return access, ok
}

// WithFastModePolicy 在当前请求的访问快照副本中设置 Fast 策略。
func WithFastModePolicy(ctx context.Context, policy string) context.Context {
	access, _ := AccessSnapshotFromContext(ctx)
	access.fastModePolicy = policy
	return WithAccessSnapshot(ctx, access)
}

func (a AccessSnapshot) FastModePolicy() string { return a.fastModePolicy }

// WithRuntimeAPIKey 将已重新授权的分组快照发布给本请求，不改变原凭据身份或认证缓存。
func WithRuntimeAPIKey(ctx context.Context, key *APIKey) context.Context {
	access, ok := AccessSnapshotFromContext(ctx)
	if !ok || key == nil || access.KeyID != key.ID {
		return ctx
	}
	access.key = key
	if key.User != nil {
		access.PayerUserID = key.User.ID
	}
	return WithAccessSnapshot(ctx, access)
}
