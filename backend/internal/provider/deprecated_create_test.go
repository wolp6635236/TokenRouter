package provider

import (
	context "context"
	testing "testing"
	time "time"

	require "github.com/stretchr/testify/require"
)

func TestAdminServiceCreateProviderDiscardsDeprecatedLongContextBillingExtra(t *testing.T) {
	repo := &deprecatedCreateStore{}
	svc := NewAdmin(repo, AdminOptions{Creation: CreationOptions{Now: time.Now, LoadLocation: time.LoadLocation}, Credentials: CreateCredentialHooks{Validate: func(context.Context, *Record) error { return nil }}})

	provider, err := svc.CreateProvider(context.Background(), &CreateProviderInput{
		Name:        "openai-provider",
		Platform:    PlatformOpenAI,
		Type:        ProviderTypeAPIKey,
		Credentials: map[string]any{"api_key": "test"},
		Extra:       map[string]any{"openai_long_context_billing_enabled": "malformed", "preserved": true},
	})

	require.NoError(t, err)
	require.Same(t, provider, repo.createdProvider)
	require.NotContains(t, provider.Extra, "openai_long_context_billing_enabled")
	require.Equal(t, true, provider.Extra["preserved"])
}

// 此处检查创建结果的指针，HTTP 和使用方测试覆盖模型数据转换。
type deprecatedCreateStore struct {
	AdminStore
	createdProvider *Record
}

func (s *deprecatedCreateStore) Create(_ context.Context, value *Record) error {
	value.ID = 1
	s.createdProvider = value
	return nil
}
