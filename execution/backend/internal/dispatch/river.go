// Package dispatch connects River delivery to the Execution application.
package dispatch

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/xyzbit/agentkiln/execution/backend/internal/app"
	"github.com/xyzbit/agentkiln/execution/backend/internal/domain"
	"github.com/xyzbit/agentkiln/execution/backend/internal/jobs"
	"github.com/xyzbit/agentkiln/execution/backend/internal/postgres"
)

// Config bounds queue polling, delivery lifetime and per-process concurrency.
type Config struct {
	ID              string
	OnlineSlots     int
	EvaluationSlots int
	PollInterval    time.Duration
	RescueAfter     time.Duration
}

// NewRiver returns an unstarted worker client. Each delivery gets a repository
// that acknowledges its River job in the same transaction as its final result.
func NewRiver(store *postgres.Store, executor domain.Executor, options app.Options, config Config) (*river.Client[pgx.Tx], error) {
	if config.OnlineSlots < 0 || config.EvaluationSlots < 0 || config.OnlineSlots+config.EvaluationSlots == 0 || config.PollInterval <= 0 || options.ExecutionTimeout <= 0 || options.CleanupGrace <= 0 || options.PersistenceTimeout <= 0 {
		return nil, errors.New("worker requires non-negative slots, at least one active queue, and positive timeouts")
	}
	jobTimeout := options.ExecutionTimeout + options.CleanupGrace + options.PersistenceTimeout
	if config.RescueAfter <= jobTimeout {
		return nil, errors.New("rescue delay must exceed execution, cleanup and persistence timeouts")
	}
	workers := river.NewWorkers()
	options.Workers = false
	options.WorkerID = config.ID
	river.AddWorker(workers, &executionWorker{store: store, executor: executor, options: options, poll: config.PollInterval})
	queues := map[string]river.QueueConfig{}
	if config.OnlineSlots > 0 {
		queues[jobs.Online] = river.QueueConfig{MaxWorkers: config.OnlineSlots}
	}
	if config.EvaluationSlots > 0 {
		queues[jobs.Evaluation] = river.QueueConfig{MaxWorkers: config.EvaluationSlots}
	}
	return river.NewClient(riverpgxv5.New(store.Pool()), &river.Config{
		ID: config.ID, Schema: store.Schema(), Workers: workers, Queues: queues,
		JobTimeout: jobTimeout, RescueStuckJobsAfter: config.RescueAfter,
		FetchPollInterval: config.PollInterval, FetchCooldown: config.PollInterval,
	})
}

type executionWorker struct {
	river.WorkerDefaults[jobs.Execution]
	store    *postgres.Store
	executor domain.Executor
	options  app.Options
	poll     time.Duration
}

func (worker *executionWorker) Work(ctx context.Context, job *river.Job[jobs.Execution]) error {
	service := app.NewService(worker.store.ForJob(job), worker.executor, worker.options)
	err := service.Execute(ctx, job.Args.ExecutionID)
	if errors.Is(err, domain.ErrQueueExpired) || errors.Is(err, domain.ErrInProgress) || errors.Is(err, domain.ErrResourceCapacity) || errors.Is(err, domain.ErrQuotaExceeded) || errors.Is(err, domain.ErrExecutorUnavailable) {
		return river.JobSnooze(worker.poll)
	}
	return err
}
