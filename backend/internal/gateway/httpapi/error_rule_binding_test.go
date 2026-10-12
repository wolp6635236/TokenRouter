package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/TokenFlux/TokenRouter/internal/gateway/errorpolicy"
	gatewaytestkit "github.com/TokenFlux/TokenRouter/internal/gateway/testkit"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

func TestApplyErrorPassthroughRule_NoBoundService(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	status, errType, errMsg, matched := ApplyErrorPassthroughRule(
		c,
		capability.PlatformAnthropic,
		http.StatusUnprocessableEntity,
		[]byte(`{"error":{"message":"invalid schema"}}`),
		http.StatusBadGateway,
		"upstream_error",
		"Upstream request failed",
	)

	assert.False(t, matched)
	assert.Equal(t, http.StatusBadGateway, status)
	assert.Equal(t, "upstream_error", errType)
	assert.Equal(t, "Upstream request failed", errMsg)
}

func TestApplyErrorPassthroughRule_SkipMonitoringSetsContextKey(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	rule := gatewaytestkit.NonFailoverRule(http.StatusBadRequest, "prompt is too long", http.StatusBadRequest, "上下文超限")
	rule.SkipMonitoring = true

	ruleSvc := gatewaytestkit.ErrorRules([]*errorpolicy.ErrorPassthroughRule{rule})
	BindErrorPassthroughService(c, ruleSvc)

	_, _, _, matched := ApplyErrorPassthroughRule(
		c,
		capability.PlatformAnthropic,
		http.StatusBadRequest,
		[]byte(`{"error":{"message":"prompt is too long"}}`),
		http.StatusBadGateway,
		"upstream_error",
		"Upstream request failed",
	)

	assert.True(t, matched)
	v, exists := c.Get(OpsSkipPassthroughKey)
	assert.True(t, exists, "OpsSkipPassthroughKey should be set when skip_monitoring=true")
	boolVal, ok := v.(bool)
	assert.True(t, ok, "value should be bool")
	assert.True(t, boolVal)
}

func TestApplyErrorPassthroughRule_NoSkipMonitoringDoesNotSetContextKey(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	rule := gatewaytestkit.NonFailoverRule(http.StatusBadRequest, "prompt is too long", http.StatusBadRequest, "上下文超限")
	rule.SkipMonitoring = false

	ruleSvc := gatewaytestkit.ErrorRules([]*errorpolicy.ErrorPassthroughRule{rule})
	BindErrorPassthroughService(c, ruleSvc)

	_, _, _, matched := ApplyErrorPassthroughRule(
		c,
		capability.PlatformAnthropic,
		http.StatusBadRequest,
		[]byte(`{"error":{"message":"prompt is too long"}}`),
		http.StatusBadGateway,
		"upstream_error",
		"Upstream request failed",
	)

	assert.True(t, matched)
	_, exists := c.Get(OpsSkipPassthroughKey)
	assert.False(t, exists, "OpsSkipPassthroughKey should NOT be set when skip_monitoring=false")
}
