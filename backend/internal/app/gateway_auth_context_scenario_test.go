package app

// 本文件检查 apikey_middleware.go、qoder_gateway.go 与 usage.go 使用的 Key 和订阅上下文读取。

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	keyhttp "github.com/TokenFlux/TokenRouter/internal/apikey/httpapi"
	"github.com/TokenFlux/TokenRouter/internal/billing"
	gatewayhttp "github.com/TokenFlux/TokenRouter/internal/gateway/httpapi"
)

func TestAPIKeyAndSubscriptionFromContext(t *testing.T) {
	c := &gin.Context{}

	key := &apikey.APIKey{ID: 1}
	c.Set(string(keyhttp.ContextKeyAPIKey), key)
	gotKey, ok := keyhttp.GetAPIKeyFromContext(c)
	require.True(t, ok)
	require.Equal(t, int64(1), gotKey.ID)

	sub := &billing.UserSubscription{ID: 2}
	c.Set(string(gatewayhttp.ContextKeySubscription), sub)
	gotSub, ok := gatewayhttp.SubscriptionFromContext(c)
	require.True(t, ok)
	require.Equal(t, int64(2), gotSub.ID)
}
