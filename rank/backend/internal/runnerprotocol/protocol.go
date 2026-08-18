package runnerprotocol

import (
	"encoding/json"

	"github.com/xyzbit/chat2ranker/rank/backend/internal/domain"
)

const Version = 1

const (
	KindCase  = "case"
	KindJudge = "judge"
)

type Request struct {
	ProtocolVersion int               `json:"protocolVersion"`
	ExecutionID     string            `json:"executionId"`
	Kind            string            `json:"kind"`
	RunID           string            `json:"runId"`
	CaseID          string            `json:"caseId"`
	RunnerType      string            `json:"runnerType"`
	Model           string            `json:"model,omitempty"`
	Tools           []string          `json:"tools,omitempty"`
	Prompt          string            `json:"prompt"`
	Expected        map[string]any    `json:"expected,omitempty"`
	CandidateOutput string            `json:"candidateOutput,omitempty"`
	WorkspaceDir    string            `json:"workspaceDir"`
	ArtifactDir     string            `json:"artifactDir"`
	HarnessHome     string            `json:"harnessHome"`
	TimeoutMs       int64             `json:"timeoutMs"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

type Response struct {
	ProtocolVersion int                  `json:"protocolVersion"`
	ExecutionID     string               `json:"executionId"`
	Kind            string               `json:"kind"`
	Status          string               `json:"status"`
	Output          string               `json:"output,omitempty"`
	Passed          bool                 `json:"passed,omitempty"`
	Score           float64              `json:"score,omitempty"`
	Reason          string               `json:"reason,omitempty"`
	Cost            float64              `json:"cost,omitempty"`
	CostKnown       bool                 `json:"costKnown"`
	DurationMs      int64                `json:"durationMs"`
	Usage           domain.Usage         `json:"usage,omitempty"`
	Artifacts       []domain.ArtifactRef `json:"artifacts,omitempty"`
	Error           string               `json:"error,omitempty"`
	Native          json.RawMessage      `json:"native,omitempty"`
}
