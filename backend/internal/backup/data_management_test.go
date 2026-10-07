package backup_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/backup"
	"github.com/TokenFlux/TokenRouter/internal/server/httpx"

	"github.com/stretchr/testify/require"
)

func TestDataManagementService_GetAgentHealth_Deprecated(t *testing.T) {
	t.Parallel()

	socketPath := filepath.Join(t.TempDir(), "unused.sock")
	svc := backup.NewDataManagementServiceWithOptions(socketPath, 0)
	health := svc.GetAgentHealth(context.Background())

	require.False(t, health.Enabled)
	require.Equal(t, backup.DataManagementDeprecatedReason, health.Reason)
	require.Equal(t, socketPath, health.SocketPath)
}

func TestDataManagementService_EnsureAgentEnabled_Deprecated(t *testing.T) {
	t.Parallel()

	socketPath := filepath.Join(t.TempDir(), "unused.sock")
	svc := backup.NewDataManagementServiceWithOptions(socketPath, 100)
	err := svc.EnsureAgentEnabled(context.Background())
	require.Error(t, err)

	statusCode, status := httpx.ToHTTP(err)
	require.Equal(t, 503, statusCode)
	require.Equal(t, backup.DataManagementDeprecatedReason, status.Reason)
	require.Equal(t, socketPath, status.Metadata["socket_path"])
}
