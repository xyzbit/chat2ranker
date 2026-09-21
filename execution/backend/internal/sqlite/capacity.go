package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/xyzbit/agentkiln/execution/backend/internal/domain"
)

// CapacityState projects local work without loading prompts or artifacts.
// SQLite has no distributed model/tenant admission limits or River backoff.
func (s *Store) CapacityState(ctx context.Context) (domain.CapacityState, error) {
	state := domain.CapacityState{}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return state, err
	}
	defer tx.Rollback()
	pools, err := tx.QueryContext(ctx, `SELECT body FROM runtime_pools`)
	if err != nil {
		return state, err
	}
	for pools.Next() {
		var body string
		var pool domain.RuntimePool
		if err = pools.Scan(&body); err != nil {
			pools.Close()
			return state, err
		}
		if err = json.Unmarshal([]byte(body), &pool); err != nil {
			pools.Close()
			return state, err
		}
		state.Pools = append(state.Pools, pool)
	}
	pools.Close()
	if pools.Err() != nil {
		return state, pools.Err()
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,
 CASE WHEN COALESCE(json_extract(spec_json,'$.metadata.responseId'),'')<>'' THEN 'online' ELSE 'evaluation' END,
 status, executor, COALESCE(json_extract(spec_json,'$.harness'),''),
 COALESCE(json_extract(spec_json,'$.modelConnectionId'),''), COALESCE(json_extract(spec_json,'$.model'),''),
 COALESCE(json_extract(spec_json,'$.metadata.tenantId'),''), runtime_plan_json,
 COALESCE(json_array_length(spec_json,'$.messages'),0)>0, created_at, queue_deadline_ms, runtime_cleanup_pending, max_model_requests
 FROM executions WHERE status IN ('queued','running','cancelling') OR runtime_cleanup_pending=1`)
	if err != nil {
		return state, err
	}
	defer rows.Close()
	for rows.Next() {
		entry := domain.CapacityEntry{DeliveryReady: true}
		var created string
		var plan sql.NullString
		var deadline int64
		if err := rows.Scan(&entry.ID, &entry.Queue, &entry.Status, &entry.Executor, &entry.Harness, &entry.Connection, &entry.Model, &entry.Tenant, &plan, &entry.StructuredHistory, &created, &deadline, &entry.CleanupPending, &entry.MaxModelRequests); err != nil {
			return state, err
		}
		entry.QueueExpiresAt = queueDeadline(deadline)
		entry.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return state, err
		}
		if plan.Valid {
			if err = json.Unmarshal([]byte(plan.String), &entry.RuntimePlan); err != nil {
				return state, err
			}
		}
		state.Entries = append(state.Entries, entry)
	}
	if rows.Err() != nil {
		return state, rows.Err()
	}
	rows.Close()
	return state, tx.Commit()
}
