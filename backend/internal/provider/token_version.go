package provider

import (
	"context"
)

// CheckTokenVersion 检查 provider 的 token 版本是否已过时，并返回最新的 provider
// 用于解决异步刷新任务与请求线程的竞态条件：
// 如果刷新任务已更新 token 并删除缓存，此时请求线程的旧 provider 对象不应写入缓存
//
// 返回值:
//   - latestProvider: 从 DB 获取的最新 provider（如果查询失败则返回 nil）
//   - isStale: true 表示 token 已过时（应使用 latestProvider），false 表示可以使用当前 provider
func CheckTokenVersion(ctx context.Context, provider *Record, repo RefreshRepository, observers ...func(string, ...any)) (latestProvider *Record, isStale bool) {
	debug := func(string, ...any) {}
	if len(observers) > 0 && observers[0] != nil {
		debug = observers[0]
	}

	if provider == nil || repo == nil {
		return nil, false
	}

	currentVersion := provider.GetCredentialAsInt64("_token_version")

	latestProvider, err := repo.GetByID(ctx, provider.ID)
	if err != nil || latestProvider == nil {
		// 查询失败，默认允许缓存，不返回 latestProvider
		return nil, false
	}

	latestVersion := latestProvider.GetCredentialAsInt64("_token_version")

	// 情况 1: 当前 provider 没有版本号，但 DB 中已有版本号
	// 说明异步刷新任务已更新 token，当前 provider 已过时
	if currentVersion == 0 && latestVersion > 0 {
		debug("token_version_stale_no_current_version",
			"provider_id", provider.ID,
			"latest_version", latestVersion)
		return latestProvider, true
	}

	// 情况 2: 两边都没有版本号，说明从未被异步刷新过，允许缓存
	if currentVersion == 0 && latestVersion == 0 {
		return latestProvider, false
	}

	// 情况 3: 比较版本号，如果 DB 中的版本更新，当前 provider 已过时
	if latestVersion > currentVersion {
		debug("token_version_stale",
			"provider_id", provider.ID,
			"current_version", currentVersion,
			"latest_version", latestVersion)
		return latestProvider, true
	}

	return latestProvider, false
}
