package provider

import (
	"context"
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/egress"
	"github.com/TokenFlux/TokenRouter/internal/provider"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	xai "github.com/TokenFlux/TokenRouter/internal/upstream/grok"
	"github.com/stretchr/testify/require"
)

func TestBuildGrokBillingURLUsesCLIForOfficialAPIHosts(t *testing.T) {
	for _, baseURL := range []string{xai.DefaultBaseURL, "https://us-west-2.api.x.ai/v1"} {
		value := &provider.Record{Platform: capability.PlatformGrok, Type: capability.ProviderTypeOAuth, Credentials: map[string]any{"base_url": baseURL}}

		weekly, err := billingURLForTest(value, (egress.OperatorURLPolicy{}).Validate, true)
		require.NoError(t, err)
		require.Equal(t, xai.DefaultCLIBaseURL+xai.BillingWeeklyPath, weekly)
	}
}

func TestBuildGrokBillingURLKeepsCustomRelay(t *testing.T) {
	value := &provider.Record{Platform: capability.PlatformGrok, Type: capability.ProviderTypeOAuth, Credentials: map[string]any{
		"base_url": "https://relay.example.test/xai/v1",
	}}

	monthly, err := billingURLForTest(value, (egress.OperatorURLPolicy{}).Validate, false)
	require.NoError(t, err)
	require.Equal(t, "https://relay.example.test/xai/v1"+xai.BillingMonthlyPath, monthly)
}

func TestGrokBillingURLFollowsProviderBaseURL(t *testing.T) {
	t.Run("oauth default stays on CLI gateway", func(t *testing.T) {
		value := &provider.Record{
			Platform:    capability.PlatformGrok,
			Type:        capability.ProviderTypeOAuth,
			Credentials: map[string]any{},
		}

		weeklyURL, err := billingURLForTest(value, nil, true)
		require.NoError(t, err)
		require.Equal(t, xai.DefaultCLIBaseURL+"/billing?format=credits", weeklyURL)

		monthlyURL, err := billingURLForTest(value, nil, false)
		require.NoError(t, err)
		require.Equal(t, xai.DefaultCLIBaseURL+"/billing", monthlyURL)
	})

	t.Run("oauth custom forwarding address carries billing probes", func(t *testing.T) {
		value := &provider.Record{
			Platform: capability.PlatformGrok,
			Type:     capability.ProviderTypeOAuth,
			Credentials: map[string]any{
				"base_url": "https://relay.example.test/v1",
			},
		}

		weeklyURL, err := billingURLForTest(value, nil, true)
		require.NoError(t, err)
		require.Equal(t, "https://relay.example.test/v1/billing?format=credits", weeklyURL)
	})

	t.Run("billing probe honors the operator allowlist like forwarding", func(t *testing.T) {
		// 额度探测与转发共用 URL 策略，被白名单拒绝的主机会在发送 OAuth token 前返回错误。
		value := &provider.Record{
			Platform: capability.PlatformGrok,
			Type:     capability.ProviderTypeOAuth,
			Credentials: map[string]any{
				"base_url": "https://relay.example.test/v1",
			},
		}
		policy := egress.OperatorURLPolicy{Enabled: true, UpstreamHosts: []string{"cli-chat-proxy.grok.com"}}

		_, err := billingURLForTest(value, policy.Validate, true)
		require.EqualError(t, err, "invalid base url: base URL rejected by URL security policy")
	})
}

// billingURLForTest 组合 URL 校验和端点解析函数。
func billingURLForTest(value *provider.Record, operator xai.BaseURLValidator, weekly bool) (string, error) {
	validator, err := GrokBaseURLValidator(value, operator)
	if err != nil {
		return "", err
	}
	requests := &GrokQuotaTransport{OperatorValidator: operator}
	return xai.BuildBillingEndpointURL(requests.baseURL(context.Background(), value, false), weekly, validator)
}
