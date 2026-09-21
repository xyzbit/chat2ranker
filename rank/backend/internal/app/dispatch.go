package app

import (
	"context"
	"errors"
	"time"
)

// RunPendingDispatcher discovers work admitted by separate HTTP processes.
// One dispatcher owns recovery and observers; periodic discovery does not reset live runs.
func (s *Service) RunPendingDispatcher(ctx context.Context) error {
	if s.externalDispatch {
		return errors.New("an external-dispatch API cannot own pending work")
	}
	ticker := time.NewTicker(s.dispatchRetryInterval)
	defer ticker.Stop()
	for {
		runs, err := s.repo.ListActiveRuns(ctx)
		if err != nil {
			return err
		}
		for _, run := range runs {
			s.dispatch(run.ID)
		}
		if err := s.ResumeActiveResponses(ctx); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
