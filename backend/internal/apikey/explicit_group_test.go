package apikey_test

import (
	"context"
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/identity"
	"github.com/stretchr/testify/require"
)

// TestAPIKeyCreateRequiresExplicitPositiveGroup 验证未绑定分组的普通 Key 在创建时拒绝，不尝试读取或创建默认分组。
func TestAPIKeyCreateRequiresExplicitPositiveGroup(t *testing.T) {
	zero, negative := int64(0), int64(-1)
	for _, groupID := range []*int64{nil, &zero, &negative} {
		service := newAPIKeyTestService(apiKeyTestDependencies{})
		_, err := service.Create(context.Background(), 7, apikey.CreateAPIKeyRequest{Name: "missing group", GroupID: groupID})
		require.Error(t, err)
		require.Contains(t, err.Error(), "GROUP_REQUIRED")
	}
}

// TestAPIKeyLoadingPreservesUnboundLegacyKey 验证历史无组 Key 的字符串保持可管理，加载时不偷偷绑定其它分组。
func TestAPIKeyLoadingPreservesUnboundLegacyKey(t *testing.T) {
	repo := &authRepoStub{getByKeyForAuth: func(_ context.Context, key string) (*apikey.APIKey, error) {
		return &apikey.APIKey{ID: 19, UserID: 7, Key: key, Status: billing.StatusActive, User: &identity.User{ID: 7, Status: billing.StatusActive, Role: identity.RoleUser}}, nil
	}}
	service := newAPIKeyTestService(apiKeyTestDependencies{apiKeyRepo: repo})
	loaded, err := service.GetByKey(context.Background(), "unchanged-legacy-key")
	require.NoError(t, err)
	require.Equal(t, "unchanged-legacy-key", loaded.Key)
	require.Nil(t, loaded.GroupID)
	require.Nil(t, loaded.Group)
}
