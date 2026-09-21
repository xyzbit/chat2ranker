// Package postgres persists execution state and River jobs in one transaction.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/execution/backend/internal/domain"
	"github.com/xyzbit/agentkiln/execution/backend/internal/jobs"
	"github.com/xyzbit/agentkiln/execution/backend/pgtimeout"
)

const schemaVersion = 7

// Config controls persistent task admission and delivery retry bounds.
type Config struct {
	OperationTimeout    time.Duration
	URL                 string
	Schema              string
	MaxAttempts         int
	MaxQueued           int
	MaxModelConcurrent  int
	MaxTenantConcurrent int
}

// Store owns its pool and an insert-only River client. A job-scoped copy shares
// the pool and adds transactional acknowledgement to terminal transitions.
type Store struct {
	pool   *pgxpool.Pool
	queue  *river.Client[pgx.Tx]
	config Config
	job    *river.Job[jobs.Execution]
}

// Open connects without changing schema or starting workers.
func Open(ctx context.Context, config Config) (*Store, error) {
	if !regexp.MustCompile(`^[a-z][a-z0-9_]*$`).MatchString(config.Schema) || config.MaxAttempts < 1 || config.MaxQueued < 1 || config.MaxModelConcurrent < 0 || config.MaxTenantConcurrent < 0 || config.OperationTimeout <= 0 {
		return nil, errors.New("execution postgres requires a lowercase schema, positive operation timeout, max attempts and queue limit")
	}
	connection, err := pgxpool.ParseConfig(config.URL)
	if err != nil {
		return nil, errors.New("invalid execution database URL")
	}
	connection.ConnConfig.RuntimeParams["search_path"] = config.Schema
	if err := pgtimeout.Configure(connection, config.OperationTimeout); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, config.OperationTimeout)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, connection)
	if err != nil {
		return nil, err
	}
	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{Schema: config.Schema})
	if err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{pool: pool, queue: client, config: config}, nil
}

func (s *Store) Pool() *pgxpool.Pool { return s.pool }
func (s *Store) Schema() string      { return s.config.Schema }
func (s *Store) Close() error        { s.pool.Close(); return nil }

// ForJob returns a per-delivery repository without mutating the shared store.
func (s *Store) ForJob(job *river.Job[jobs.Execution]) *Store {
	copy := *s
	copy.job = job
	return &copy
}

