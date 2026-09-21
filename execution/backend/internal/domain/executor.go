package domain

import (
	"context"
	"encoding/json"
	"time"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
)

// ExecutorEvent is a non-terminal progress event emitted while a harness
// invocation is running.
type ExecutorEvent struct {
	Type    string
	Message string
	Data    json.RawMessage
	At      time.Time
}

type EventSink func(ExecutorEvent) error

// Executor runs one immutable harness invocation. The local adapter is a
// best-effort development implementation; durable adapters may map this port
// to a container, Kubernetes Job, or remote sandbox. Run returns only after
// confirming shutdown, or returns an error wrapping ErrRuntimeStopUnconfirmed.
// That error retains the execution's persistent capacity reservation.
type Executor interface {
	Name() string
	Probe(context.Context, string) contract.Availability
	Run(context.Context, contract.Execution, EventSink) (contract.Result, error)
	// Cleanup confirms release of a terminal attempt without starting commands.
	// Missing evidence returns ErrRuntimeStopUnconfirmed and retains capacity.
	Cleanup(context.Context, contract.Execution) error
}
