package domain

import (
	"context"
	"errors"
	"time"

	"github.com/xyzbit/chat2ranker/execution/backend/contract"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)

// Repository is the persistence port owned by the generic execution domain.
// Implementations may use SQLite, PostgreSQL, or another transactional store.
type Repository interface {
	Create(context.Context, contract.Execution, contract.Event) error
	Get(context.Context, string) (contract.Execution, error)
	GetByIdempotencyKey(context.Context, string) (contract.Execution, error)
	ListActive(context.Context) ([]contract.Execution, error)
	ListEvents(ctx context.Context, executionID string, afterSequence int64) ([]contract.Event, error)
	AppendEvent(ctx context.Context, event contract.Event) error
	MarkRunning(ctx context.Context, id string, attempt int, executor, externalHandle string, startedAt time.Time, event contract.Event) error
	Complete(ctx context.Context, id string, result contract.Result, completedAt time.Time, event contract.Event) error
	Fail(ctx context.Context, id, message string, completedAt time.Time, event contract.Event) error
	Cancel(ctx context.Context, id string, completedAt time.Time, event contract.Event) error
	Requeue(ctx context.Context, id string, event contract.Event) error
	Close() error
}