// Migrate serializes schema upgrades on a dedicated connection. River migration
// versions commit separately because PostgreSQL enum additions require it.
func (s *Store) Migrate(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, s.config.OperationTimeout)
	defer cancel()
	connection, err := s.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer connection.Release()
	if _, err = connection.Exec(ctx, `SELECT pg_advisory_lock(hashtextextended($1,0))`, s.config.Schema+"/migration"); err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if _, err := connection.Exec(cleanup, `SELECT pg_advisory_unlock(hashtextextended($1,0))`, s.config.Schema+"/migration"); err != nil {
			_ = connection.Conn().Close(cleanup)
		}
	}()
	tx, err := connection.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+pgx.Identifier{s.config.Schema}.Sanitize()); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `
CREATE TABLE IF NOT EXISTS execution_schema(version integer PRIMARY KEY);
CREATE TABLE IF NOT EXISTS executions(
 id text PRIMARY KEY, idempotency_key text NOT NULL UNIQUE,
 body jsonb NOT NULL, last_sequence bigint NOT NULL DEFAULT 0, job_id bigint
);
CREATE INDEX IF NOT EXISTS execution_status_idx ON executions((body->>'status'));
CREATE TABLE IF NOT EXISTS execution_events(
 execution_id text NOT NULL REFERENCES executions(id), sequence bigint NOT NULL,
 body jsonb NOT NULL, PRIMARY KEY(execution_id,sequence)
);
CREATE TABLE IF NOT EXISTS model_connections(id text PRIMARY KEY, body jsonb NOT NULL, credential_ref text NOT NULL);
CREATE TABLE IF NOT EXISTS system_model_bindings(
 role text PRIMARY KEY, connection_id text NOT NULL REFERENCES model_connections(id), body jsonb NOT NULL
);
CREATE TABLE IF NOT EXISTS runtime_pools(id text PRIMARY KEY, endpoint text NOT NULL UNIQUE, body jsonb NOT NULL);
CREATE TABLE IF NOT EXISTS credentials(ref text PRIMARY KEY, ciphertext bytea NOT NULL);
CREATE TABLE IF NOT EXISTS model_quotas(id text PRIMARY KEY, body jsonb NOT NULL);
CREATE TABLE IF NOT EXISTS model_quota_connections(connection_id text PRIMARY KEY REFERENCES model_connections(id), group_id text NOT NULL REFERENCES model_quotas(id));
CREATE TABLE IF NOT EXISTS model_permits(
 execution_id text NOT NULL REFERENCES executions(id), attempt integer NOT NULL, sequence integer NOT NULL,
 group_id text NOT NULL REFERENCES model_quotas(id), input_tokens bigint NOT NULL, tokens bigint NOT NULL,
 valid_until_ms bigint NOT NULL, expires_at_ms bigint NOT NULL, actual_tokens bigint,
 PRIMARY KEY(execution_id,attempt,sequence)
);
CREATE INDEX IF NOT EXISTS model_permits_group_expiry ON model_permits(group_id,expires_at_ms);
`); err != nil {
		return err
	}
	var version int
	err = tx.QueryRow(ctx, `SELECT version FROM execution_schema`).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	} else if err == nil && (version < 1 || version > schemaVersion) {
		return fmt.Errorf("execution schema %d is unsupported; expected %d", version, schemaVersion)
	}
	if err != nil {
		return err
	}
	if version == 1 {
		if _, err = tx.Exec(ctx, `UPDATE executions SET body=body || '{"runtimeCleanupPending":true}'::jsonb
 WHERE body->>'status' IN ('running','cancelling')
 OR (body->>'status'='failed' AND (body->>'error' LIKE '%execution_outcome_unknown%' OR body->>'error' LIKE '%runtime stop could not be confirmed%'))`); err != nil {
			return err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	migrator, err := rivermigrate.New(riverpgxv5.New(s.pool), &rivermigrate.Config{Schema: s.config.Schema})
	if err != nil {
		return err
	}
	if _, err = migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return err
	}
	_, err = connection.Exec(ctx, `WITH existing AS (UPDATE execution_schema SET version=$1 RETURNING version) INSERT INTO execution_schema(version) SELECT $1 WHERE NOT EXISTS(SELECT 1 FROM existing)`, schemaVersion)
	return err
}

// CheckSchema fails startup until an explicit migration has succeeded.
func (s *Store) CheckSchema(ctx context.Context) error {
	var version int
	if err := s.pool.QueryRow(ctx, `SELECT version FROM execution_schema`).Scan(&version); err != nil {
		return err
	}
	if version != schemaVersion {
		return fmt.Errorf("execution schema %d is unsupported; expected %d", version, schemaVersion)
	}
	migrator, err := rivermigrate.New(riverpgxv5.New(s.pool), &rivermigrate.Config{Schema: s.config.Schema})
	if err != nil {
		return err
	}
	existing, err := migrator.ExistingVersions(ctx)
	if err != nil {
		return err
	}
	known := migrator.AllVersions()
	if len(existing) != len(known) {
		return errors.New("River migrations do not match the running release")
	}
	return nil
}

