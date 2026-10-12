package textattempt

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

func TestShouldUseAntigravityCompat(t *testing.T) {
	tests := []struct {
		name     string
		provider *gatewayprovider.ExecutionProvider
		want     bool
	}{
		{"oauth", &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformAntigravity, Type: capability.ProviderTypeOAuth}}, true},
		{"setup token", &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformAntigravity, Type: capability.ProviderTypeSetupToken}}, false},
		{"upstream", &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformAntigravity, Type: capability.ProviderTypeUpstream}}, false},
		{"api key", &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformAntigravity, Type: capability.ProviderTypeAPIKey}}, false},
		{"anthropic oauth", &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformAnthropic, Type: capability.ProviderTypeOAuth}}, false},
		{"nil", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, shouldUseAntigravityCompat(tt.provider))
		})
	}
}
