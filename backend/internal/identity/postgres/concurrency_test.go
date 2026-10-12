package postgres

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestApplyRedeemConcurrencyAdjustment_UsesAtomicFloor(t *testing.T) {
	repo, mock := newRedeemAdjustmentRepoMock(t)
	mock.ExpectExec(`UPDATE users SET concurrency = GREATEST\(concurrency \+ \$1, 0\), updated_at = NOW\(\) WHERE id = \$2 AND deleted_at IS NULL`).
		WithArgs(-7, int64(42)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.ApplyRedeemConcurrencyAdjustment(context.Background(), 42, -7))
	require.NoError(t, mock.ExpectationsWereMet())
}
