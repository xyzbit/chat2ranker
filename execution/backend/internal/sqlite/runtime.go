package sqlite

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
)

func (s *Store) BeginRuntime(ctx context.Context, id string, attempt int, value contract.RuntimeAllocation) error {
	if value.Handle != "" {
		return errors.New("runtime creation intent cannot contain a handle")
	}
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE executions SET runtime_json=? WHERE id=? AND attempt=? AND status='running' AND runtime_cleanup_pending=1 AND runtime_json IS NULL`, string(body), id, attempt)
	return affected(result, err)
}

func (s *Store) AttachRuntime(ctx context.Context, id string, attempt int, handle string) error {
	if handle == "" {
		return errors.New("runtime handle is required")
	}
	result, err := s.db.ExecContext(ctx, `UPDATE executions SET runtime_json=json_set(runtime_json,'$.handle',?) WHERE id=? AND attempt=? AND runtime_json IS NOT NULL AND runtime_cleanup_pending=1 AND (json_extract(runtime_json,'$.handle') IS NULL OR json_extract(runtime_json,'$.handle')=?)`, handle, id, attempt, handle)
	return affected(result, err)
}

func (s *Store) ListPendingCleanup(ctx context.Context) ([]contract.Execution, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,idempotency_key,status,executor,attempt,external_handle,worker_version,spec_json,result_json,error,created_at,started_at,completed_at,runtime_timeout_ms,cleanup_grace_ms,runtime_cleanup_pending,runtime_json,queue_deadline_ms,runtime_plan_json,max_model_requests FROM executions WHERE runtime_cleanup_pending=1 AND status IN ('completed','failed','cancelled') ORDER BY created_at,id`)
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
