package selection

import (
	"context"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
)

// selectProviderForModelWithPlatform 选择单平台提供商（完全隔离）
func (s *Generic) selectProviderForModelWithPlatform(ctx context.Context, groupID *int64, sessionHash string, requestedModel string, excludedIDs map[int64]struct{}, platform string) (*gatewayprovider.ExecutionProvider, error) {
	core, scope := s.genericSelector()
	selected, err := core.SelectPlatform(ctx, groupID, sessionHash, requestedModel, excludedIDs, platform)
	return scope.oldProvider(selected), err
}

// selectProviderWithMixedScheduling 选择提供商（支持混合调度）
// 查询原生平台提供商 + 启用 mixed_scheduling 的 antigravity 提供商
func (s *Generic) selectProviderWithMixedScheduling(ctx context.Context, groupID *int64, sessionHash string, requestedModel string, excludedIDs map[int64]struct{}, nativePlatform string) (*gatewayprovider.ExecutionProvider, error) {
	core, scope := s.genericSelector()
	selected, err := core.SelectPlatform(ctx, groupID, sessionHash, requestedModel, excludedIDs, "")
	return scope.oldProvider(selected), err
}
