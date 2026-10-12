package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	dbent "github.com/TokenFlux/TokenRouter/ent"
	"github.com/TokenFlux/TokenRouter/ent/enttest"
	dbusagecleanuptask "github.com/TokenFlux/TokenRouter/ent/usagecleanuptask"
	"github.com/TokenFlux/TokenRouter/internal/pkg/pagination"
	"github.com/TokenFlux/TokenRouter/internal/usage"
)

func newUsageCleanupEntRepo(t *testing.T) (*CleanupStore, *dbent.Client) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:usage_cleanup?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(drv)))
	t.Cleanup(func() { _ = client.Close() })

	repo := &CleanupStore{client: client, sql: db}
	return repo, client
}

func TestUsageCleanupRepositoryEntCreateAndList(t *testing.T) {
	repo, _ := newUsageCleanupEntRepo(t)

	start := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	task := &usage.UsageCleanupTask{
		Status:    usage.UsageCleanupStatusPending,
		Filters:   usage.UsageCleanupFilters{StartTime: start, EndTime: end},
		CreatedBy: 9,
	}
	require.NoError(t, repo.CreateTask(context.Background(), task))
	require.NotZero(t, task.ID)

	task2 := &usage.UsageCleanupTask{
		Status:    usage.UsageCleanupStatusRunning,
		Filters:   usage.UsageCleanupFilters{StartTime: start.Add(-24 * time.Hour), EndTime: end.Add(-24 * time.Hour)},
		CreatedBy: 10,
	}
	require.NoError(t, repo.CreateTask(context.Background(), task2))

	tasks, result, err := repo.ListTasks(context.Background(), pagination.PaginationParams{Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Len(t, tasks, 2)
	require.Equal(t, int64(2), result.Total)
	require.Greater(t, tasks[0].ID, tasks[1].ID)
	require.Equal(t, start, tasks[1].Filters.StartTime)
	require.Equal(t, end, tasks[1].Filters.EndTime)
}

func TestUsageCleanupRepositoryEntListEmpty(t *testing.T) {
	repo, _ := newUsageCleanupEntRepo(t)

	tasks, result, err := repo.ListTasks(context.Background(), pagination.PaginationParams{Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Empty(t, tasks)
	require.Equal(t, int64(0), result.Total)
}

func TestUsageCleanupRepositoryEntGetStatusAndProgress(t *testing.T) {
	repo, client := newUsageCleanupEntRepo(t)

	task := &usage.UsageCleanupTask{
		Status:    usage.UsageCleanupStatusPending,
		Filters:   usage.UsageCleanupFilters{StartTime: time.Now().UTC(), EndTime: time.Now().UTC().Add(time.Hour)},
		CreatedBy: 3,
	}
	require.NoError(t, repo.CreateTask(context.Background(), task))

	status, err := repo.GetTaskStatus(context.Background(), task.ID)
	require.NoError(t, err)
	require.Equal(t, usage.UsageCleanupStatusPending, status)

	_, err = repo.GetTaskStatus(context.Background(), task.ID+99)
	require.ErrorIs(t, err, sql.ErrNoRows)
	require.ErrorIs(t, err, usage.ErrCleanupTaskNotFound)

	require.NoError(t, repo.UpdateTaskProgress(context.Background(), task.ID, 42))
	loaded, err := client.UsageCleanupTask.Get(context.Background(), task.ID)
	require.NoError(t, err)
	require.Equal(t, int64(42), loaded.DeletedRows)
}

func TestUsageCleanupRepositoryEntCancelAndFinish(t *testing.T) {
	repo, client := newUsageCleanupEntRepo(t)

	task := &usage.UsageCleanupTask{
		Status:    usage.UsageCleanupStatusPending,
		Filters:   usage.UsageCleanupFilters{StartTime: time.Now().UTC(), EndTime: time.Now().UTC().Add(time.Hour)},
		CreatedBy: 5,
	}
	require.NoError(t, repo.CreateTask(context.Background(), task))

	ok, err := repo.CancelTask(context.Background(), task.ID, 7)
	require.NoError(t, err)
	require.True(t, ok)

	loaded, err := client.UsageCleanupTask.Get(context.Background(), task.ID)
	require.NoError(t, err)
	require.Equal(t, usage.UsageCleanupStatusCanceled, loaded.Status)
	require.NotNil(t, loaded.CanceledBy)
	require.NotNil(t, loaded.CanceledAt)
	require.NotNil(t, loaded.FinishedAt)

	loaded.Status = usage.UsageCleanupStatusSucceeded
	_, err = client.UsageCleanupTask.Update().Where(dbusagecleanuptask.IDEQ(task.ID)).SetStatus(loaded.Status).Save(context.Background())
	require.NoError(t, err)

	ok, err = repo.CancelTask(context.Background(), task.ID, 7)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestUsageCleanupRepositoryEntCancelError(t *testing.T) {
	repo, client := newUsageCleanupEntRepo(t)

	task := &usage.UsageCleanupTask{
		Status:    usage.UsageCleanupStatusPending,
		Filters:   usage.UsageCleanupFilters{StartTime: time.Now().UTC(), EndTime: time.Now().UTC().Add(time.Hour)},
		CreatedBy: 5,
	}
	require.NoError(t, repo.CreateTask(context.Background(), task))

	require.NoError(t, client.Close())
	_, err := repo.CancelTask(context.Background(), task.ID, 7)
	require.Error(t, err)
}

func TestUsageCleanupRepositoryEntMarkResults(t *testing.T) {
	repo, client := newUsageCleanupEntRepo(t)

	task := &usage.UsageCleanupTask{
		Status:    usage.UsageCleanupStatusRunning,
		Filters:   usage.UsageCleanupFilters{StartTime: time.Now().UTC(), EndTime: time.Now().UTC().Add(time.Hour)},
		CreatedBy: 12,
	}
	require.NoError(t, repo.CreateTask(context.Background(), task))

	require.NoError(t, repo.MarkTaskSucceeded(context.Background(), task.ID, 6))
	loaded, err := client.UsageCleanupTask.Get(context.Background(), task.ID)
	require.NoError(t, err)
	require.Equal(t, usage.UsageCleanupStatusSucceeded, loaded.Status)
	require.Equal(t, int64(6), loaded.DeletedRows)
	require.NotNil(t, loaded.FinishedAt)

	task2 := &usage.UsageCleanupTask{
		Status:    usage.UsageCleanupStatusRunning,
		Filters:   usage.UsageCleanupFilters{StartTime: time.Now().UTC(), EndTime: time.Now().UTC().Add(time.Hour)},
		CreatedBy: 12,
	}
	require.NoError(t, repo.CreateTask(context.Background(), task2))

	require.NoError(t, repo.MarkTaskFailed(context.Background(), task2.ID, 4, "boom"))
	loaded2, err := client.UsageCleanupTask.Get(context.Background(), task2.ID)
	require.NoError(t, err)
	require.Equal(t, usage.UsageCleanupStatusFailed, loaded2.Status)
	require.Equal(t, "boom", *loaded2.ErrorMessage)
}

func TestUsageCleanupRepositoryEntInvalidStatus(t *testing.T) {
	repo, _ := newUsageCleanupEntRepo(t)

	task := &usage.UsageCleanupTask{
		Status:    "invalid",
		Filters:   usage.UsageCleanupFilters{StartTime: time.Now().UTC(), EndTime: time.Now().UTC().Add(time.Hour)},
		CreatedBy: 1,
	}
	require.Error(t, repo.CreateTask(context.Background(), task))
}

func TestUsageCleanupRepositoryEntListInvalidFilters(t *testing.T) {
	repo, client := newUsageCleanupEntRepo(t)

	now := time.Now().UTC()
	driver, ok := client.Driver().(*entsql.Driver)
	require.True(t, ok)
	_, err := driver.DB().ExecContext(
		context.Background(),
		`INSERT INTO usage_cleanup_tasks (status, filters, created_by, deleted_rows, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		usage.UsageCleanupStatusPending,
		[]byte("invalid-json"),
		int64(1),
		int64(0),
		now,
		now,
	)
	require.NoError(t, err)

	_, _, err = repo.ListTasks(context.Background(), pagination.PaginationParams{Page: 1, PageSize: 10})
	require.Error(t, err)
}

func TestUsageCleanupTaskFromEntFull(t *testing.T) {
	start := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	errMsg := "failed"
	canceledBy := int64(2)
	canceledAt := start.Add(time.Minute)
	startedAt := start.Add(2 * time.Minute)
	finishedAt := start.Add(3 * time.Minute)
	filters := usage.UsageCleanupFilters{StartTime: start, EndTime: end}
	filtersJSON, err := json.Marshal(filters)
	require.NoError(t, err)

	task, err := usageCleanupTaskFromEnt(&dbent.UsageCleanupTask{
		ID:           10,
		Status:       usage.UsageCleanupStatusFailed,
		Filters:      filtersJSON,
		CreatedBy:    11,
		DeletedRows:  7,
		ErrorMessage: &errMsg,
		CanceledBy:   &canceledBy,
		CanceledAt:   &canceledAt,
		StartedAt:    &startedAt,
		FinishedAt:   &finishedAt,
		CreatedAt:    start,
		UpdatedAt:    end,
	})
	require.NoError(t, err)
	require.Equal(t, int64(10), task.ID)
	require.Equal(t, usage.UsageCleanupStatusFailed, task.Status)
	require.NotNil(t, task.ErrorMsg)
	require.NotNil(t, task.CanceledBy)
	require.NotNil(t, task.CanceledAt)
	require.NotNil(t, task.StartedAt)
	require.NotNil(t, task.FinishedAt)
}

func TestUsageCleanupTaskFromEntInvalidFilters(t *testing.T) {
	task, err := usageCleanupTaskFromEnt(&dbent.UsageCleanupTask{
		Filters: json.RawMessage("invalid-json"),
	})
	require.Error(t, err)
	require.Empty(t, task)
}

func TestNewUsageCleanupRepository(t *testing.T) {
	db, _ := newSQLMock(t)
	repo := NewUsageCleanupRepository(nil, db)
	require.NotNil(t, repo)
}

func TestUsageCleanupRepositoryCreateTask(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &CleanupStore{sql: db}

	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	task := &usage.UsageCleanupTask{
		Status:    usage.UsageCleanupStatusPending,
		Filters:   usage.UsageCleanupFilters{StartTime: start, EndTime: end},
		CreatedBy: 12,
	}
	now := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery("INSERT INTO usage_cleanup_tasks").
		WithArgs(task.Status, sqlmock.AnyArg(), task.CreatedBy, task.DeletedRows).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).AddRow(int64(1), now, now))

	err := repo.CreateTask(context.Background(), task)
	require.NoError(t, err)
	require.Equal(t, int64(1), task.ID)
	require.Equal(t, now, task.CreatedAt)
	require.Equal(t, now, task.UpdatedAt)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageCleanupRepositoryCreateTaskNil(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &CleanupStore{sql: db}

	err := repo.CreateTask(context.Background(), nil)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageCleanupRepositoryCreateTaskQueryError(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &CleanupStore{sql: db}

	task := &usage.UsageCleanupTask{
		Status:    usage.UsageCleanupStatusPending,
		Filters:   usage.UsageCleanupFilters{StartTime: time.Now(), EndTime: time.Now().Add(time.Hour)},
		CreatedBy: 1,
	}

	mock.ExpectQuery("INSERT INTO usage_cleanup_tasks").
		WithArgs(task.Status, sqlmock.AnyArg(), task.CreatedBy, task.DeletedRows).
		WillReturnError(sql.ErrConnDone)

	err := repo.CreateTask(context.Background(), task)
	require.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageCleanupRepositoryListTasksEmpty(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &CleanupStore{sql: db}

	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM usage_cleanup_tasks").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(0)))

	tasks, result, err := repo.ListTasks(context.Background(), pagination.PaginationParams{Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Empty(t, tasks)
	require.Equal(t, int64(0), result.Total)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageCleanupRepositoryListTasks(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &CleanupStore{sql: db}

	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)
	filters := usage.UsageCleanupFilters{StartTime: start, EndTime: end}
	filtersJSON, err := json.Marshal(filters)
	require.NoError(t, err)

	createdAt := time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Minute)
	rows := sqlmock.NewRows([]string{
		"id", "status", "filters", "created_by", "deleted_rows", "error_message",
		"canceled_by", "canceled_at",
		"started_at", "finished_at", "created_at", "updated_at",
	}).AddRow(
		int64(1),
		usage.UsageCleanupStatusSucceeded,
		filtersJSON,
		int64(2),
		int64(9),
		"error",
		nil,
		nil,
		start,
		end,
		createdAt,
		updatedAt,
	)

	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM usage_cleanup_tasks").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(1)))
	mock.ExpectQuery("SELECT id, status, filters, created_by, deleted_rows, error_message").
		WithArgs(20, 0).
		WillReturnRows(rows)

	tasks, result, err := repo.ListTasks(context.Background(), pagination.PaginationParams{Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	require.Equal(t, int64(1), tasks[0].ID)
	require.Equal(t, usage.UsageCleanupStatusSucceeded, tasks[0].Status)
	require.Equal(t, int64(2), tasks[0].CreatedBy)
	require.Equal(t, int64(9), tasks[0].DeletedRows)
	require.NotNil(t, tasks[0].ErrorMsg)
	require.Equal(t, "error", *tasks[0].ErrorMsg)
	require.NotNil(t, tasks[0].StartedAt)
	require.NotNil(t, tasks[0].FinishedAt)
	require.Equal(t, int64(1), result.Total)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageCleanupRepositoryListTasksQueryError(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &CleanupStore{sql: db}

	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM usage_cleanup_tasks").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(2)))
	mock.ExpectQuery("SELECT id, status, filters, created_by, deleted_rows, error_message").
		WithArgs(20, 0).
		WillReturnError(sql.ErrConnDone)

	_, _, err := repo.ListTasks(context.Background(), pagination.PaginationParams{Page: 1, PageSize: 20})
	require.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageCleanupRepositoryListTasksInvalidFilters(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &CleanupStore{sql: db}

	rows := sqlmock.NewRows([]string{
		"id", "status", "filters", "created_by", "deleted_rows", "error_message",
		"canceled_by", "canceled_at",
		"started_at", "finished_at", "created_at", "updated_at",
	}).AddRow(
		int64(1),
		usage.UsageCleanupStatusSucceeded,
		[]byte("not-json"),
		int64(2),
		int64(9),
		nil,
		nil,
		nil,
		nil,
		nil,
		time.Now().UTC(),
		time.Now().UTC(),
	)

	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM usage_cleanup_tasks").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(1)))
	mock.ExpectQuery("SELECT id, status, filters, created_by, deleted_rows, error_message").
		WithArgs(20, 0).
		WillReturnRows(rows)

	_, _, err := repo.ListTasks(context.Background(), pagination.PaginationParams{Page: 1, PageSize: 20})
	require.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageCleanupRepositoryClaimNextPendingTaskNone(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &CleanupStore{sql: db}

	mock.ExpectQuery("UPDATE usage_cleanup_tasks").
		WithArgs(usage.UsageCleanupStatusPending, usage.UsageCleanupStatusRunning, int64(1800), usage.UsageCleanupStatusRunning).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "status", "filters", "created_by", "deleted_rows", "error_message",
			"started_at", "finished_at", "created_at", "updated_at",
		}))

	task, err := repo.ClaimNextPendingTask(context.Background(), 1800)
	require.NoError(t, err)
	require.Nil(t, task)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageCleanupRepositoryClaimNextPendingTask(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &CleanupStore{sql: db}

	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	filters := usage.UsageCleanupFilters{StartTime: start, EndTime: end}
	filtersJSON, err := json.Marshal(filters)
	require.NoError(t, err)

	rows := sqlmock.NewRows([]string{
		"id", "status", "filters", "created_by", "deleted_rows", "error_message",
		"started_at", "finished_at", "created_at", "updated_at",
	}).AddRow(
		int64(4),
		usage.UsageCleanupStatusRunning,
		filtersJSON,
		int64(7),
		int64(0),
		nil,
		start,
		nil,
		start,
		start,
	)

	mock.ExpectQuery("UPDATE usage_cleanup_tasks").
		WithArgs(usage.UsageCleanupStatusPending, usage.UsageCleanupStatusRunning, int64(1800), usage.UsageCleanupStatusRunning).
		WillReturnRows(rows)

	task, err := repo.ClaimNextPendingTask(context.Background(), 1800)
	require.NoError(t, err)
	require.NotNil(t, task)
	require.Equal(t, int64(4), task.ID)
	require.Equal(t, usage.UsageCleanupStatusRunning, task.Status)
	require.Equal(t, int64(7), task.CreatedBy)
	require.NotNil(t, task.StartedAt)
	require.Nil(t, task.ErrorMsg)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageCleanupRepositoryClaimNextPendingTaskError(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &CleanupStore{sql: db}

	mock.ExpectQuery("UPDATE usage_cleanup_tasks").
		WithArgs(usage.UsageCleanupStatusPending, usage.UsageCleanupStatusRunning, int64(1800), usage.UsageCleanupStatusRunning).
		WillReturnError(sql.ErrConnDone)

	_, err := repo.ClaimNextPendingTask(context.Background(), 1800)
	require.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageCleanupRepositoryClaimNextPendingTaskInvalidFilters(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &CleanupStore{sql: db}

	rows := sqlmock.NewRows([]string{
		"id", "status", "filters", "created_by", "deleted_rows", "error_message",
		"started_at", "finished_at", "created_at", "updated_at",
	}).AddRow(
		int64(4),
		usage.UsageCleanupStatusRunning,
		[]byte("invalid"),
		int64(7),
		int64(0),
		nil,
		nil,
		nil,
		time.Now().UTC(),
		time.Now().UTC(),
	)

	mock.ExpectQuery("UPDATE usage_cleanup_tasks").
		WithArgs(usage.UsageCleanupStatusPending, usage.UsageCleanupStatusRunning, int64(1800), usage.UsageCleanupStatusRunning).
		WillReturnRows(rows)

	_, err := repo.ClaimNextPendingTask(context.Background(), 1800)
	require.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageCleanupRepositoryMarkTaskSucceeded(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &CleanupStore{sql: db}

	mock.ExpectExec("UPDATE usage_cleanup_tasks").
		WithArgs(usage.UsageCleanupStatusSucceeded, int64(12), int64(9)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.MarkTaskSucceeded(context.Background(), 9, 12)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageCleanupRepositoryMarkTaskFailed(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &CleanupStore{sql: db}

	mock.ExpectExec("UPDATE usage_cleanup_tasks").
		WithArgs(usage.UsageCleanupStatusFailed, int64(4), "boom", int64(2)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.MarkTaskFailed(context.Background(), 2, 4, "boom")
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageCleanupRepositoryGetTaskStatus(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &CleanupStore{sql: db}

	mock.ExpectQuery("SELECT status FROM usage_cleanup_tasks").
		WithArgs(int64(9)).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow(usage.UsageCleanupStatusPending))

	status, err := repo.GetTaskStatus(context.Background(), 9)
	require.NoError(t, err)
	require.Equal(t, usage.UsageCleanupStatusPending, status)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageCleanupRepositoryGetTaskStatusQueryError(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &CleanupStore{sql: db}

	mock.ExpectQuery("SELECT status FROM usage_cleanup_tasks").
		WithArgs(int64(9)).
		WillReturnError(sql.ErrConnDone)

	_, err := repo.GetTaskStatus(context.Background(), 9)
	require.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageCleanupRepositoryUpdateTaskProgress(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &CleanupStore{sql: db}

	mock.ExpectExec("UPDATE usage_cleanup_tasks").
		WithArgs(int64(123), int64(8)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.UpdateTaskProgress(context.Background(), 8, 123)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageCleanupRepositoryCancelTask(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &CleanupStore{sql: db}

	mock.ExpectQuery("UPDATE usage_cleanup_tasks").
		WithArgs(usage.UsageCleanupStatusCanceled, int64(6), int64(9), usage.UsageCleanupStatusPending, usage.UsageCleanupStatusRunning).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(6)))

	ok, err := repo.CancelTask(context.Background(), 6, 9)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageCleanupRepositoryCancelTaskNoRows(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &CleanupStore{sql: db}

	mock.ExpectQuery("UPDATE usage_cleanup_tasks").
		WithArgs(usage.UsageCleanupStatusCanceled, int64(6), int64(9), usage.UsageCleanupStatusPending, usage.UsageCleanupStatusRunning).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	ok, err := repo.CancelTask(context.Background(), 6, 9)
	require.NoError(t, err)
	require.False(t, ok)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageCleanupRepositoryDeleteUsageLogsBatchMissingRange(t *testing.T) {
	db, _ := newSQLMock(t)
	repo := &CleanupStore{sql: db}

	_, err := repo.DeleteUsageLogsBatch(context.Background(), usage.UsageCleanupFilters{}, 10)
	require.Error(t, err)
}

func TestUsageCleanupRepositoryDeleteUsageLogsBatch(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &CleanupStore{sql: db}

	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	userID := int64(3)
	model := " gpt-4 "
	filters := usage.UsageCleanupFilters{
		StartTime: start,
		EndTime:   end,
		UserID:    &userID,
		Model:     &model,
	}

	mock.ExpectQuery("DELETE FROM usage_logs").
		WithArgs(start, end, userID, "gpt-4", 2).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(1)).AddRow(int64(2)))

	deleted, err := repo.DeleteUsageLogsBatch(context.Background(), filters, 2)
	require.NoError(t, err)
	require.Equal(t, int64(2), deleted)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageCleanupRepositoryDeleteUsageLogsBatchQueryError(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &CleanupStore{sql: db}

	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	filters := usage.UsageCleanupFilters{StartTime: start, EndTime: end}

	mock.ExpectQuery("DELETE FROM usage_logs").
		WithArgs(start, end, 5).
		WillReturnError(sql.ErrConnDone)

	_, err := repo.DeleteUsageLogsBatch(context.Background(), filters, 5)
	require.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBuildUsageCleanupWhere(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	userID := int64(1)
	apiKeyID := int64(2)
	providerID := int64(3)
	groupID := int64(4)
	teamID := int64(5)
	model := " gpt-4 "
	stream := true
	billingType := int8(2)

	where, args := buildUsageCleanupWhere(usage.UsageCleanupFilters{
		StartTime:   start,
		EndTime:     end,
		UserID:      &userID,
		APIKeyID:    &apiKeyID,
		ProviderID:  &providerID,
		GroupID:     &groupID,
		TeamID:      &teamID,
		Model:       &model,
		Stream:      &stream,
		BillingType: &billingType,
	})

	require.Equal(t, "created_at >= $1 AND created_at <= $2 AND user_id = $3 AND api_key_id = $4 AND provider_id = $5 AND group_id = $6 AND team_id = $7 AND model = $8 AND stream = $9 AND billing_type = $10", where)
	require.Equal(t, []any{start, end, userID, apiKeyID, providerID, groupID, teamID, "gpt-4", stream, billingType}, args)
}

func TestBuildUsageCleanupWhereRequestTypePriority(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	requestType := int16(usage.RequestTypeWSV2)
	stream := false

	where, args := buildUsageCleanupWhere(usage.UsageCleanupFilters{
		StartTime:   start,
		EndTime:     end,
		RequestType: &requestType,
		Stream:      &stream,
	})

	require.Equal(t, "created_at >= $1 AND created_at <= $2 AND (request_type = $3 OR (request_type = 0 AND openai_ws_mode = TRUE))", where)
	require.Equal(t, []any{start, end, requestType}, args)
}

func TestBuildUsageCleanupWhereRequestTypeLegacyFallback(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	requestType := int16(usage.RequestTypeStream)

	where, args := buildUsageCleanupWhere(usage.UsageCleanupFilters{
		StartTime:   start,
		EndTime:     end,
		RequestType: &requestType,
	})

	require.Equal(t, "created_at >= $1 AND created_at <= $2 AND (request_type = $3 OR (request_type = 0 AND stream = TRUE AND openai_ws_mode = FALSE))", where)
	require.Equal(t, []any{start, end, requestType}, args)
}

func TestBuildUsageCleanupWhereModelEmpty(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	model := "   "

	where, args := buildUsageCleanupWhere(usage.UsageCleanupFilters{
		StartTime: start,
		EndTime:   end,
		Model:     &model,
	})

	require.Equal(t, "created_at >= $1 AND created_at <= $2", where)
	require.Equal(t, []any{start, end}, args)
}
