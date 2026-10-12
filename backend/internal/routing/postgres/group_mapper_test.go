package postgres

import (
	"testing"

	"github.com/stretchr/testify/require"

	dbent "github.com/TokenFlux/TokenRouter/ent"
	"github.com/TokenFlux/TokenRouter/internal/routing"
)

func TestGroupEntityToService_PreservesImageGenerationControls(t *testing.T) {
	group := &dbent.Group{
		ID:   1,
		Name: "openai-images",

		Status:               routing.StatusActive,
		RateMultiplier:       1,
		AllowImageGeneration: true,
	}

	got := GroupFromEnt(group)
	require.NotNil(t, got)
	require.True(t, got.AllowImageGeneration)
}
