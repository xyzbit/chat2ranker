package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/execution/backend/internal/domain"
)

func (s *Store) ConfigureRuntimePool(ctx context.Context, pool domain.RuntimePool, replace bool) error {
	ctx, cancel := context.WithTimeout(ctx, s.config.OperationTimeout)
	defer cancel()
	if err := pool.Validate(); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, key := range []string{"/admission", "/runtime-admission"} {
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, s.config.Schema+key); err != nil {
			return err
		}
	}
	var body []byte
	err = tx.QueryRow(ctx, `SELECT body FROM runtime_pools WHERE id=$1`, pool.ID).Scan(&body)
	exists := !errors.Is(err, pgx.ErrNoRows)
	if err != nil && exists {
		return err
	}
	var old domain.RuntimePool
	if exists {
		if err = json.Unmarshal(body, &old); err != nil {
			return err
		}
		if old == pool {
			return tx.Commit(ctx)
		}
		if !replace {
			return errors.New("runtime pool configuration differs; use an explicit pool update")
		}
	}
	if !exists || old.Endpoint != pool.Endpoint || old.Scope != pool.Scope {
		var retained bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM executions WHERE body->>'executor'='opensandbox'
   AND (body->>'status' IN ('queued','running','cancelling') OR body->>'runtimeCleanupPending'='true')
   AND (body->'runtimePlan'->>'pool'=$1 OR body->'runtimePlan' IS NULL))`, pool.ID).Scan(&retained); err != nil {
			return err
		}
		if retained {
			return errors.New("drain queued, active and unconfirmed runtimes before changing a pool target")
		}
	}
	body, err = json.Marshal(pool)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO runtime_pools(id,endpoint,body) VALUES($1,$2,$3)
 ON CONFLICT(id) DO UPDATE SET endpoint=excluded.endpoint,body=excluded.body`, pool.ID, pool.Endpoint, body); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func runtimePool(ctx context.Context, tx pgx.Tx, value contract.Execution) (domain.RuntimePool, error) {
	var pool domain.RuntimePool
	plan := value.RuntimePlan
	if plan == nil {
		return pool, domain.ErrRuntimePlan
	}
	var body []byte
	if err := tx.QueryRow(ctx, `SELECT body FROM runtime_pools WHERE id=$1`, plan.Pool).Scan(&body); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return pool, domain.ErrRuntimePlan
		}
		return pool, err
	}
	if err := json.Unmarshal(body, &pool); err != nil {
		return pool, err
	}
	if pool.Endpoint != plan.Endpoint || pool.Scope != plan.Scope || plan.Resources.CPUMillis <= 0 || plan.Resources.MemoryBytes <= 0 {
		return pool, domain.ErrRuntimePlan
	}
	return pool, nil
}

// reserveRuntime runs under the same transaction lock as model/tenant admission.
// Running records reserve resources before any network allocation is attempted.
func (s *Store) reserveRuntime(ctx context.Context, tx pgx.Tx, value contract.Execution) error {
	if value.Executor != "opensandbox" {
		if value.RuntimePlan != nil {
			return domain.ErrRuntimePlan
		}
		return nil
	}
	pool, err := runtimePool(ctx, tx, value)
	if err != nil {
		return err
	}
	var used contract.RuntimeResources
	var missing bool
	if err = tx.QueryRow(ctx, `SELECT
 COALESCE(sum((body->'runtimePlan'->'resources'->>'cpuMillis')::bigint),0),
 COALESCE(sum((body->'runtimePlan'->'resources'->>'memoryBytes')::bigint),0),
 COALESCE(bool_or(body->'runtimePlan' IS NULL),false)
 FROM executions WHERE body->>'executor'='opensandbox'
 AND (body->>'status' IN ('running','cancelling') OR body->>'runtimeCleanupPending'='true')
 AND (body->'runtimePlan'->>'pool'=$1 OR body->'runtimePlan' IS NULL)`, pool.ID).Scan(&used.CPUMillis, &used.MemoryBytes, &missing); err != nil {
		return err
	}
	if missing || !domain.FitsRuntime(pool.Capacity, used, value.RuntimePlan.Resources) {
		return domain.ErrResourceCapacity
	}
	return nil
}
