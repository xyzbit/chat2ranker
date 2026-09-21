package app

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/execution/backend/internal/domain"
	"github.com/xyzbit/agentkiln/execution/backend/internal/jobs"
)

// localWorkers is single-process concurrency control; PostgreSQL uses River.
type localWorkers struct {
	once               sync.Once
	mu                 sync.Mutex
	stopping           bool
	wg                 sync.WaitGroup
	online, evaluation int
	stop               chan struct{}
}

func newLocalWorkers(online, evaluation int) *localWorkers {
	return &localWorkers{online: online, evaluation: evaluation, stop: make(chan struct{})}
}

func (s *Service) dispatch(_ string) {
	local := s.local
	local.mu.Lock()
	defer local.mu.Unlock()
	if local.stopping {
		return
	}
	local.once.Do(func() {
		for queue, slots := range map[string]int{jobs.Online: local.online, jobs.Evaluation: local.evaluation} {
			for range slots {
				local.wg.Add(1)
				go s.localLoop(queue)
			}
		}
	})
}

func (s *Service) localLoop(queue string) {
	defer s.local.wg.Done()
	for {
		id, ctx, release, err := s.claimLocal(queue)
		if err != nil {
			slog.Error("local execution poll failed", "error", err)
		}
		if id != "" {
			err := s.Execute(ctx, id)
			release()
			waiting := errors.Is(err, domain.ErrResourceCapacity) || errors.Is(err, domain.ErrExecutorUnavailable) || errors.Is(err, domain.ErrInProgress)
			if err != nil && !waiting {
				slog.Error("local execution delivery failed", "execution", id, "error", err)
			}
			if !waiting {
				continue
			}
		}
		timer := time.NewTimer(s.pollInterval)
		select {
		case <-s.local.stop:
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (s *Service) claimLocal(queue string) (string, context.Context, func(), error) {
	s.local.mu.Lock()
	defer s.local.mu.Unlock()
	if s.local.stopping {
		return "", nil, nil, nil
	}
	pollCtx, pollCancel := context.WithTimeout(context.Background(), s.persistenceTimeout)
	defer pollCancel()
	active, err := s.repo.ListActive(pollCtx)
	if err != nil {
		return "", nil, nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, value := range active {
		if value.Status != contract.StatusQueued || jobs.Queue(value.Spec) != queue || s.cancels[value.ID] != nil {
			continue
		}
		ctx, cancel := context.WithCancel(context.Background())
		s.cancels[value.ID] = cancel
		return value.ID, ctx, func() {
			cancel()
			s.mu.Lock()
			delete(s.cancels, value.ID)
			s.mu.Unlock()
		}, nil
	}
	return "", nil, nil, nil
}

// StopLocal stops claiming queued work, drains active invocations until ctx's
// deadline, then cancels and waits for bounded runtime cleanup and persistence.
// Queued records remain durable for the next local owner.
func (s *Service) StopLocal(ctx context.Context) error {
	if s.local == nil {
		return nil
	}
	s.local.mu.Lock()
	if !s.local.stopping {
		s.local.stopping = true
		close(s.local.stop)
	}
	s.local.mu.Unlock()
	done := make(chan struct{})
	go func() { s.local.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
	}
	s.mu.Lock()
	for _, cancel := range s.cancels {
		cancel()
	}
	s.mu.Unlock()
	timer := time.NewTimer(s.cleanupGrace + s.persistenceTimeout)
	defer timer.Stop()
	select {
	case <-done:
		return nil
	case <-timer.C:
		return errors.New("local runtime shutdown could not be confirmed within cleanup deadline")
	}
}
