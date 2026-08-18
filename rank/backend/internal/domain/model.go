package domain

import (
	"encoding/json"
	"time"
)

type Case struct {
	ID       string         `json:"id"`
	Title    string         `json:"title"`
	Input    string         `json:"input"`
	Expected map[string]any `json:"expected"`
}

type DatasetFamily struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Description     string    `json:"description"`
	LatestVersionID string    `json:"latestVersionId"`
	CreatedAt       time.Time `json:"createdAt"`
}

type DatasetVersion struct {
	ID          string          `json:"id"`
	FamilyID    string          `json:"familyId"`
	Name        string          `json:"name"`
	Version     int             `json:"version"`
	Source      string          `json:"source"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema,omitempty"`
	Rubric      json.RawMessage `json:"rubric,omitempty"`
	Cases       []Case          `json:"cases"`
	CreatedAt   time.Time       `json:"createdAt"`
	CaseCount   int             `json:"caseCount"`
}

type DatasetAsset struct {
	DatasetVersion
	FamilyDescription string           `json:"familyDescription"`
	VersionCount      int              `json:"versionCount"`
	Versions          []DatasetVersion `json:"versions"`
}

type AgentFamily struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Handle          string    `json:"handle"`
	Description     string    `json:"description"`
	LatestVersionID string    `json:"latestVersionId"`
	CreatedAt       time.Time `json:"createdAt"`
}

type AgentVersion struct {
	ID          string    `json:"id"`
	FamilyID    string    `json:"familyId"`
	Name        string    `json:"name"`
	Handle      string    `json:"handle"`
	Version     int       `json:"version"`
	RunnerType  string    `json:"runnerType"`
	Description string    `json:"description"`
	Model       string    `json:"model"`
	Tools       []string  `json:"tools"`
	CreatedAt   time.Time `json:"createdAt"`
}

type RuntimeAvailability struct {
	Available bool   `json:"available"`
	Label     string `json:"label,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

type AgentVersionView struct {
	AgentVersion
	Runtime RuntimeAvailability `json:"runtime"`
}

type AgentAsset struct {
	AgentVersionView
	FamilyDescription string             `json:"familyDescription"`
	VersionCount      int                `json:"versionCount"`
	Versions          []AgentVersionView `json:"versions"`
}

type Message struct {
	ID           string    `json:"id"`
	ExperimentID string    `json:"-"`
	Role         string    `json:"role"`
	Content      string    `json:"content"`
	RunID        string    `json:"runId,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
}

type ControlCommand struct {
	ID               string          `json:"id"`
	ExperimentID     string          `json:"experimentId"`
	ControlSessionID string          `json:"controlSessionId"`
	IdempotencyKey   string          `json:"idempotencyKey"`
	Type             string          `json:"type"`
	Payload          json.RawMessage `json:"payload"`
	Result           json.RawMessage `json:"result"`
	CreatedAt        time.Time       `json:"createdAt"`
}

type ControlEvent struct {
	ID               int64           `json:"id"`
	ExperimentID     string          `json:"-"`
	ControlSessionID string          `json:"controlSessionId"`
	CommandID        string          `json:"commandId"`
	Type             string          `json:"type"`
	Payload          json.RawMessage `json:"payload"`
	CreatedAt        time.Time       `json:"createdAt"`
}

type A2UIAction struct {
	Command string `json:"command"`
	Token   string `json:"token"`
}

type A2UIProjection struct {
	Revision string                `json:"revision"`
	Actions  map[string]A2UIAction `json:"actions"`
}

