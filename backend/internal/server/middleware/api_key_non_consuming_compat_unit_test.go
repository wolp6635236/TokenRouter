package middleware

import (
	gatewayhttp "github.com/TokenFlux/TokenRouter/internal/gateway/httpapi"
)

func isAPIKeyNonConsumingRequest(method, path string) bool {
	return gatewayhttp.IsAPIKeyNonConsumingRequest(method, path)
}
