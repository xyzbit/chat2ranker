package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/xyzbit/chat2ranker/execution/backend/contract"
	"github.com/xyzbit/chat2ranker/execution/backend/internal/domain"
	_ "modernc.org/sqlite"
)

type Store struct{ db *sql.DB }

func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db}
	if _, err = db.ExecContext(ctx, `PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000;`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err = store.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	const schema = `
CREATE TABLE IF NOT EXISTS schema_migrations(
  version INTEGER PRIMARY KEY,
  applied_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS executions(
  id TEXT PRIMARY KEY,
  idempotency_key TEXT NOT NULL UNIQUE,
  status TEXT NOT NULL,
  executor TEXT NOT NULL,
  attempt INTEGER NOT NULL,
  external_handle TEXT NOT NULL,
  worker_version TEXT NOT NULL,
  spec_json TEXT NOT NULL,
  result_json TEXT,
  error TEXT NOT NULL,
  created_at TEXT NOT NULL,
  started_at TEXT,
  completed_at TEXT
);
CREATE INDEX IF NOT EXISTS executions_status_created_idx ON executions(status, created_at);
CREATE TABLE IF NOT EXISTS execution_events(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  execution_id TEXT NOT NULL REFERENCES executions(id) ON DELETE CASCADE,
  sequence INTEGER NOT NULL,
  attempt INTEGER NOT NULL,
  type TEXT NOT NULL,
  status TEXT NOT NULL,
  message TEXT NOT NULL,
  data_json TEXT,
  created_at TEXT NOT NULL,
  UNIQUE(execution_id, sequence)
);
CREATE INDEX IF NOT EXISTS execution_events_execution_idx ON execution_events(execution_id, sequence);
INSERT OR IGNORE INTO schema_migrations(version, applied_at)
VALUES(1, strftime('%Y-%m-%dT%H:%M:%fZ','now'));
`
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("apply execution sqlite schema: %w", err)
	}
	return nil
}

func (s *Store) Create(ctx context.Context, execution contract.Execution, event contract.Event) error {
	spec, err := json.Marshal(execution.Spec)
	if err != nil {
		return err
	}
	transaction, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	_, err = transaction.ExecContext(ctx, `INSERT INTO executions(
id,idempotency_key,status,executor,attempt,external_handle,worker_version,spec_json,error,created_at
) VALUES(?,?,?,?,?,?,?,?,?,?)`, execution.ID, execution.IdempotencyKey, execution.Status, execution.Executor, execution.Attempt, execution.ExternalHandle, execution.WorkerVersion, string(spec), execution.Error, formatTime(execution.CreatedAt))
	if err != nil {
		return mapError(err)
	}
	if err := appendEvent(ctx, transaction, event); err != nil {
		return err
	}
	return transaction.Commit()
}

func (s *Store) Get(ctx context.Context, id string) (contract.Execution, error) {
	return scanExecution(s.db.QueryRowContext(ctx, `SELECT id,idempotency_key,status,executor,attempt,external_handle,worker_version,spec_json,result_json,error,created_at,started_at,completed_at FROM executions WHERE id=?`, id))
}

func (s *Store) GetByIdempotencyKey(ctx context.Context, key string) (contract.Execution, error) {
	return scanExecution(s.db.QueryRowContext(ctx, `SELECT id,idempotency_key,status,executor,attempt,external_handle,worker_version,spec_json,result_json,error,created_at,started_at,completed_at FROM executions WHERE idempotency_key=?`, key))
}

func (s *Store) ListActive(ctx context.Context) ([]contract.Execution, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,idempotency_key,status,executor,attempt,external_handle,worker_version,spec_json,result_json,error,created_at,started_at,completed_at FROM executions WHERE status IN (?,?,?) ORDER BY created_at`, contract.StatusQueued, contract.StatusRunning, "cancelling")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []contract.Execution{}
	for rows.Next() {
		execution, err := scanExecution(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, execution)
	}
	return result, rows.Err()
}

func (s *Store) ListEvents(ctx context.Context, executionID string, afterSequence int64) ([]contract.Event, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT sequence,execution_id,attempt,type,status,message,data_json,created_at FROM execution_events WHERE execution_id=? AND sequence>? ORDER BY sequence`, executionID, afterSequence)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []contract.Event{}
	for rows.Next() {
		var event contract.Event
		var data sql.NullString
		var createdAt string
		if err := rows.Scan(&event.Sequence, &event.ExecutionID, &event.Attempt, &event.Type, &event.Status, &event.Message, &data, &createdAt); err != nil {
			return nil, err
		}
		if data.Valid {
			event.Data = json.RawMessage(data.String)
		}
		event.At, err = time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, err
		}
		result = append(result, event)
	}
	return result, rows.Err()
}

func (s *Store) AppendEvent(ctx context.Context, event contract.Event) error {
	return appendEvent(ctx, s.db, event)
}

