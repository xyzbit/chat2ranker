// Package contract defines the versioned Execution Service HTTP payloads.
package contract

import (
	"encoding/json"
	"time"
)

const ProtocolVersion = 1

const (
	KindAgent = "agent"
	KindJudge = "judge"

	StatusQueued    = "queued"
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
)

// Spec is an immutable request for one harness invocation.
type Spec struct {
	ProtocolVersion int               `json:"protocolVersion"`
	Kind            string            `json:"kind"`
	Harness         string            `json:"harness"`
	Model           string            `json:"model,omitempty"`
	Preset          string            `json:"preset,omitempty"`
	SystemPrompt    string            `json:"systemPrompt,omitempty"`
	Tools           []string          `json:"tools,omitempty"`
	Skills          []string          `json:"skills,omitempty"`
	Prompt          string            `json:"prompt"`
	Expected        map[string]any    `json:"expected,omitempty"`
	CandidateOutput string            `json:"candidateOutput,omitempty"`
	TimeoutMs       int64             `json:"timeoutMs,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

type SubmitRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	Spec           Spec   `json:"spec"`
}

type Usage struct {
	InputTokens      int64 `json:"inputTokens,omitempty"`
	OutputTokens     int64 `json:"outputTokens,omitempty"`
	CacheReadTokens  int64 `json:"cacheReadTokens,omitempty"`
	CacheWriteTokens int64 `json:"cacheWriteTokens,omitempty"`
	ReasoningTokens  int64 `json:"reasoningTokens,omitempty"`
}

type ArtifactRef struct {
	Kind        string `json:"kind"`
	ExecutionID string `json:"executionId"`
	Path        string `json:"path"`
	Size        int64  `json:"size"`
}

type Result struct {
	Output     string          `json:"output,omitempty"`
	Passed     bool            `json:"passed,omitempty"`
	Score      float64         `json:"score,omitempty"`
	Reason     string          `json:"reason,omitempty"`
	Cost       float64         `json:"cost,omitempty"`
	CostKnown  bool            `json:"costKnown"`
	DurationMs int64           `json:"durationMs"`
	Usage      Usage           `json:"usage,omitempty"`
	Artifacts  []ArtifactRef   `json:"artifacts,omitempty"`
	Native     json.RawMessage `json:"native,omitempty"`
}

// Execution is the durable control-plane record for one harness invocation.
type Execution struct {
	ID             string     `json:"id"`
	IdempotencyKey string     `json:"-"`
	Status         string     `json:"status"`
	Executor       string     `json:"executor"`
	Attempt        int        `json:"attempt"`
	ExternalHandle string     `json:"externalHandle,omitempty"`
	WorkerVersion  string     `json:"workerVersion"`
	Spec           Spec       `json:"spec"`
	Result         *Result    `json:"result,omitempty"`
	Error          string     `json:"error,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	StartedAt      *time.Time `json:"startedAt,omitempty"`
	CompletedAt    *time.Time `json:"completedAt,omitempty"`
}

// Event is one durable, ordered lifecycle fact for an execution. Sequence is
// monotonic within one execution and is used as the SSE Last-Event-ID value.
type Event struct {
	Sequence    int64           `json:"sequence"`
	ExecutionID string          `json:"executionId"`
	Attempt     int             `json:"attempt"`
	Type        string          `json:"type"`
	Status      string          `json:"status,omitempty"`
	Message     string          `json:"message,omitempty"`
	Data        json.RawMessage `json:"data,omitempty"`
	At          time.Time       `json:"at"`
}

type Availability struct {
	Available bool   `json:"available"`
	Label     string `json:"label,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

type ArtifactContent struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
}

func IsTerminal(status string) bool {
	return status == StatusCompleted || status == StatusFailed || status == StatusCancelled
}
