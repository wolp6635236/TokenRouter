package scheduler

import "context"

// RPMCache RPM 计数器缓存接口
// 用于 Anthropic OAuth/SetupToken 提供商的每分钟请求数限制。
type RPMCache interface {
	// IncrementRPM 原子递增并返回当前分钟的计数
	// minute key 使用 Redis 服务器时间，各实例共用同一个分钟窗口。
	IncrementRPM(ctx context.Context, providerID int64) (count int, err error)

	// GetRPM 获取当前分钟的 RPM 计数
	GetRPM(ctx context.Context, providerID int64) (count int, err error)

	// GetRPMBatch 批量获取多个提供商的 RPM 计数（使用 Pipeline）
	GetRPMBatch(ctx context.Context, providerIDs []int64) (map[int64]int, error)
}
