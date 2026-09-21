package app

import (
	"context"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/execution/backend/internal/domain"
)

// ExpireQueued finalizes overdue unstarted work independently of Worker capacity.
// API reconciliation and delivery both use the deadline frozen at admission.
func (s *Service) ExpireQueued(ctx context.Context) error {
	return s.repo.ExpireQueued(ctx, s.now().UTC())
}

func (s *Service) expireDelivery(ctx context.Context, id string) error {
	if err := s.ExpireQueued(ctx); err != nil {
		return err
	}
	value, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	// A competing transaction may hold the row skipped by reconciliation. Do
	// not acknowledge delivery until that owner commits an authoritative state.
	if value.Status == contract.StatusQueued {
		return domain.ErrQueueExpired
	}
	if !contract.IsTerminal(value.Status) {
		return domain.ErrInProgress
	}
	return nil
}
