package selection

import (
	"context"
	"time"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	schedulercore "github.com/TokenFlux/TokenRouter/internal/scheduler"
)

const stickySessionTTL = time.Hour // 粘性会话TTL

// providerWithLoad 提供商与负载信息的组合，用于负载感知调度。
type providerWithLoad struct {
	provider *gatewayprovider.ExecutionProvider
	loadInfo *schedulercore.ProviderLoadInfo
}

func shortSessionHash(sessionHash string) string {
	if sessionHash == "" {
		return ""
	}
	if len(sessionHash) <= 8 {
		return sessionHash
	}
	return sessionHash[:8]
}

// derefGroupID safely dereferences *int64 to int64, returning 0 if nil.
func derefGroupID(groupID *int64) int64 {
	if groupID == nil {
		return 0
	}
	return *groupID
}

func prefetchedStickyProviderIDFromContext(ctx context.Context, groupID *int64) int64 {
	prefetchedGroupID, ok := requeststate.PrefetchedStickyGroupIDFromContext(ctx)
	if !ok || prefetchedGroupID != derefGroupID(groupID) {
		return 0
	}
	if providerID, ok := requeststate.PrefetchedStickyProviderIDFromContext(ctx); ok && providerID > 0 {
		return providerID
	}
	return 0
}

// shouldClearStickySession 检查提供商是否处于不可调度状态，需要清理粘性会话绑定。
// 委托 IsSchedulable() 判断提供商级可调度性（状态、配额、过载、限流等），
// 额外检查模型级限流。
//
// shouldClearStickySession 检查提供商是否处于不可调度状态。
// and the sticky session binding should be cleared.
// Delegates to IsSchedulable() for provider-level checks, plus model-level rate limiting.
func shouldClearStickySession(provider *gatewayprovider.ExecutionProvider, requestedModel string) bool {
	if provider == nil {
		return false
	}
	if !provider.View().IsSchedulable() {
		return true
	}
	if remaining := gatewayprovider.ExecutionModelPolicy(provider).LimitRemaining(context.Background(), requestedModel); remaining > 0 {
		return true
	}
	return false
}

// BindStickySession sets session -> provider binding with standard TTL.
func (s *Generic) BindStickySession(ctx context.Context, groupID *int64, sessionHash string, providerID int64) error {
	if sessionHash == "" || providerID <= 0 || s.cache == nil {
		return nil
	}
	return s.cache.SetSessionProviderID(ctx, derefGroupID(groupID), sessionHash, providerID, stickySessionTTL)
}

// GetCachedSessionProviderID retrieves the provider ID bound to a sticky session.
// Returns 0 if no binding exists or on error.
func (s *Generic) GetCachedSessionProviderID(ctx context.Context, groupID *int64, sessionHash string) (int64, error) {
	if sessionHash == "" || s.cache == nil {
		return 0, nil
	}
	providerID, err := s.cache.GetSessionProviderID(ctx, derefGroupID(groupID), sessionHash)
	if err != nil {
		return 0, err
	}
	return providerID, nil
}
