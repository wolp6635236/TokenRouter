package provider

import (
	"context"
	"log/slog"
	"reflect"
	"time"
)

// newOpenAIRefreshSourceFixture 夹具只组合生产实现，缓存等待、资格、刷新与 CAS 算法不在测试中复制。
func newOpenAIRefreshSourceFixture(repo *openAIProviderRepoStub, cache *openAITokenCacheStub, authorization *openAIOAuthServiceStub) *OpenAITokenSource {
	source := &OpenAITokenSource{Repository: repo, Cache: cache, Metrics: &OpenAITokenMetricsStore{}, Policy: OpenAIProviderRefreshPolicy(), Debug: slog.Debug, Warn: slog.Warn}
	if authorization != nil {
		api := NewOAuthRefreshAPI(repo, cache, RefreshOptions{Platform: ProviderRefreshPlatformPolicy()})
		executor := &OpenAITokenRefresher{Authorization: authorization}
		source.Refresh = func(ctx context.Context, value *Record, window time.Duration) (*OAuthRefreshResult, error) {
			return api.RefreshIfNeeded(ctx, value, executor, window)
		}
	}
	return source
}

func newClaudeRefreshSourceFixture(repo *claudeProviderRepoStub, cache *claudeTokenCacheStub, authorization *claudeOAuthServiceStub) *ClaudeTokenSource {
	source := &ClaudeTokenSource{Options: ClaudeTokenOptions{Repository: repo, Cache: cache, Policy: ClaudeProviderRefreshPolicy(), Debug: slog.Debug, Warn: slog.Warn}}
	if authorization != nil {
		api := NewOAuthRefreshAPI(repo, cache, RefreshOptions{Platform: ProviderRefreshPlatformPolicy()})
		executor := claudeExchangeFixture{authorization}
		source.Options.Refresh = func(ctx context.Context, value *Record, window time.Duration) (*OAuthRefreshResult, error) {
			return api.RefreshIfNeeded(ctx, value, executor, window)
		}
	}
	return source
}

// Claude 执行器只注入测试交换结果，资格与合并调用所属模块的生产函数。
type claudeExchangeFixture struct{ exchange *claudeOAuthServiceStub }

func (claudeExchangeFixture) CanRefresh(value *Record) bool { return CanRefreshClaude(value) }
func (claudeExchangeFixture) NeedsRefresh(value *Record, window time.Duration) bool {
	return NeedsRefreshClaude(value, window)
}
func (claudeExchangeFixture) CacheKey(value *Record) string { return ClaudeTokenCacheKey(value) }
func (e claudeExchangeFixture) Refresh(ctx context.Context, value *Record) (map[string]any, error) {
	return RefreshClaudeCredentials(ctx, value, e.exchange.RefreshProviderToken)
}

// UpdateOAuthCredentialsIfUnchanged 模拟条件写入，并支持注入写入失败。
func (r *openAIProviderRepoStub) UpdateOAuthCredentialsIfUnchanged(ctx context.Context, version CredentialVersion, credentials map[string]any) (bool, error) {
	if r.provider == nil || !reflect.DeepEqual(FailureVersion(r.provider).CredentialVersion, version) {
		return false, nil
	}
	next := CloneRecord(r.provider)
	next.Credentials = CloneValues(credentials)
	err := r.Update(ctx, next)
	return err == nil, err
}

func (r *claudeProviderRepoStub) UpdateOAuthCredentialsIfUnchanged(ctx context.Context, version CredentialVersion, credentials map[string]any) (bool, error) {
	if r.provider == nil || !reflect.DeepEqual(FailureVersion(r.provider).CredentialVersion, version) {
		return false, nil
	}
	next := CloneRecord(r.provider)
	next.Credentials = CloneValues(credentials)
	err := r.Update(ctx, next)
	return err == nil, err
}
