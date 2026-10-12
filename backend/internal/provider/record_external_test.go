package provider_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/provider"
)

func TestProvider_BillingRateMultiplier_DefaultsToOneWhenNil(t *testing.T) {
	var a provider.Record
	require.NoError(t, json.Unmarshal([]byte(`{"id":1,"name":"acc","status":"active"}`), &a))
	require.Nil(t, a.RateMultiplier)
	require.Equal(t, 1.0, a.BillingRateMultiplier())
}

func TestProvider_BillingRateMultiplier_AllowsZero(t *testing.T) {
	v := 0.0
	a := provider.Record{RateMultiplier: &v}
	require.Equal(t, 0.0, a.BillingRateMultiplier())
}

func TestProvider_BillingRateMultiplier_NegativeFallsBackToOne(t *testing.T) {
	v := -1.0
	a := provider.Record{RateMultiplier: &v}
	require.Equal(t, 1.0, a.BillingRateMultiplier())
}
