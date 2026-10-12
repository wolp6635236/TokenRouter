package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/TokenFlux/TokenRouter/internal/scheduler"
)

const (
	StatusClientClosedRequest = 499

	GatewayQueueFullCode        = "gateway_queue_full"
	GatewayConcurrencyLimitCode = "gateway_concurrency_limit"
)

func ConcurrencyErrorResponse(err error, slotType string) (int, string, string, string) {
	if _, ok := errors.AsType[*scheduler.WaitQueueFullError](err); ok {
		return http.StatusTooManyRequests, "rate_limit_error", GatewayQueueFullCode,
			"Too many pending requests, please retry later"
	}

	if concurrencyErr, ok := errors.AsType[*scheduler.ConcurrencyError](err); ok {
		if concurrencyErr.SlotType != "" {
			slotType = concurrencyErr.SlotType
		}
		return http.StatusTooManyRequests, "rate_limit_error", GatewayConcurrencyLimitCode,
			fmt.Sprintf("Concurrency limit exceeded for %s, please retry later", slotType)
	}

	if errors.Is(err, context.Canceled) {
		return StatusClientClosedRequest, "api_error", "", "context canceled"
	}

	return http.StatusServiceUnavailable, "api_error", "", "Service temporarily unavailable, please retry later"
}