func (s *Store) Create(ctx context.Context, value contract.Execution, event contract.Event) error {
	ctx, cancel := context.WithTimeout(ctx, s.config.OperationTimeout)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, s.config.Schema+"/admission"); err != nil {
		return err
	}
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM executions WHERE idempotency_key=$1)`, value.IdempotencyKey).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return domain.ErrConflict
	}
	if err = validateModelQuotaPlan(ctx, tx, value); err != nil {
		return err
	}
	if value.Executor == "opensandbox" {
		pool, poolErr := runtimePool(ctx, tx, value)
		if poolErr != nil {
			return poolErr
		}
		if !domain.FitsRuntime(pool.Capacity, contract.RuntimeResources{}, value.RuntimePlan.Resources) {
			return domain.ErrRuntimePlan
		}
	} else if value.RuntimePlan != nil {
		return domain.ErrRuntimePlan
	}
	var queued int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM executions WHERE body->>'status'=$1`, contract.StatusQueued).Scan(&queued); err != nil {
		return err
	}
	if queued >= s.config.MaxQueued {
		return domain.ErrOverloaded
	}
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO executions(id,idempotency_key,body) VALUES($1,$2,$3)`, value.ID, value.IdempotencyKey, body); err != nil {
		return mapError(err)
	}
	job, err := s.queue.InsertTx(ctx, tx, jobs.Execution{ExecutionID: value.ID}, &river.InsertOpts{Queue: jobs.Queue(value.Spec), MaxAttempts: s.config.MaxAttempts})
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE executions SET job_id=$2 WHERE id=$1`, value.ID, job.Job.ID); err != nil {
		return err
	}
	if err = appendEvent(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) Get(ctx context.Context, id string) (contract.Execution, error) {
	return scanExecution(s.pool.QueryRow(ctx, `SELECT body,idempotency_key FROM executions WHERE id=$1`, id))
}
func (s *Store) GetByIdempotencyKey(ctx context.Context, key string) (contract.Execution, error) {
	return scanExecution(s.pool.QueryRow(ctx, `SELECT body,idempotency_key FROM executions WHERE idempotency_key=$1`, key))
}
func (s *Store) ListActive(ctx context.Context) ([]contract.Execution, error) {
	rows, err := s.pool.Query(ctx, `SELECT body,idempotency_key FROM executions WHERE body->>'status' = ANY($1) ORDER BY body->>'createdAt',id`, []string{contract.StatusQueued, contract.StatusRunning, contract.StatusCancelling})
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []contract.Execution{}
	for rows.Next() {
		value, err := scanExecution(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}
func (s *Store) ListEvents(ctx context.Context, id string, after int64) ([]contract.Event, error) {
	rows, err := s.pool.Query(ctx, `SELECT body FROM execution_events WHERE execution_id=$1 AND sequence>$2 ORDER BY sequence`, id, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []contract.Event{}
	for rows.Next() {
		var value contract.Event
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) AppendEvent(ctx context.Context, event contract.Event) error {
	return s.transition(ctx, event, func(value *contract.Execution) error {
		if value.Status != contract.StatusRunning || value.Attempt != event.Attempt {
			return domain.ErrConflict
		}
		return nil
	})
}
func (s *Store) MarkRunning(ctx context.Context, id string, attempt int, executor, handle, workerVersion string, at time.Time, event contract.Event) error {
	return s.transition(ctx, event, func(value *contract.Execution) error {
		if value.ID != id || value.Status != contract.StatusQueued || attempt != value.Attempt+1 {
			return domain.ErrConflict
		}
		value.RuntimeCleanupPending = true
		value.WorkerVersion = workerVersion
		value.Status, value.Attempt, value.Executor, value.ExternalHandle, value.StartedAt = contract.StatusRunning, attempt, executor, handle, &at
		value.CompletedAt, value.Error = nil, ""
		return nil
	})
}
func (s *Store) RecordEvidence(ctx context.Context, id string, attempt int, result contract.Result) error {
	body, err := json.Marshal(result)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `UPDATE executions SET body=jsonb_set(body,'{result}',$3::jsonb)
 WHERE id=$1 AND (body->>'attempt')::integer=$2 AND $2>0
 AND body->>'status' IN ('running','cancelling')`, id, attempt, body)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrConflict
	}
	return nil
}

func (s *Store) Complete(ctx context.Context, id string, result contract.Result, at time.Time, event contract.Event) error {
	return s.transition(ctx, event, func(value *contract.Execution) error {
		if value.ID != id || value.Status != contract.StatusRunning || value.Attempt != event.Attempt || value.RuntimeCleanupPending {
			return domain.ErrConflict
		}
		value.Status, value.Result, value.CompletedAt, value.Error = contract.StatusCompleted, &result, &at, ""
		return nil
	})
}
func (s *Store) Fail(ctx context.Context, id, message string, at time.Time, event contract.Event) error {
	return s.transition(ctx, event, func(value *contract.Execution) error {
		if value.ID != id || contract.IsTerminal(value.Status) || value.Attempt != event.Attempt {
			return domain.ErrConflict
		}
		value.Status, value.CompletedAt, value.Error = contract.StatusFailed, &at, message
		return nil
	})
}
func (s *Store) Cancel(ctx context.Context, id string, at time.Time, event contract.Event) error {
	return s.transition(ctx, event, func(value *contract.Execution) error {
		if value.ID != id || contract.IsTerminal(value.Status) || value.Attempt != event.Attempt || value.RuntimeCleanupPending {
			return domain.ErrConflict
		}
		value.Status, value.CompletedAt = contract.StatusCancelled, &at
		return nil
	})
}

func (s *Store) RequestCancel(ctx context.Context, id string, at time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, s.config.OperationTimeout)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	value, err := scanExecution(tx.QueryRow(ctx, `SELECT body,idempotency_key FROM executions WHERE id=$1 FOR UPDATE`, id))
	if err != nil {
		return err
	}
	if contract.IsTerminal(value.Status) || value.Status == contract.StatusCancelling {
		return nil
	}
	value.Status = contract.StatusCancelling
	if value.Attempt == 0 {
		value.Status = contract.StatusCancelled
		value.CompletedAt = &at
	}
	event := contract.Event{ExecutionID: id, Attempt: value.Attempt, Status: value.Status, Type: "execution." + value.Status, At: at}
	if err = writeExecution(ctx, tx, value, event); err != nil {
		return err
	}
	var jobID int64
	if err = tx.QueryRow(ctx, `SELECT job_id FROM executions WHERE id=$1`, id).Scan(&jobID); err != nil {
		return err
	}
	if _, err = s.queue.JobCancelTx(ctx, tx, jobID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) transition(ctx context.Context, event contract.Event, change func(*contract.Execution) error) error {
	ctx, cancel := context.WithTimeout(ctx, s.config.OperationTimeout)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if event.Type == "execution.running" {
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, s.config.Schema+"/runtime-admission"); err != nil {
			return err
		}
	}
	value, err := scanExecution(tx.QueryRow(ctx, `SELECT body,idempotency_key FROM executions WHERE id=$1 FOR UPDATE`, event.ExecutionID))
	if err != nil {
		return err
	}
	if event.Type == "execution.running" {
		var now time.Time
		if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			return err
		}
		if value.Status == contract.StatusQueued && (value.QueueExpiresAt.IsZero() || !now.Before(value.QueueExpiresAt) || !event.At.Before(value.QueueExpiresAt)) {
			return domain.ErrQueueExpired
		}
		if err = s.reserveRuntime(ctx, tx, value); err != nil {
			return err
		}
		if err = checkModelRate(ctx, tx, value); err != nil {
			return err
		}
		if err = s.reserveQuota(ctx, tx, value.Spec); err != nil {
			return err
		}
	}
	if err = change(&value); err != nil {
		return err
	}
	if err = writeExecution(ctx, tx, value, event); err != nil {
		return err
	}
	if contract.IsTerminal(value.Status) && s.job != nil {
		if s.job.Args.ExecutionID != value.ID {
			return errors.New("job and execution identity differ")
		}
		if _, err = river.JobCompleteTx[*riverpgxv5.Driver](ctx, tx, s.job); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func writeExecution(ctx context.Context, tx pgx.Tx, value contract.Execution, event contract.Event) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE executions SET body=$2 WHERE id=$1`, value.ID, body); err != nil {
		return err
	}
	return appendEvent(ctx, tx, event)
}
func appendEvent(ctx context.Context, tx pgx.Tx, event contract.Event) error {
	if err := tx.QueryRow(ctx, `UPDATE executions SET last_sequence=last_sequence+1 WHERE id=$1 RETURNING last_sequence`, event.ExecutionID).Scan(&event.Sequence); err != nil {
		return mapError(err)
	}
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO execution_events(execution_id,sequence,body) VALUES($1,$2,$3)`, event.ExecutionID, event.Sequence, body)
	return err
}

type scanner interface{ Scan(...any) error }

func scanExecution(row scanner) (contract.Execution, error) {
	var value contract.Execution
	var key string
	err := row.Scan(&value, &key)
	value.IdempotencyKey = key
	return value, mapError(err)
}
func mapError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && (postgresError.Code == "23505" || postgresError.Code == "23503") {
		return domain.ErrConflict
	}
	return err
}

var _ domain.Repository = (*Store)(nil)

// ConfirmRuntimeStopped records executor evidence, including after an unknown
// business outcome. It never acknowledges or changes the associated River job.
func (s *Store) ConfirmRuntimeStopped(ctx context.Context, id string, attempt int) error {
	result, err := s.pool.Exec(ctx, `UPDATE executions
 SET body=jsonb_set(body,'{runtimeCleanupPending}','false'::jsonb)
 WHERE id=$1 AND (body->>'attempt')::integer=$2 AND $2>0
 AND body->>'status'<>'queued'`, id, attempt)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return domain.ErrConflict
	}
	return nil
}
