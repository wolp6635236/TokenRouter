package httpapi

import (
	"context"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
)

func (s *openAIShadowFixture) CreateShadow(ctx context.Context, parentID int64, opts providercore.ShadowOptions) (*providercore.Record, error) {
	if s.createSparkShadowErr != nil {
		return nil, s.createSparkShadowErr
	}
	pid := parentID
	return &providercore.Record{
		ID:               9001,
		Name:             opts.Name,
		Platform:         capability.PlatformOpenAI,
		Type:             capability.ProviderTypeOAuth,
		Priority:         opts.Priority,
		Concurrency:      opts.Concurrency,
		GroupIDs:         opts.GroupIDs,
		ParentProviderID: &pid,
		QuotaDimension:   providercore.QuotaDimensionSpark,
		Credentials:      map[string]any{},
		Extra:            map[string]any{},
	}, nil
}

// openAIShadowFixture 返回影子创建结果或测试指定的错误。
type openAIShadowFixture struct {
	OpenAIAdminOperations
	createSparkShadowErr error
}
