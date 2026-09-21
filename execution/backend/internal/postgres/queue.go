package postgres

import (
	"context"
	"time"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/execution/backend/internal/domain"
)

// ExpireQueued locks only overdue unstarted records. The failed event and River
// cancellation commit together; concurrent starters and reapers cannot win twice.
func (s *Store) ExpireQueued(ctx context.Context, now time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, s.config.OperationTimeout)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT body,job_id FROM executions
 WHERE body->>'status'='queued'
 AND COALESCE((body->>'queueExpiresAt')::timestamptz,'0001-01-01T00:00:00Z'::timestamptz)<=$1
 ORDER BY id FOR UPDATE SKIP LOCKED`, now)
	if err != nil {
		return err
	}
	type expired struct {
		value contract.Execution
		jobID *int64
	}
	var candidates []expired
	for rows.Next() {
		var entry expired
		if err := rows.Scan(&entry.value, &entry.jobID); err != nil {
			rows.Close()
			return err
		}
		candidates = append(candidates, entry)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, entry := range candidates {
		value := entry.value
		event := domain.QueueExpiryEvent(value.ID, value.Attempt, value.QueueExpiresAt, now)
		value.Status, value.Error, value.CompletedAt = contract.StatusFailed, event.Message, &now
		if err = writeExecution(ctx, tx, value, event); err != nil {
			return err
		}
		if entry.jobID != nil {
			if _, err = s.queue.JobCancelTx(ctx, tx, *entry.jobID); err != nil {
				return err
			}
		}
	}
	return tx.Commit(ctx)
}
