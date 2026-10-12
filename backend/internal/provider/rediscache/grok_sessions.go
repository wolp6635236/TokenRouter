package rediscache

import (
	"github.com/redis/go-redis/v9"

	"github.com/TokenFlux/TokenRouter/internal/infra/redis/session"
	"github.com/TokenFlux/TokenRouter/internal/provider"
)

func NewGrokSessionStore(rdb *redis.Client) *provider.GrokSessionStore {
	if rdb == nil {
		return provider.NewGrokSessionStore(nil)
	}
	return provider.NewGrokSessionStore(session.New(rdb, "oauth:session:xai", provider.GrokSessionTTL))
}
