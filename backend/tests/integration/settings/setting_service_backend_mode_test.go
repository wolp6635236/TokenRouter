package settings_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/config"
	"github.com/TokenFlux/TokenRouter/internal/gateway"
	"github.com/TokenFlux/TokenRouter/internal/settings/composite"
	settingskit "github.com/TokenFlux/TokenRouter/internal/settings/testkit"
)

func TestUpdateSettings_InvalidatesBackendModeCache(t *testing.T) {
	repo := &bmUpdateRepoStub{
		getValueFn: func(ctx context.Context, key string) (string, error) {
			require.Equal(t, gateway.SettingKeyBackendModeEnabled, key)
			return "true", nil
		},
	}
	svc := settingskit.NewComposite(repo, &config.Config{})
	svc.Backend.Publish(true)

	err := svc.Save(context.Background(), &composite.Snapshot{
		BackendModeEnabled: false,
	})
	require.NoError(t, err)
	require.Equal(t, "false", repo.updates[gateway.SettingKeyBackendModeEnabled])
	require.False(t, svc.Backend.Enabled(context.Background()))
}
