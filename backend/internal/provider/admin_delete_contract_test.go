package provider_test

import (
	"context"
	"errors"
	"testing"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/stretchr/testify/require"
)

// deleteProviderStore 替换删除所需的数据库读写，级联顺序由 Admin 执行。
type deleteProviderStore struct {
	providercore.AdminStore
	shadows            []*providercore.Record
	listErr, deleteErr error
	deletedIDs         []int64
}

func (s *deleteProviderStore) ListShadowsByParent(context.Context, int64) ([]*providercore.Record, error) {
	return s.shadows, s.listErr
}

func (s *deleteProviderStore) Delete(_ context.Context, id int64) error {
	s.deletedIDs = append(s.deletedIDs, id)
	return s.deleteErr
}

func TestAdminDeleteProviderNotFound(t *testing.T) {
	repo := &deleteProviderStore{deleteErr: providercore.ErrProviderNotFound}
	err := providercore.NewAdmin(repo, providercore.AdminOptions{}).DeleteProvider(t.Context(), 55)
	require.ErrorIs(t, err, providercore.ErrProviderNotFound)
	require.Equal(t, []int64{55}, repo.deletedIDs)
}

func TestAdminDeleteProviderLookupFailureDoesNotDelete(t *testing.T) {
	cause := errors.New("db down")
	repo := &deleteProviderStore{listErr: cause}
	err := providercore.NewAdmin(repo, providercore.AdminOptions{}).DeleteProvider(t.Context(), 55)
	require.ErrorIs(t, err, cause)
	require.Empty(t, repo.deletedIDs)
}

func TestAdminDeleteProviderStorageFailure(t *testing.T) {
	cause := errors.New("delete failed")
	repo := &deleteProviderStore{deleteErr: cause}
	err := providercore.NewAdmin(repo, providercore.AdminOptions{}).DeleteProvider(t.Context(), 55)
	require.ErrorIs(t, err, cause)
	require.Equal(t, []int64{55}, repo.deletedIDs)
}

func TestAdminDeleteProviderDeletesShadowsBeforeParent(t *testing.T) {
	repo := &deleteProviderStore{shadows: []*providercore.Record{{ID: 56}}}
	err := providercore.NewAdmin(repo, providercore.AdminOptions{}).DeleteProvider(t.Context(), 55)
	require.NoError(t, err)
	require.Equal(t, []int64{56, 55}, repo.deletedIDs)
}
