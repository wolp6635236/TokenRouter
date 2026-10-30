package app

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/batchimage"
	"github.com/TokenFlux/TokenRouter/internal/protocol"
	"github.com/TokenFlux/TokenRouter/internal/routing"
)

// batch_image_worker_test.go checks batch worker defaults and group admission
// rules. Queue reservation uses normalized defaults when application settings
// are absent, and batch permission follows the unified protocol selection even
// when a legacy mirror contains the opposite value.

// batchGroupPolicyFixture exposes one group for batch admission and pricing reads.
type batchGroupPolicyFixture struct {
	routing.GroupRepository                // unused group persistence methods
	group                   *routing.Group // configured protocol policy record
}

// batchPriceConfigFixture supplies absent prices for protocol admission checks.
type batchPriceConfigFixture struct {
	routing.PricingConfigRepository // unused pricing persistence methods
}

// ListAll returns no price overrides for the batch admission fixture.
func (batchPriceConfigFixture) ListAll(context.Context) ([]routing.PricingConfig, error) {
	return nil, nil
}

// TestNewBatchImageWorkerOptionsFromConfig_UsesFiniteReserveTimeout passes nil
// configuration to the batch worker adapter. It requires ReserveBlockTimeout
// to equal the module's default and remain a positive duration.
func TestNewBatchImageWorkerOptionsFromConfig_UsesFiniteReserveTimeout(t *testing.T) {
	opts := batchWorkerOptions(nil)
	require.Equal(t, batchimage.DefaultBatchImageWorkerReserveBlockTimeout, opts.ReserveBlockTimeout)
	require.Positive(t, opts.ReserveBlockTimeout)
}

// GetByIDLite returns the configured group for each batch policy lookup.
func (r batchGroupPolicyFixture) GetByIDLite(context.Context, int64) (*routing.Group, error) {
	return r.group, nil
}

// TestBatchPricingGroupsUsesUnifiedProtocols supplies contradictory legacy mirrors
// and protocol selections. Batch admission must follow only the unified selection,
// including explicit empty selections that close creation despite a true mirror.
func TestBatchPricingGroupsUsesUnifiedProtocols(t *testing.T) {
	for _, allowed := range []bool{false, true} {
		group := &routing.Group{ID: 7, AllowBatchImageGeneration: !allowed}
		if allowed {
			group.AllowedProtocols = []protocol.ProtocolID{protocol.ProtocolImageBatches}
		}
		settings := routing.NewPricingConfigService(batchPriceConfigFixture{}, nil)
		reader := batchPricingGroups{source: batchGroupPolicyFixture{group: group}, settings: settings}
		result, err := reader.GetByIDLite(context.Background(), group.ID)
		require.NoError(t, err)
		require.Equal(t, allowed, result.AllowBatchImageGeneration)
	}
}
