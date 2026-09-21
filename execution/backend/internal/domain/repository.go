package domain

import (
	"context"
	"errors"
	"time"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
)

var (
	ErrResourceCapacity       = errors.New("execution is waiting for sandbox CPU or memory capacity")
	ErrRuntimePlan            = errors.New("execution runtime plan does not match a configured resource pool")
	ErrQueueExpired           = errors.New("execution queue deadline expired")
	ErrNotFound               = errors.New("not found")
	ErrConflict               = errors.New("conflict")
	ErrOverloaded             = errors.New("execution queue is full")
	ErrQuotaExceeded          = errors.New("execution is waiting for a global model or tenant slot")
	ErrExecutorUnavailable    = errors.New("worker does not support the frozen execution backend")
	ErrInProgress             = errors.New("previous execution attempt is still within its runtime lifetime")
	ErrRuntimeStopUnconfirmed = errors.New("runtime stop could not be confirmed")
)

// Repository is the persistence port owned by the generic execution domain.
// Implementations may use SQLite, PostgreSQL, or another transactional store.
type Repository interface {
	// ConfigureRuntimePool creates or verifies a durable budget. replace permits an explicit capacity update; target changes require draining all work.
	ConfigureRuntimePool(context.Context, RuntimePool, bool) error
	Create(context.Context, contract.Execution, contract.Event) error
	Get(context.Context, string) (contract.Execution, error)
	GetByIdempotencyKey(context.Context, string) (contract.Execution, error)
	ListActive(context.Context) ([]contract.Execution, error)
	// ExpireQueued fails overdue unstarted work and cancels its delivery atomically.
	ExpireQueued(ctx context.Context, now time.Time) error
	CapacityState(context.Context) (CapacityState, error)
	ListEvents(ctx context.Context, executionID string, afterSequence int64) ([]contract.Event, error)
	AppendEvent(ctx context.Context, event contract.Event) error
	MarkRunning(ctx context.Context, id string, attempt int, executor, externalHandle, workerVersion string, startedAt time.Time, event contract.Event) error
	// BeginRuntime records creation intent only once, on the running attempt.
	BeginRuntime(context.Context, string, int, contract.RuntimeAllocation) error
	// AttachRuntime records a provider handle after creation, including a late
	// response after cancellation or failure; it cannot replace another handle.
	AttachRuntime(context.Context, string, int, string) error
	// ListPendingCleanup returns terminal attempts still retaining resources.
	ListPendingCleanup(context.Context) ([]contract.Execution, error)
	// ConfirmRuntimeStopped clears an attempt's cleanup obligation without changing
	// its business outcome. It also accepts terminal unknown attempts.
	ConfirmRuntimeStopped(ctx context.Context, id string, attempt int) error
	// RecordEvidence retains an observed result and only confirmed artifact references
	// on the current active attempt. It does not authorize success or runtime replay.
	RecordEvidence(ctx context.Context, id string, attempt int, result contract.Result) error
	Complete(ctx context.Context, id string, result contract.Result, completedAt time.Time, event contract.Event) error
	Fail(ctx context.Context, id, message string, completedAt time.Time, event contract.Event) error
	Cancel(ctx context.Context, id string, completedAt time.Time, event contract.Event) error
	// RequestCancel persists intent. Queued work terminates immediately; running
	// work remains cancelling until its runtime has stopped.
	RequestCancel(ctx context.Context, id string, at time.Time) error
	ListModelConnections(context.Context) ([]contract.ModelConnection, error)
	GetModelConnection(context.Context, string) (contract.ModelConnection, error)
	SaveModelConnection(context.Context, contract.ModelConnection) error
	DeleteModelConnection(context.Context, string) error
	ListSystemModelBindings(context.Context) ([]contract.SystemModelBinding, error)
	GetSystemModelBinding(context.Context, string) (contract.SystemModelBinding, error)
	SaveSystemModelBinding(context.Context, contract.SystemModelBinding) error
	Close() error
}
