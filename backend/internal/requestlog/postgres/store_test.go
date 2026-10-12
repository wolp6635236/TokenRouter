package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/infra/telemetry"
	"github.com/TokenFlux/TokenRouter/internal/requestlog"
)

// TestSaveClassifiesInvalidRecords 数据错误可丢弃，连接故障和数据库约束错误保留重试。
func TestSaveClassifiesInvalidRecords(t *testing.T) {
	for _, test := range []struct {
		code    string
		invalid bool
	}{
		{"22P05", true},
		{"22001", true},
		{"08006", false},
		{"23514", false},
		{"40001", false},
	} {
		t.Run(test.code, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()
			failure := fmt.Errorf("write: %w", &pq.Error{Code: pq.ErrorCode(test.code)})
			mock.ExpectBegin()
			mock.ExpectPrepare("INSERT INTO request_records").ExpectExec().WillReturnError(failure)
			mock.ExpectRollback()
			err = NewStore(db).Save(t.Context(), []telemetry.RequestRecord{{RequestID: "id"}})
			require.ErrorIs(t, err, failure)
			if test.invalid {
				require.ErrorIs(t, err, requestlog.ErrInvalidRecord)
			} else {
				require.NotErrorIs(t, err, requestlog.ErrInvalidRecord)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// TestSaveRejectsUnencodableRecord 超出 JSON 时间范围的快照可以被队列识别为无效。
func TestSaveRejectsUnencodableRecord(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	mock.ExpectBegin()
	mock.ExpectPrepare("INSERT INTO request_records")
	mock.ExpectRollback()
	err = NewStore(db).Save(t.Context(), []telemetry.RequestRecord{{RequestID: "id", StartedAt: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}})
	require.ErrorIs(t, err, requestlog.ErrInvalidRecord)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestFindResolvesIDsOnce 检查同一详情查询共用候选 ID，并保留各来源的权限参数。
func TestFindResolvesIDsOnce(t *testing.T) {
	for _, test := range []struct {
		name  string
		admin bool
		exact bool
	}{
		{"user external", false, false},
		{"admin external", true, false},
		{"user exact", false, true},
		{"admin exact", true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()
			ids := pq.Array([]string{"local-request", "client:legacy"})
			mock.ExpectQuery(`SELECT ARRAY\(SELECT id FROM request_lookup_ids\(\$1\)\)`).
				WithArgs("external-id").WillReturnRows(sqlmock.NewRows([]string{"ids", "exact"}).AddRow(`{"local-request","client:legacy"}`, test.exact))
			mock.ExpectQuery(`SELECT record FROM request_records`).WithArgs(ids, int64(42), test.admin).
				WillReturnRows(sqlmock.NewRows([]string{"record"}))
			mock.ExpectQuery(`SELECT .* FROM usage_logs`).WithArgs(ids, int64(42), test.admin, "external-id", test.exact).
				WillReturnRows(sqlmock.NewRows([]string{"id"}))
			mock.ExpectQuery(`SELECT .* FROM ops_error_logs`).WithArgs(ids, int64(42), test.admin, test.exact).
				WillReturnRows(sqlmock.NewRows([]string{"id"}))
			if test.admin {
				mock.ExpectQuery(`SELECT .* FROM audit_logs`).WithArgs(ids).
					WillReturnRows(sqlmock.NewRows([]string{"id"}))
			}
			items, err := NewStore(db).Find(t.Context(), "external-id", 42, test.admin)
			require.NoError(t, err)
			require.Empty(t, items)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// TestFindStopsWhenLookupIsCanceled ID 解析被取消时结束查询，后续来源不继续读库。
func TestFindStopsWhenLookupIsCanceled(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	mock.ExpectQuery(`SELECT ARRAY`).WithArgs("id").WillReturnError(context.Canceled)
	_, err = NewStore(db).Find(t.Context(), "id", 42, false)
	require.ErrorIs(t, err, context.Canceled)
	require.NoError(t, mock.ExpectationsWereMet())
}
