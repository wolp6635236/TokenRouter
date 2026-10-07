package googleforward_test

import (
	"context"
	"time"

	gatewaytestkit "github.com/TokenFlux/TokenRouter/internal/gateway/testkit"
)

type rateLimit429ProviderRepoStub struct {
	gatewaytestkit.ErrorPolicyStore
	rateLimitCalls     int
	lastRateLimitID    int64
	lastRateLimitReset time.Time
}

func (r *rateLimit429ProviderRepoStub) SetRateLimited(_ context.Context, id int64, resetAt time.Time) error {
	r.rateLimitCalls++
	r.lastRateLimitID = id
	r.lastRateLimitReset = resetAt
	return nil
}
