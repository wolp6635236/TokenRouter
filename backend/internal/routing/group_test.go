package routing

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPricingConfigNestedSnapshotIsolation 检查嵌套 JSON 数组和对象的副本隔离。
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
