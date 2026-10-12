package httpapi

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSetOpsUpstreamModelStoresOnlyTrimmedModelSlug(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	SetOpsUpstreamModel(c, "  gpt-5.6-sol  ")
	value, ok := c.Get(OpsUpstreamModelKey)
	require.True(t, ok)
	require.Equal(t, "gpt-5.6-sol", value)
	SetOpsUpstreamModel(c, "  ")
	value, ok = c.Get(OpsUpstreamModelKey)
	require.True(t, ok)
	require.Equal(t, "gpt-5.6-sol", value)
	ClearOpsUpstreamModel(c)
	value, ok = c.Get(OpsUpstreamModelKey)
	require.True(t, ok)
	require.Equal(t, "", value)
}
