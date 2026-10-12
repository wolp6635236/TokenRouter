package provider

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBackgroundRefreshPolicy_DefaultSkips(t *testing.T) {
	p := DefaultBackgroundRefreshPolicy()

	require.ErrorIs(t, p.HandleLockHeld(), ErrRefreshSkipped)
	require.ErrorIs(t, p.HandleAlreadyRefreshed(), ErrRefreshSkipped)
}

func TestBackgroundRefreshPolicy_SuccessOverride(t *testing.T) {
	p := BackgroundRefreshPolicy{
		OnLockHeld:       BackgroundSkipAsSuccess,
		OnAlreadyRefresh: BackgroundSkipAsSuccess,
	}

	require.NoError(t, p.HandleLockHeld())
	require.NoError(t, p.HandleAlreadyRefreshed())
}
