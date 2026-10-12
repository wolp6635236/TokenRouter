package apikey

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/creative"
)

type creativeFakeManagedKeyRepo struct {
	key     *APIKey
	getErr  error
	createN int
}

// TestEnsureCreativeManagedKey 校验隐藏执行 Key 的幂等供应。
func TestEnsureCreativeManagedKey(t *testing.T) {
	repo := &creativeFakeManagedKeyRepo{}
	manager := ManagedKeys{Store: repo, Prefix: "sk-", ManagedBy: creative.CreativeManagedBy, NamePrefix: "creative-studio"}
	ctx := context.Background()

	key, err := manager.Ensure(ctx, 7, 12)
	require.NoError(t, err)
	require.NotNil(t, key.ManagedBy)
	require.Equal(t, creative.CreativeManagedBy, *key.ManagedBy)
	require.Equal(t, APIKeyBillingModeAuto, key.BillingMode)
	require.Equal(t, int64(12), *key.GroupID)
	require.Equal(t, "creative-studio:12", key.Name)

	// 第二次供应直接复用已创建的 Key。
	reused, err := manager.Ensure(ctx, 7, 12)
	require.NoError(t, err)
	require.Equal(t, key.ID, reused.ID)
	require.Equal(t, 1, repo.createN)
}

func (r *creativeFakeManagedKeyRepo) GetManagedKeyByUserAndGroup(ctx context.Context, userID, groupID int64, managedBy string) (*APIKey, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	if r.key != nil {
		return r.key, nil
	}
	return nil, ErrAPIKeyNotFound
}

func (r *creativeFakeManagedKeyRepo) CreateManagedKey(ctx context.Context, key *APIKey) error {
	r.createN++
	if key.ID == 0 {
		key.ID = 900 + int64(r.createN)
	}
	r.key = key
	return nil
}
