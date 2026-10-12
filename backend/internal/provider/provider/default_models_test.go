package provider

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultSparkShadowModelMapping(t *testing.T) {
	mapping := DefaultSparkShadowModels()

	require.Len(t, mapping, 1, "Spark 只公开完整原生型号")
	require.Equal(t, "gpt-5.3-codex-spark", mapping["gpt-5.3-codex-spark"])
	mapping["gpt-5.3-codex-spark"] = "changed"
	require.Equal(t, "gpt-5.3-codex-spark", DefaultSparkShadowModels()["gpt-5.3-codex-spark"])
}

func TestSparkModelVariants(t *testing.T) {
	got := sparkModelVariants()
	require.ElementsMatch(t, []string{
		"gpt-5.3-codex-spark",
	}, got)
}
