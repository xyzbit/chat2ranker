package app

import (
	"context"
	"errors"
	"time"

	"github.com/xyzbit/agentkiln/execution/backend/internal/domain"
)

// ReconcileCleanup releases only terminal attempts whose executor can confirm
// resource cleanup. Unknown or unreachable runtimes keep their reservations.
func (s *Service) ReconcileCleanup(ctx context.Context) error {
	values, err := s.repo.ListPendingCleanup(ctx)
	if err != nil {
		return err
	}
	var failures []error
	for _, value := range values {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if value.Executor != s.executor.Name() {
			continue
		}
		cleanupCtx, cancel := context.WithTimeout(ctx, min(s.cleanupGrace, time.Duration(value.CleanupGraceMs)*time.Millisecond))
		err := s.executor.Cleanup(cleanupCtx, value)
		cancel()
		if errors.Is(err, domain.ErrRuntimeStopUnconfirmed) {
			continue
		}
		if err == nil {
			err = s.repo.ConfirmRuntimeStopped(ctx, value.ID, value.Attempt)
		}
		if err != nil && !errors.Is(err, domain.ErrConflict) {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
