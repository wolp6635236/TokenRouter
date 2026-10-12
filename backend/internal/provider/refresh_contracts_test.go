package provider

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewOAuthRefreshAPI_DefaultTTL(t *testing.T) {
	api := NewOAuthRefreshAPI(nil, nil, RefreshOptions{})
	require.Equal(t, defaultRefreshLockTTL, api.lockTTL)
}

func TestNewOAuthRefreshAPI_CustomTTL(t *testing.T) {
	api := NewOAuthRefreshAPI(nil, nil, RefreshOptions{LockTTL: 90 * time.Second})
	require.Equal(t, 90*time.Second, api.lockTTL)
}

func TestNewOAuthRefreshAPI_ZeroTTLUsesDefault(t *testing.T) {
	api := NewOAuthRefreshAPI(nil, nil, RefreshOptions{})
	require.Equal(t, defaultRefreshLockTTL, api.lockTTL)
}
