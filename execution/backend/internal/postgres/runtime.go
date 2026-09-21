package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/execution/backend/internal/domain"
)

func (s *Store) BeginRuntime(ctx context.Context, id string, attempt int, value contract.RuntimeAllocation) error {
	if value.Handle != "" {
		return errors.New("runtime creation intent cannot contain a handle")
	}
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	result, err := s.pool.Exec(ctx, `UPDATE executions SET body=jsonb_set(body,'{runtime}',$3::jsonb)
 WHERE id=$1 AND (body->>'attempt')::integer=$2 AND body->>'status'='running'
 AND body->>'runtimeCleanupPending'='true' AND body->'runtime' IS NULL`, id, attempt, body)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return domain.ErrConflict
	}
	return nil
}

func (s *Store) AttachRuntime(ctx context.Context, id string, attempt int, handle string) error {
	if handle == "" {
		return errors.New("runtime handle is required")
	}
	result, err := s.pool.Exec(ctx, `UPDATE executions SET body=jsonb_set(body,'{runtime,handle}',to_jsonb($3::text))
 WHERE id=$1 AND (body->>'attempt')::integer=$2 AND body->'runtime' IS NOT NULL
 AND body->>'runtimeCleanupPending'='true' AND (body->'runtime'->>'handle' IS NULL OR body->'runtime'->>'handle'=$3)`, id, attempt, handle)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return domain.ErrConflict
	}
	return nil
}

func (s *Store) ListPendingCleanup(ctx context.Context) ([]contract.Execution, error) {
	rows, err := s.pool.Query(ctx, `SELECT body,idempotency_key FROM executions WHERE body->>'runtimeCleanupPending'='true' AND body->>'status' IN ('completed','failed','cancelled') ORDER BY body->>'createdAt',id`)
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
