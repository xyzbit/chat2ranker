package domain

import "time"

// AgentEvaluationDefaults reference generic Dataset families; new drafts resolve their latest version.
type AgentEvaluationDefaults struct {
	Revision             int64  `json:"revision"`
	DevelopmentDatasetID string `json:"developmentDatasetId"`
	RegressionDatasetID  string `json:"regressionDatasetId"`
}

type EvaluationEvidence struct {
	RunID              string     `json:"runId"`
	DatasetVersionID   string     `json:"datasetVersionId"`
	EvaluatorVersionID string     `json:"evaluatorVersionId"`
	EvaluatorDigest    string     `json:"evaluatorDigest"`
	AgentVersionID     string     `json:"agentVersionId"`
	TrialCount         int        `json:"trialCount"`
	Passed             int        `json:"passed"`
	ValidTrials        int        `json:"validTrials"`
	ScheduledTrials    int        `json:"scheduledTrials"`
	Cost               float64    `json:"cost"`
	CostKnown          bool       `json:"costKnown"`
	DurationMs         int64      `json:"durationMs"`
	CompletedAt        *time.Time `json:"completedAt"`
}

type ReleaseDraft struct {
	DeploymentID                DeploymentID        `json:"deploymentId"`
	Alias                       string              `json:"alias"`
	AgentVersionID              string              `json:"agentVersionId"`
	CurrentAgentVersionID       string              `json:"currentAgentVersionId"`
	ExpectedReleaseRevisionID   ReleaseRevisionID   `json:"expectedReleaseRevisionId"`
	Policy                      ReleasePolicy       `json:"policy"`
	DevelopmentDatasetVersionID string              `json:"developmentDatasetVersionId"`
	RegressionDatasetVersionID  string              `json:"regressionDatasetVersionId"`
	CurrentEvidence             *EvaluationEvidence `json:"currentEvidence,omitempty"`
	CandidateEvidence           *EvaluationEvidence `json:"candidateEvidence,omitempty"`
	Comparable                  bool                `json:"comparable"`
}

type ReleaseTaskID string

// ReleaseTask freezes review evidence and owns a regression Run independently of chat selection.
type ReleaseTask struct {
	ID ReleaseTaskID `json:"id"`
	ReleaseDraft
	IdempotencyKey    string             `json:"-"`
	RequestHash       string             `json:"-"`
	ExperimentID      string             `json:"experimentId,omitempty"`
	ReviewCommandID   string             `json:"reviewCommandId,omitempty"`
	RunRegression     bool               `json:"runRegression"`
	TrialCount        int                `json:"trialCount"`
	RunID             string             `json:"runId,omitempty"`
	Status            string             `json:"status"`
	Actor             string             `json:"actor"`
	Reason            string             `json:"reason,omitempty"`
	Error             string             `json:"error,omitempty"`
	Evaluation        *ReleaseEvaluation `json:"evaluation,omitempty"`
	ReleaseRevisionID ReleaseRevisionID  `json:"releaseRevisionId,omitempty"`
	CreatedAt         time.Time          `json:"createdAt"`
	UpdatedAt         time.Time          `json:"updatedAt"`
}
