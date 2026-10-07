package billing

import (
	"context"
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/pkg/apperror"
	"github.com/stretchr/testify/require"
)

// TestPlanTextRequiresLocalization 防止普通列更新成功后，购买页仍读取旧内容。
func TestPlanTextRequiresLocalization(t *testing.T) {
	text := "Updated"
	for _, request := range []UpdatePlanRequest{
		{Name: &text}, {Description: &text}, {Features: &text}, {ProductName: &text},
	} {
		_, err := NewPlans(nil, nil).UpdatePlan(context.Background(), 1, request)
		require.Equal(t, "LOCALIZATION_REQUIRED", apperror.Reason(err))
	}
}
