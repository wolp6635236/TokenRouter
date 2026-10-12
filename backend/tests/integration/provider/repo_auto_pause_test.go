package provider_test

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"reflect"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/scheduler"
)

type providerIDsPayloadMatcher struct {
	want []int64
}

func (m providerIDsPayloadMatcher) Match(value driver.Value) bool {
	raw, ok := value.([]byte)
	if !ok {
		return false
	}
	var payload struct {
		ProviderIDs []int64 `json:"provider_ids"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return false
	}
	return reflect.DeepEqual(m.want, payload.ProviderIDs)
}

func TestAutoPauseExpiredProvidersEnqueuesAffectedProviders(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	now := time.Now()
	mock.ExpectQuery(`(?s)UPDATE providers.*RETURNING id`).
		WithArgs(now).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(11)).AddRow(int64(29)))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO scheduler_outbox (event_type, provider_id, group_id, payload)")).
		WithArgs(scheduler.SchedulerOutboxEventProviderBulkChanged, nil, nil, providerIDsPayloadMatcher{want: []int64{11, 29}}).
		WillReturnResult(sqlmock.NewResult(1, 1))

	repo := newProviderStoreContract(nil, db, nil)
	updated, err := repo.AutoPauseExpiredProviders(context.Background(), now)

	require.NoError(t, err)
	require.EqualValues(t, 2, updated)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAutoPauseExpiredProvidersSkipsOutboxWithoutChanges(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	now := time.Now()
	mock.ExpectQuery(`(?s)UPDATE providers.*RETURNING id`).
		WithArgs(now).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	repo := newProviderStoreContract(nil, db, nil)
	updated, err := repo.AutoPauseExpiredProviders(context.Background(), now)

	require.NoError(t, err)
	require.Zero(t, updated)
	require.NoError(t, mock.ExpectationsWereMet())
}
