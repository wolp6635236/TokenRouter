package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/upstream/bedrock"
)

func TestNewBedrockSignerFromProvider_DefaultRegion(t *testing.T) {
	provider := &provider.Record{
		Platform: capability.PlatformAnthropic,
		Type:     capability.ProviderTypeBedrock,
		Credentials: map[string]any{
			"aws_access_key_id":     "test-akid",
			"aws_secret_access_key": "test-secret",
		},
	}

	signer, err := NewBedrockSignerFromProvider(provider)
	require.NoError(t, err)
	require.NotNil(t, signer)
	assert.Equal(t, bedrock.DefaultBedrockRegion, signer.Region)
}
