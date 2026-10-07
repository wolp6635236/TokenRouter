package provider_test

import (
	"context"
	"reflect"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/provider"
)

// ClearRefreshCooldownIfUnchanged 原稳定行夹具保留清理次数断言，存在当前行时同时核对条件写入输入。
func (r *tokenRefreshProviderRepo) ClearRefreshCooldownIfUnchanged(ctx context.Context, v provider.RefreshCooldownVersion) (bool, error) {
	if current := r.providersByID[v.ID]; current != nil && !reflect.DeepEqual(provider.ObserveRefreshCooldown(gatewayprovider.ExecutionRecord(current)), v) {
		return false, nil
	}
	return true, r.ClearTempUnschedulable(ctx, v.ID)
}
