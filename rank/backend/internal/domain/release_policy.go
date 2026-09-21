package domain

import "time"

type ReleaseDecisionID string

// ReleasePolicy binds release gates to one immutable evaluation dataset.
// Revision zero means that evaluated promotion has not been configured.
type ReleasePolicy struct {
	Revision            int64     `json:"revision"`
	DatasetVersionID    string    `json:"datasetVersionId"`
	MinPassRate         int       `json:"minPassRate"`
	MinValidTrialRate   int       `json:"minValidTrialRate"`
	MaxEvidenceAgeHours int       `json:"maxEvidenceAgeHours"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

// ReleaseCheck records an unrounded measured value and its required threshold.
type ReleaseCheck struct {
	Code     string  `json:"code"`
	Passed   bool    `json:"passed"`
	Actual   float64 `json:"actual"`
	Required float64 `json:"required"`
}

// ReleaseEvaluation freezes policy, evidence identity, and all checks at decision time.
type ReleaseEvaluation struct {
	Policy              ReleasePolicy  `json:"policy"`
	EvidenceRunID       string         `json:"evidenceRunId,omitempty"`
	DatasetVersionID    string         `json:"datasetVersionId,omitempty"`
	EvaluatorVersionID  string         `json:"evaluatorVersionId,omitempty"`
	EvidenceCompletedAt *time.Time     `json:"evidenceCompletedAt,omitempty"`
	Checks              []ReleaseCheck `json:"checks"`
	Passed              bool           `json:"passed"`
	CheckedAt           time.Time      `json:"checkedAt"`
}

type ReleasePreflight struct {
	DeploymentAliasID         DeploymentAliasID `json:"-"`
	DeploymentID              DeploymentID      `json:"deploymentId"`
	Alias                     string            `json:"alias"`
	AgentVersionID            string            `json:"agentVersionId"`
	CurrentAgentVersionID     string            `json:"currentAgentVersionId,omitempty"`
	ExpectedReleaseRevisionID ReleaseRevisionID `json:"expectedReleaseRevisionId"`
	Evaluation                ReleaseEvaluation `json:"evaluation"`
}

// ReleaseDecision is append-only, including rejections which do not move an alias.
type ReleaseDecision struct {
	ID                ReleaseDecisionID `json:"id"`
	DeploymentID      DeploymentID      `json:"deploymentId"`
	Alias             string            `json:"alias"`
	AgentVersionID    string            `json:"agentVersionId"`
	ReleaseRevisionID ReleaseRevisionID `json:"releaseRevisionId,omitempty"`
	Result            string            `json:"result"`
	Actor             string            `json:"actor"`
	Reason            string            `json:"reason"`
	Evaluation        ReleaseEvaluation `json:"evaluation"`
}

type ReleaseDecisionPage struct {
	Items      []ReleaseDecision `json:"items"`
	NextCursor int64             `json:"nextCursor,omitempty"`
}
