package app

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/batchimage"
)

func TestNewBatchImageWorkerOptionsFromConfig_UsesFiniteReserveTimeout(t *testing.T) {
	opts := batchWorkerOptions(nil)
	require.Equal(t, batchimage.DefaultBatchImageWorkerReserveBlockTimeout, opts.ReserveBlockTimeout)
	require.Positive(t, opts.ReserveBlockTimeout)
}
