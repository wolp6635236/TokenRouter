package provider_test

import (
	"testing"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"

	"github.com/stretchr/testify/require"
)

func intPtrHelper(v int) *int { return &v }

func TestEffectiveLoadFactor_NilProvider(t *testing.T) {
	var a *providercore.Record
	require.Equal(t, 1, a.EffectiveLoadFactor())
}

func TestEffectiveLoadFactor_NilLoadFactor_PositiveConcurrency(t *testing.T) {
	a := &providercore.Record{Concurrency: 5}
	require.Equal(t, 5, a.EffectiveLoadFactor())
}

func TestEffectiveLoadFactor_NilLoadFactor_ZeroConcurrency(t *testing.T) {
	a := &providercore.Record{Concurrency: 0}
	require.Equal(t, 1, a.EffectiveLoadFactor())
}

func TestEffectiveLoadFactor_PositiveLoadFactor(t *testing.T) {
	a := &providercore.Record{Concurrency: 5, LoadFactor: intPtrHelper(20)}
	require.Equal(t, 20, a.EffectiveLoadFactor())
}

func TestEffectiveLoadFactor_ZeroLoadFactor_FallbackToConcurrency(t *testing.T) {
	a := &providercore.Record{Concurrency: 5, LoadFactor: intPtrHelper(0)}
	require.Equal(t, 5, a.EffectiveLoadFactor())
}

func TestEffectiveLoadFactor_NegativeLoadFactor_FallbackToConcurrency(t *testing.T) {
	a := &providercore.Record{Concurrency: 3, LoadFactor: intPtrHelper(-1)}
	require.Equal(t, 3, a.EffectiveLoadFactor())
}

func TestEffectiveLoadFactor_ZeroLoadFactor_ZeroConcurrency(t *testing.T) {
	a := &providercore.Record{Concurrency: 0, LoadFactor: intPtrHelper(0)}
	require.Equal(t, 1, a.EffectiveLoadFactor())
}