func (s *Store) MarkRunning(ctx context.Context, id string, attempt int, executor, externalHandle string, startedAt time.Time, event contract.Event) error {
	return s.transition(ctx, func(transaction *sql.Tx) error {
		result, err := transaction.ExecContext(ctx, `UPDATE executions SET status=?,attempt=?,executor=?,external_handle=?,started_at=?,completed_at=NULL,error='' WHERE id=? AND status=?`, contract.StatusRunning, attempt, executor, externalHandle, formatTime(startedAt), id, contract.StatusQueued)
		return affected(result, err)
	}, event)
}

func (s *Store) Complete(ctx context.Context, id string, value contract.Result, completedAt time.Time, event contract.Event) error {
	resultJSON, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return s.transition(ctx, func(transaction *sql.Tx) error {
		result, err := transaction.ExecContext(ctx, `UPDATE executions SET status=?,result_json=?,completed_at=?,error='' WHERE id=? AND status=?`, contract.StatusCompleted, string(resultJSON), formatTime(completedAt), id, contract.StatusRunning)
		return affected(result, err)
	}, event)
}

func (s *Store) Fail(ctx context.Context, id, message string, completedAt time.Time, event contract.Event) error {
	return s.transition(ctx, func(transaction *sql.Tx) error {
		result, err := transaction.ExecContext(ctx, `UPDATE executions SET status=?,completed_at=?,error=? WHERE id=? AND status IN (?,?)`, contract.StatusFailed, formatTime(completedAt), message, id, contract.StatusQueued, contract.StatusRunning)
		return affected(result, err)
	}, event)
}

func (s *Store) Cancel(ctx context.Context, id string, completedAt time.Time, event contract.Event) error {
	return s.transition(ctx, func(transaction *sql.Tx) error {
		result, err := transaction.ExecContext(ctx, `UPDATE executions SET status=?,completed_at=? WHERE id=? AND status IN (?,?)`, contract.StatusCancelled, formatTime(completedAt), id, contract.StatusQueued, contract.StatusRunning)
		return affected(result, err)
	}, event)
}

func (s *Store) Requeue(ctx context.Context, id string, event contract.Event) error {
	return s.transition(ctx, func(transaction *sql.Tx) error {
		result, err := transaction.ExecContext(ctx, `UPDATE executions SET status=?,external_handle='',started_at=NULL,completed_at=NULL,error='' WHERE id=? AND status IN (?,?)`, contract.StatusQueued, id, contract.StatusQueued, contract.StatusRunning)
		return affected(result, err)
	}, event)
}

type sqlExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func appendEvent(ctx context.Context, executor sqlExecutor, event contract.Event) error {
	var data any
	if len(event.Data) > 0 {
		data = string(event.Data)
	}
	_, err := executor.ExecContext(ctx, `INSERT INTO execution_events(execution_id,sequence,attempt,type,status,message,data_json,created_at)
SELECT ?,COALESCE(MAX(sequence),0)+1,?,?,?,?,?,? FROM execution_events WHERE execution_id=?`, event.ExecutionID, event.Attempt, event.Type, event.Status, event.Message, data, formatTime(event.At), event.ExecutionID)
	return mapError(err)
}

func (s *Store) transition(ctx context.Context, update func(*sql.Tx) error, event contract.Event) error {
	transaction, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	if err := update(transaction); err != nil {
		return err
	}
	if err := appendEvent(ctx, transaction, event); err != nil {
		return err
	}
	return transaction.Commit()
}

type scanner interface{ Scan(...any) error }

func scanExecution(row scanner) (contract.Execution, error) {
	var value contract.Execution
	var specJSON string
	var resultJSON, startedAt, completedAt sql.NullString
	var createdAt string
	err := row.Scan(&value.ID, &value.IdempotencyKey, &value.Status, &value.Executor, &value.Attempt, &value.ExternalHandle, &value.WorkerVersion, &specJSON, &resultJSON, &value.Error, &createdAt, &startedAt, &completedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return value, domain.ErrNotFound
	}
	if err != nil {
		return value, err
	}
	if err = json.Unmarshal([]byte(specJSON), &value.Spec); err != nil {
		return value, err
	}
	if resultJSON.Valid {
		var result contract.Result
		if err = json.Unmarshal([]byte(resultJSON.String), &result); err != nil {
			return value, err
		}
		value.Result = &result
	}
	value.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return value, err
	}
	value.StartedAt = parseNullableTime(startedAt)
	value.CompletedAt = parseNullableTime(completedAt)
	return value, nil
}

func formatTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }

func parseNullableTime(value sql.NullString) *time.Time {
	if !value.Valid {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value.String)
	if err != nil {
		return nil
	}
	return &parsed
}

func mapError(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return domain.ErrConflict
	}
	return err
}

func affected(result sql.Result, err error) error {
	if err != nil {
		return mapError(err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return domain.ErrConflict
	}
	return nil
}

var _ domain.Repository = (*Store)(nil)
