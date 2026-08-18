package domain

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound          = errors.New("not found")
	ErrConflict          = errors.New("conflict")
	ErrIdempotencyReplay = errors.New("idempotency replay")
)

type UnitOfWork interface {
	WithinTx(ctx context.Context, fn func(Repository) error) error
}

type DatasetRepository interface {
	ListDatasetFamilies(ctx context.Context) ([]DatasetFamily, error)
	ListDatasetVersions(ctx context.Context, familyID string) ([]DatasetVersion, error)
	GetDatasetVersion(ctx context.Context, id string) (DatasetVersion, error)
	CreateDatasetFamily(ctx context.Context, family DatasetFamily, version DatasetVersion) error
	CreateDatasetVersion(ctx context.Context, version DatasetVersion) error
}

type AgentRepository interface {
	ListAgentFamilies(ctx context.Context) ([]AgentFamily, error)
	ListAgentVersions(ctx context.Context, familyID string) ([]AgentVersion, error)
	GetAgentVersion(ctx context.Context, id string) (AgentVersion, error)
	CreateAgentFamily(ctx context.Context, family AgentFamily, version AgentVersion) error
	CreateAgentVersion(ctx context.Context, version AgentVersion) error
}

type ExperimentRepository interface {
	ListExperiments(ctx context.Context) ([]ExperimentSummary, error)
	GetExperiment(ctx context.Context, id string) (Experiment, error)
	CreateExperiment(ctx context.Context, experiment Experiment, initial Message) error
	UpdateExperimentSelection(ctx context.Context, experiment Experiment, message Message) error
	ListMessages(ctx context.Context, experimentID string) ([]Message, error)
	AddMessages(ctx context.Context, experimentID string, messages ...Message) error
	AppendControlMessages(ctx context.Context, experimentID string, messages ...Message) error
	BindControlSession(ctx context.Context, experimentID, controlSessionID string, updatedAt time.Time) error
	UpdateExperimentControl(ctx context.Context, experiment Experiment) error
}

type ControlRepository interface {
	GetControlCommandByIdempotencyKey(ctx context.Context, experimentID, key string) (ControlCommand, error)
	CreateControlCommand(ctx context.Context, command ControlCommand, event ControlEvent) error
	ListControlEvents(ctx context.Context, experimentID string) ([]ControlEvent, error)
}

type RunRepository interface {
	GetRun(ctx context.Context, id string) (Run, error)
	GetRunByIdempotencyKey(ctx context.Context, experimentID, key string) (Run, error)
	ListRunsByExperiment(ctx context.Context, experimentID string) ([]Run, error)
	ListActiveRuns(ctx context.Context) ([]Run, error)
	CreateRun(ctx context.Context, run Run, items []RunItem, created RunEvent) error
	UpdateRunStatus(ctx context.Context, runID, status string, at time.Time, runError string) error
	ListRunItems(ctx context.Context, runID string) ([]RunItem, error)
	ResetActiveRunItems(ctx context.Context, runID string) error
	ClaimRunItem(ctx context.Context, runID, itemID string, startedAt time.Time) (bool, error)
	CompleteRunItem(ctx context.Context, itemID, resultKey string, result CaseResult, completedAt time.Time, events []RunEvent) (bool, error)
	FinishRun(ctx context.Context, run Run, message Message, event RunEvent) error
	CancelRun(ctx context.Context, runID string, at time.Time, event RunEvent) error
	AppendRunEvent(ctx context.Context, event RunEvent) error
}

// Repository is the composite persistence port owned by the Rank domain.
// Application services depend on these interfaces; SQL and SQLite types stay
// entirely inside persistence adapters.
type Repository interface {
	UnitOfWork
	DatasetRepository
	AgentRepository
	ExperimentRepository
	ControlRepository
	RunRepository
}
