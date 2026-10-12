package provider

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

func cloneQoderFailedCredentials(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	dst := make(map[string]any, len(src))
	for key, value := range src {
		if nested, ok := value.(map[string]any); ok {
			dst[key] = cloneQoderFailedCredentials(nested)
			continue
		}
		dst[key] = value
	}
	return dst
}

func TestQoderGatewayRefreshExecutorNeedsRefreshUsesFailedCredentialSnapshot(t *testing.T) {
	now := time.Now()
	failedProvider := Record{
		ID:       96,
		Platform: capability.PlatformQoder,
		Type:     capability.ProviderTypeCosy,
		Credentials: map[string]any{
			"security_oauth_token": "failed-token",
			"refresh_token":        "failed-refresh",
			"machine_id":           "machine-1",
			"expires_at":           now.Add(1 * time.Hour).Format(time.RFC3339),
		},
	}
	rotatedProvider := failedProvider
	rotatedProvider.Credentials = cloneQoderFailedCredentials(failedProvider.Credentials)
	rotatedProvider.Credentials["security_oauth_token"] = "rotated-token"
	rotatedProvider.Credentials["refresh_token"] = "rotated-refresh"

	failedCredentials := QoderRefreshCredentialsHash(failedProvider.Credentials)

	require.True(t, NeedsRefreshQoderAfterFailure(&failedProvider, failedCredentials, 15*time.Minute))

	failedWithoutExpiry := failedProvider
	failedWithoutExpiry.Credentials = cloneQoderFailedCredentials(failedProvider.Credentials)
	delete(failedWithoutExpiry.Credentials, "expires_at")
	failedCredentials = QoderRefreshCredentialsHash(failedWithoutExpiry.Credentials)
	require.True(t, NeedsRefreshQoderAfterFailure(&failedWithoutExpiry, failedCredentials, 15*time.Minute))

	changedMapping := failedProvider
	changedMapping.Credentials = cloneQoderFailedCredentials(failedProvider.Credentials)
	changedMapping.Credentials["model_mapping"] = map[string]any{"qwen3.7-plus": "qmodel"}
	failedCredentials = QoderRefreshCredentialsHash(failedProvider.Credentials)
	require.True(t, NeedsRefreshQoderAfterFailure(&changedMapping, failedCredentials, 15*time.Minute))

	failedCredentials = QoderRefreshCredentialsHash(failedProvider.Credentials)
	require.False(t, NeedsRefreshQoderAfterFailure(&rotatedProvider, failedCredentials, 15*time.Minute))

	missingRefreshToken := failedProvider
	missingRefreshToken.Credentials = cloneQoderFailedCredentials(failedProvider.Credentials)
	delete(missingRefreshToken.Credentials, "refresh_token")
	require.False(t, NeedsRefreshQoderAfterFailure(&missingRefreshToken, failedCredentials, 15*time.Minute))
	require.False(t, NeedsRefreshQoderAfterFailure(&failedProvider, "", 15*time.Minute))
}
