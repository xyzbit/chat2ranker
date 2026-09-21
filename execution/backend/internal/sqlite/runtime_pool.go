package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/execution/backend/internal/domain"
)

func (s *Store) ConfigureRuntimePool(ctx context.Context, pool domain.RuntimePool, replace bool) error {
	if err := pool.Validate(); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE runtime_pools SET id=id WHERE id=?`, pool.ID); err != nil {
		return err
	}
	var body string
	err = tx.QueryRowContext(ctx, `SELECT body FROM runtime_pools WHERE id=?`, pool.ID).Scan(&body)
	exists := !errors.Is(err, sql.ErrNoRows)
	if err != nil && exists {
		return err
	}
	var old domain.RuntimePool
	if exists {
		if err = json.Unmarshal([]byte(body), &old); err != nil {
			return err
		}
		if old == pool {
			return tx.Commit()
		}
		if !replace {
			return errors.New("runtime pool configuration differs; use an explicit pool update")
		}
	}
	if !exists || old.Endpoint != pool.Endpoint || old.Scope != pool.Scope {
		var retained bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM executions WHERE executor='opensandbox'
 AND (status IN ('queued','running','cancelling') OR runtime_cleanup_pending=1)
 AND (json_extract(runtime_plan_json,'$.pool')=? OR runtime_plan_json IS NULL))`, pool.ID).Scan(&retained); err != nil {
			return err
		}
		if retained {
			return errors.New("drain queued, active and unconfirmed runtimes before changing a pool target")
		}
	}
	encoded, err := json.Marshal(pool)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO runtime_pools(id,endpoint,body) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET endpoint=excluded.endpoint,body=excluded.body`, pool.ID, pool.Endpoint, string(encoded)); err != nil {
		return err
	}
	return tx.Commit()
}

func runtimePool(ctx context.Context, tx *sql.Tx, plan *contract.RuntimePlan) (domain.RuntimePool, error) {
	var pool domain.RuntimePool
	if plan == nil {
		return pool, domain.ErrRuntimePlan
	}
	var body string
	if err := tx.QueryRowContext(ctx, `SELECT body FROM runtime_pools WHERE id=?`, plan.Pool).Scan(&body); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return pool, domain.ErrRuntimePlan
		}
		return pool, err
	}
	if err := json.Unmarshal([]byte(body), &pool); err != nil {
		return pool, err
	}
	if pool.Endpoint != plan.Endpoint || pool.Scope != plan.Scope || plan.Resources.CPUMillis <= 0 || plan.Resources.MemoryBytes <= 0 {
		return pool, domain.ErrRuntimePlan
	}
	return pool, nil
}

func reserveRuntime(ctx context.Context, tx *sql.Tx, id string) error {
	var executor string
	var planJSON sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT executor,runtime_plan_json FROM executions WHERE id=?`, id).Scan(&executor, &planJSON); err != nil {
		return err
	}
	if executor != "opensandbox" {
		if planJSON.Valid {
			return domain.ErrRuntimePlan
		}
		return nil
	}
	var plan *contract.RuntimePlan
	if planJSON.Valid {
		if err := json.Unmarshal([]byte(planJSON.String), &plan); err != nil {
			return err
		}
	}
	pool, err := runtimePool(ctx, tx, plan)
	if err != nil {
		return err
	}
	var used contract.RuntimeResources
	var missing bool
	if err = tx.QueryRowContext(ctx, `SELECT
 COALESCE(sum(json_extract(runtime_plan_json,'$.resources.cpuMillis')),0),
 COALESCE(sum(json_extract(runtime_plan_json,'$.resources.memoryBytes')),0),
 COALESCE(max(runtime_plan_json IS NULL),0)
 FROM executions WHERE executor='opensandbox' AND (status IN ('running','cancelling') OR runtime_cleanup_pending=1)
 AND (json_extract(runtime_plan_json,'$.pool')=? OR runtime_plan_json IS NULL)`, pool.ID).Scan(&used.CPUMillis, &used.MemoryBytes, &missing); err != nil {
		return err
	}
	if missing || !domain.FitsRuntime(pool.Capacity, used, plan.Resources) {
		return domain.ErrResourceCapacity
	}
	return nil
}
