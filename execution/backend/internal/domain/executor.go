package domain

import (
	"context"
	"encoding/json"
	"time"

	"github.com/xyzbit/chat2ranker/execution/backend/contract"
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
// to a container, Kubernetes Job, or remote sandbox.
type Executor interface {
	Name() string
	Probe(context.Context, string) contract.Availability
	Run(context.Context, string, contract.Spec, EventSink) (contract.Result, error)
}
