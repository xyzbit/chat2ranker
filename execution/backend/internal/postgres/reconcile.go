package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/execution/backend/internal/domain"
)

// Reconcile finalizes executions whose delivery is terminal without an
// application outcome. Started invocations wait out their frozen lifetime and
// are reported as unknown, never replayed or claimed to have stopped cleanly.
func (s *Store) Reconcile(ctx context.Context, now time.Time) error {
	rows, err := s.pool.Query(ctx, `SELECT e.body,e.idempotency_key
 FROM executions e LEFT JOIN river_job j ON j.id=e.job_id
 WHERE e.body->>'status' IN ('queued','running','cancelling')
 AND (j.id IS NULL OR j.state IN ('cancelled','discarded','completed'))`)
	if err != nil {
		return err
	}
	var candidates []contract.Execution
	for rows.Next() {
		value, err := scanExecution(rows)
		if err != nil {
			rows.Close()
			return err
		}
		candidates = append(candidates, value)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, value := range candidates {
		message := "execution_delivery_failed: delivery ended before runtime start"
		if value.Status != contract.StatusQueued {
			if value.StartedAt != nil && now.Before(value.StartedAt.Add(time.Duration(value.RuntimeTimeoutMs+value.CleanupGraceMs)*time.Millisecond)) {
				continue
			}
			message = "execution_outcome_unknown: delivery ended without a confirmed runtime outcome; invocation was not replayed"
		}
		event := contract.Event{ExecutionID: value.ID, Attempt: value.Attempt, Type: "execution.failed", Status: contract.StatusFailed, Message: message, At: now}
		if err := s.Fail(ctx, value.ID, message, now, event); err != nil && !errors.Is(err, domain.ErrConflict) {
			return err
		}
	}
	return nil
}