type Experiment struct {
	ID               string    `json:"id"`
	Title            string    `json:"title"`
	DatasetVersionID string    `json:"datasetVersionId,omitempty"`
	AgentVersionID   string    `json:"agentVersionId,omitempty"`
	ControlSessionID string    `json:"controlSessionId,omitempty"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type ExperimentSummary struct {
	Experiment
	RunCount  int         `json:"runCount"`
	LatestRun *RunSummary `json:"latestRun,omitempty"`
}

type ExperimentView struct {
	Experiment
	Dataset       *DatasetVersion `json:"dataset"`
	Agent         *AgentVersion   `json:"agent"`
	Messages      []Message       `json:"messages"`
	Runs          []Run           `json:"runs"`
	ControlEvents []ControlEvent  `json:"controlEvents"`
	A2UI          A2UIProjection  `json:"a2ui"`
}

type CaseResult struct {
	CaseID           string        `json:"caseId"`
	Title            string        `json:"title"`
	Passed           bool          `json:"passed"`
	Cost             float64       `json:"cost"`
	CostKnown        bool          `json:"costKnown"`
	Score            float64       `json:"score"`
	Output           string        `json:"output"`
	Reason           string        `json:"reason"`
	DurationMs       int64         `json:"durationMs,omitempty"`
	ExecutionID      string        `json:"executionId,omitempty"`
	JudgeExecutionID string        `json:"judgeExecutionId,omitempty"`
	Usage            Usage         `json:"usage,omitempty"`
	Artifacts        []ArtifactRef `json:"artifacts,omitempty"`
}

type Usage struct {
	InputTokens  int64 `json:"inputTokens,omitempty"`
	OutputTokens int64 `json:"outputTokens,omitempty"`
}

type ArtifactRef struct {
	Kind        string `json:"kind"`
	ExecutionID string `json:"executionId"`
	Path        string `json:"path"`
	Size        int64  `json:"size"`
}

type RunEvent struct {
	ID               int64     `json:"-"`
	RunID            string    `json:"-"`
	Type             string    `json:"type"`
	At               time.Time `json:"at"`
	CaseID           string    `json:"caseId,omitempty"`
	Status           string    `json:"status,omitempty"`
	DatasetVersionID string    `json:"datasetVersionId,omitempty"`
	AgentVersionID   string    `json:"agentVersionId,omitempty"`
	Passed           *bool     `json:"passed,omitempty"`
	Cost             *float64  `json:"cost,omitempty"`
	Score            *float64  `json:"score,omitempty"`
	Output           string    `json:"output,omitempty"`
	Reason           string    `json:"reason,omitempty"`
}

type Run struct {
	ID              string         `json:"id"`
	ExperimentID    string         `json:"experimentId"`
	IdempotencyKey  string         `json:"-"`
	Status          string         `json:"status"`
	DatasetSnapshot DatasetVersion `json:"datasetSnapshot"`
	AgentSnapshot   AgentVersion   `json:"agentSnapshot"`
	Concurrency     int            `json:"concurrency"`
	CreatedAt       time.Time      `json:"createdAt"`
	StartedAt       *time.Time     `json:"startedAt,omitempty"`
	CompletedAt     *time.Time     `json:"completedAt,omitempty"`
	DurationMs      int64          `json:"durationMs"`
	Passed          int            `json:"passed"`
	Total           int            `json:"total"`
	PassRate        int            `json:"passRate"`
	Cost            float64        `json:"cost"`
	CostKnown       bool           `json:"costKnown"`
	Error           string         `json:"error,omitempty"`
	Results         []CaseResult   `json:"results"`
	Events          []RunEvent     `json:"events"`
}

type RunSummary struct {
	ID          string     `json:"id"`
	Status      string     `json:"status"`
	Passed      int        `json:"passed"`
	Total       int        `json:"total"`
	PassRate    int        `json:"passRate"`
	Cost        float64    `json:"cost"`
	DurationMs  int64      `json:"durationMs"`
	CreatedAt   time.Time  `json:"createdAt"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
}

type RunItem struct {
	ID          string
	RunID       string
	CaseID      string
	Title       string
	Ordinal     int
	Status      string
	ResultKey   string
	Result      *CaseResult
	CreatedAt   time.Time
	StartedAt   *time.Time
	CompletedAt *time.Time
}

type Bootstrap struct {
	Datasets    []DatasetAsset      `json:"datasets"`
	Agents      []AgentAsset        `json:"agents"`
	Experiments []ExperimentSummary `json:"experiments"`
}

const (
	RunQueued    = "queued"
	RunPreparing = "preparing"
	RunRunning   = "running"
	RunScoring   = "scoring"
	RunComplete  = "complete"
	RunFailed    = "failed"
	RunCancelled = "cancelled"

	ItemQueued    = "queued"
	ItemRunning   = "running"
	ItemComplete  = "complete"
	ItemCancelled = "cancelled"
)

func IsActiveRun(status string) bool {
	return status == RunQueued || status == RunPreparing || status == RunRunning || status == RunScoring
}
