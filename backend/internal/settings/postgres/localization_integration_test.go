//go:build integration

package postgres_test

import (
	"context"
	"sync"
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/pkg/locale"
	"github.com/TokenFlux/TokenRouter/internal/settings"
	settingspg "github.com/TokenFlux/TokenRouter/internal/settings/postgres"
	"github.com/stretchr/testify/require"
)

// TestLocalizedSettingsConcurrentWrite 使用两个实例验证版本冲突与同批写入回滚。
func TestLocalizedSettingsConcurrentWrite(t *testing.T) {
	client, _ := settingsDatabase(t)
	ctx := context.Background()
	repo := settingspg.NewSettingRepository(client)
	const key = "test_localization_cas"
	const sibling = "test_localization_sibling"
	before := `{"revision":1}`
	require.NoError(t, repo.Set(ctx, key, before))
	t.Cleanup(func() { _ = repo.Delete(ctx, key); _ = repo.Delete(ctx, sibling) })
	results := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	start := make(chan struct{})
	for _, value := range []string{"first", "second"} {
		go func(value string) {
			store := settings.New(repo)
			session, err := store.Updates().Begin(ctx)
			if err != nil {
				ready.Done()
				results <- err
				return
			}
			defer session.Close()
			ready.Done()
			<-start
			results <- session.Commit(settings.PreparedChange{Module: "site", Values: map[string]string{key: value, sibling: value}, Expected: map[string]*string{key: &before}})
		}(value)
	}
	ready.Wait()
	close(start)
	first, second := <-results, <-results
	if first == nil {
		require.ErrorIs(t, second, locale.ErrConflict)
	} else {
		require.ErrorIs(t, first, locale.ErrConflict)
		require.NoError(t, second)
	}
	actual, err := repo.GetValue(ctx, key)
	require.NoError(t, err)
	other, err := repo.GetValue(ctx, sibling)
	require.NoError(t, err)
	require.Equal(t, actual, other)
}
