package routing

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPricingConfigNestedSnapshotIsolation 验证缓存副本中的任意 JSON 数组/对象修改都不能污染已发布快照。
func TestPricingConfigNestedSnapshotIsolation(t *testing.T) {
	source := &GroupRoutingPolicy{FeaturesConfig: map[string]any{"extension": []any{map[string]any{"enabled": true}, []any{"original"}}}}
	copied := source.Clone()
	values, ok := copied.FeaturesConfig["extension"].([]any)
	require.True(t, ok)
	object, ok := values[0].(map[string]any)
	require.True(t, ok)
	array, ok := values[1].([]any)
	require.True(t, ok)
	object["enabled"] = false
	array[0] = "changed"
	originalValues, ok := source.FeaturesConfig["extension"].([]any)
	require.True(t, ok)
	originalObject, ok := originalValues[0].(map[string]any)
	require.True(t, ok)
	originalArray, ok := originalValues[1].([]any)
	require.True(t, ok)
	require.Equal(t, true, originalObject["enabled"])
	require.Equal(t, "original", originalArray[0])
}

// TestPricingConfigPublicationOwnsSnapshot 验证发布快照后输入仍由存储调用者拥有，后续修改不得改变缓存值或分组关联。
func TestPricingConfigPublicationOwnsSnapshot(t *testing.T) {
	pricingConfigs := []PricingConfig{{ID: 1, Status: StatusActive, GroupIDs: []int64{9}}}

	cache := populatePricingConfigCache(pricingConfigs)
	pricingConfigs[0].Status = "disabled"

	require.Equal(t, StatusActive, cache.byID[1].Status)
	require.Equal(t, int64(1), cache.pricingConfigByGroupID[9].ID)
}
