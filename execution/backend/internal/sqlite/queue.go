package sqlite

import (
	"context"
	"time"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/execution/backend/internal/domain"
)

// ExpireQueued atomically changes only overdue queued records and appends their
// terminal events. Running work keeps its independently frozen runtime timeout.
func (s *Store) ExpireQueued(ctx context.Context, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `UPDATE executions SET status='failed',completed_at=?
 WHERE status='queued' AND queue_deadline_ms<=? RETURNING id,attempt,queue_deadline_ms`, formatTime(now), now.UnixMilli())
	if err != nil {
		return err
	}
	var events []contract.Event
	for rows.Next() {
		var id string
		var attempt int
		var deadline int64
		if err := rows.Scan(&id, &attempt, &deadline); err != nil {
			rows.Close()
			return err
		}
		events = append(events, domain.QueueExpiryEvent(id, attempt, queueDeadline(deadline), now))
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, event := range events {
		if _, err = tx.ExecContext(ctx, `UPDATE executions SET error=? WHERE id=?`, event.Message, event.ExecutionID); err != nil {
			return err
		}
		if err = appendEvent(ctx, tx, event); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func queueDeadline(milliseconds int64) time.Time {
	if milliseconds <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(milliseconds).UTC()
}
