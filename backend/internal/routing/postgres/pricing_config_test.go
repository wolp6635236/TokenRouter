package postgres

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/pkg/pagination"
)

func TestPricingConfigListOrderBy_AllowsDescendingIDSort(t *testing.T) {
	params := pagination.PaginationParams{
		SortBy:    "id",
		SortOrder: "desc",
	}

	require.Equal(t, "c.id DESC, c.id DESC", pricingConfigListOrderBy(params))
}
