package provider

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRefreshAttemptSnapshotIsolation 检查失败交换的比较快照保存交换前的嵌套凭据。
func TestRefreshAttemptSnapshotIsolation(t *testing.T) {
	credentials := map[string]any{"refresh_token": "initial", "session": map[string]any{"cookie": "initial"}}
	provider := &Record{ID: 1, Platform: PlatformOpenAI, Type: ProviderTypeOAuth, Credentials: credentials}
	attempted := snapshotRefreshRecord(provider)
	session, ok := credentials["session"].(map[string]any)
	require.True(t, ok)
	session["cookie"] = "changed-during-exchange"
	expected, ok := attempted.Credentials["session"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "initial", expected["cookie"])
}
