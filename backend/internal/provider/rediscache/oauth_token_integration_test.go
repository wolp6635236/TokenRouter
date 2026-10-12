//go:build integration

package rediscache

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/testutil/rediscontainer"
)

type GeminiTokenCacheSuite struct {
	suite.Suite
	ctx   context.Context
	rdb   *redis.Client
	cache provider.AccessTokenCache
}

func (s *GeminiTokenCacheSuite) SetupTest() {
	s.ctx = context.Background()
	s.rdb = rediscontainer.New(s.T())
	s.cache = NewOAuthTokenCache(s.rdb)
}

func (s *GeminiTokenCacheSuite) TestDeleteAccessToken() {
	cacheKey := "project-123"
	token := "token-value"
	require.NoError(s.T(), s.cache.SetAccessToken(s.ctx, cacheKey, token, time.Minute))

	got, err := s.cache.GetAccessToken(s.ctx, cacheKey)
	require.NoError(s.T(), err)
	require.Equal(s.T(), token, got)

	require.NoError(s.T(), s.cache.DeleteAccessToken(s.ctx, cacheKey))

	_, err = s.cache.GetAccessToken(s.ctx, cacheKey)
	require.True(s.T(), errors.Is(err, redis.Nil), "expected redis.Nil after delete")
}

func (s *GeminiTokenCacheSuite) TestDeleteAccessToken_MissingKey() {
	require.NoError(s.T(), s.cache.DeleteAccessToken(s.ctx, "missing-key"))
}

func TestGeminiTokenCacheSuite(t *testing.T) {
	suite.Run(t, new(GeminiTokenCacheSuite))
}
