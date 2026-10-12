package identity

import (
	"context"
	"errors"
	"time"
)

// ErrRefreshTokenNotFound is returned when a refresh token is not found in cache.
// This is used to abstract away the underlying cache implementation (e.g., redis.Nil).
var ErrRefreshTokenNotFound = errors.New("refresh token not found")

// RefreshTokenData 存储在 Redis 中的 Refresh Token 数据。
type RefreshTokenData struct {
	UserID       int64     `json:"user_id"`
	TokenVersion int64     `json:"token_version"`          // 用于检测密码更改后的Token失效
	FamilyID     string    `json:"family_id"`              // Token家族ID，用于防重放攻击
	BindingHash  string    `json:"binding_hash,omitempty"` // 会话指纹哈希（IP+UA），会话绑定开启时校验
	CreatedAt    time.Time `json:"created_at"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// RefreshTokenCache 管理 Refresh Token 的 Redis 缓存
// 用于 JWT Token 刷新机制，支持 Token 轮转和防重放攻击
//
// Key 格式:
//   - refresh_token:{token_hash}     -> RefreshTokenData (JSON)
//   - user_refresh_tokens:{user_id}  -> Set<token_hash>
//   - token_family:{family_id}       -> Set<token_hash>
type RefreshTokenCache interface {
	// StoreRefreshToken 存储 Refresh Token
	// tokenHash: Token 的 SHA256 哈希值（不存储原始 Token）
	// data: Token 关联的数据
	// ttl: Token 过期时间
	StoreRefreshToken(ctx context.Context, tokenHash string, data *RefreshTokenData, ttl time.Duration) error

	// GetRefreshToken 获取 Refresh Token 数据
	// 返回 (data, nil) 如果 Token 存在
	// 返回 (nil, ErrRefreshTokenNotFound) 如果 Token 不存在
	// 返回 (nil, err) 如果发生其他错误
	GetRefreshToken(ctx context.Context, tokenHash string) (*RefreshTokenData, error)

	// ConsumeRefreshToken 在全部校验后原子删除旧凭据，只有实际删除者可以签发后继 token。
	// 不存在返回 false；存储失败必须返回错误，不能按消费成功处理。
	ConsumeRefreshToken(ctx context.Context, tokenHash string) (bool, error)

	// DeleteRefreshToken 删除单个 Refresh Token
	// 用于登出和过期清理；轮转必须使用有消费结果的 ConsumeRefreshToken。
	DeleteRefreshToken(ctx context.Context, tokenHash string) error

	// DeleteUserRefreshTokens 删除用户的所有 Refresh Token
	// 用于密码更改或用户主动登出所有设备
	DeleteUserRefreshTokens(ctx context.Context, userID int64) error

	// DeleteTokenFamily 删除整个 Token 家族
	// 用于检测到 Token 重放攻击时，撤销整个会话链
	DeleteTokenFamily(ctx context.Context, familyID string) error

	// AddToUserTokenSet 将 Token 添加到用户的 Token 集合
	// 用于跟踪用户的所有活跃 Refresh Token
	AddToUserTokenSet(ctx context.Context, userID int64, tokenHash string, ttl time.Duration) error

	// AddToFamilyTokenSet 将 Token 添加到家族 Token 集合
	// 用于跟踪同一登录会话的所有 Token
	AddToFamilyTokenSet(ctx context.Context, familyID string, tokenHash string, ttl time.Duration) error

	// GetUserTokenHashes 获取用户的所有 Token 哈希
	// 用于批量删除用户 Token
	GetUserTokenHashes(ctx context.Context, userID int64) ([]string, error)

	// GetFamilyTokenHashes 获取家族的所有 Token 哈希
	// 用于批量删除家族 Token
	GetFamilyTokenHashes(ctx context.Context, familyID string) ([]string, error)

	// IsTokenInFamily 检查 Token 是否属于指定家族
	// 用于验证 Token 家族关系
	IsTokenInFamily(ctx context.Context, familyID string, tokenHash string) (bool, error)
}
