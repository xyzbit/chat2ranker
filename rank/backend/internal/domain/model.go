package domain

import (
	"encoding/json"
	"time"

	"github.com/xyzbit/chat2ranker/execution/backend/contract"
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
	ID          string           `json:"id"`
	FamilyID    string           `json:"familyId"`
	Name        string           `json:"name"`
	Version     int              `json:"version"`
	Source      string           `json:"source"`
	Description string           `json:"description"`
	Schema      json.RawMessage  `json:"schema,omitempty"`
	Rubric      json.RawMessage  `json:"rubric,omitempty"`
	Evaluator   EvaluatorVersion `json:"evaluator"`
	Cases       []Case           `json:"cases"`
	CreatedAt   time.Time        `json:"createdAt"`
	CaseCount   int              `json:"caseCount"`
}

type DeterministicCriterion struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Operator string `json:"operator"`
	Value    any    `json:"value,omitempty"`
	Required bool   `json:"required"`
}

type RubricCriterion struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Weight      float64 `json:"weight"`
	Threshold   float64 `json:"threshold"`
	Critical    bool    `json:"critical"`
}

type JudgeConfig struct {
	Harness string `json:"harness"`
	Model   string `json:"model,omitempty"`
}

type PassPolicy struct {
	RubricThreshold float64 `json:"rubricThreshold"`
}

// EvaluatorVersion is the immutable scoring policy bound to one dataset
// version. A Run freezes a copy before any Trial starts.
type EvaluatorVersion struct {
	ID            string                   `json:"id"`
	Version       int                      `json:"version"`
	Name          string                   `json:"name"`
	Deterministic []DeterministicCriterion `json:"deterministic"`
	Rubric        []RubricCriterion        `json:"rubric"`
	Judge         JudgeConfig              `json:"judge"`
	PassPolicy    PassPolicy               `json:"passPolicy"`
	CreatedAt     time.Time                `json:"createdAt"`
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
	ID                string    `json:"id"`
	FamilyID          string    `json:"familyId"`
	Name              string    `json:"name"`
	Handle            string    `json:"handle"`
	Version           int       `json:"version"`
	RunnerType        string    `json:"runnerType"`
	Description       string    `json:"description"`
	Model             string    `json:"model"`
	ModelConnectionID string    `json:"modelConnectionId,omitempty"`
	Preset            string    `json:"preset,omitempty"`
	SystemPrompt      string    `json:"systemPrompt,omitempty"`
	Tools             []string  `json:"tools"`
	Skills            []string  `json:"skills"`
	CreatedAt         time.Time `json:"createdAt"`
}

type RuntimeAvailability struct {
	Available  bool   `json:"available"`
	Installed  bool   `json:"installed"`
	Configured bool   `json:"configured"`
	Label      string `json:"label,omitempty"`
	Reason     string `json:"reason,omitempty"`
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
	RunCount   int                   `json:"runCount"`
	LatestRun  *RunSummary           `json:"latestRun,omitempty"`
	RunSummary *ExperimentRunSummary `json:"runSummary,omitempty"`
}

type ExperimentRunSummary struct {
	Completed     int     `json:"completed"`
	Passed        int     `json:"passed"`
	Total         int     `json:"total"`
	Cost          float64 `json:"cost"`
	CostKnown     bool    `json:"costKnown"`
	CostEstimated bool    `json:"costEstimated"`
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
	CostEstimated    bool          `json:"costEstimated"`
	Score            float64       `json:"score"`
	Output           string        `json:"output"`
	Reason           string        `json:"reason"`
	DurationMs       int64         `json:"durationMs,omitempty"`
	ExecutionID      string        `json:"executionId,omitempty"`
	JudgeExecutionID string        `json:"judgeExecutionId,omitempty"`
	Usage            Usage         `json:"usage,omitempty"`
	Artifacts        []ArtifactRef `json:"artifacts,omitempty"`
	TrialCount       int           `json:"trialCount"`
	ValidTrials      int           `json:"validTrials"`
	PassCount        int           `json:"passCount"`
	PassRate         int           `json:"passRate"`
	Reliable         bool          `json:"reliable"`
	CandidateCost    float64       `json:"candidateCost"`
	EvaluationCost   float64       `json:"evaluationCost"`
	Trials           []TrialResult `json:"trials"`
}

type CriterionResult struct {
	CriterionID string   `json:"criterionId"`
	Kind        string   `json:"kind"`
	Name        string   `json:"name"`
	Status      string   `json:"status"`
	Passed      *bool    `json:"passed,omitempty"`
	Score       *float64 `json:"score,omitempty"`
	Reason      string   `json:"reason"`
	Required    bool     `json:"required,omitempty"`
	Critical    bool     `json:"critical,omitempty"`
	Weight      float64  `json:"weight,omitempty"`
	ExecutionID string   `json:"executionId,omitempty"`
}

type TrialResult struct {
	ID                   string            `json:"id"`
	RunID                string            `json:"runId"`
	CaseID               string            `json:"caseId"`
	TrialIndex           int               `json:"trialIndex"`
	Status               string            `json:"status"`
	FailureClass         string            `json:"failureClass,omitempty"`
	Valid                bool              `json:"valid"`
	Passed               bool              `json:"passed"`
	Score                float64           `json:"score"`
	Output               string            `json:"output"`
	Reason               string            `json:"reason"`
	CandidateCost        float64           `json:"candidateCost"`
	EvaluationCost       float64           `json:"evaluationCost"`
	Cost                 float64           `json:"cost"`
	CostKnown            bool              `json:"costKnown"`
	CostEstimated        bool              `json:"costEstimated"`
	DurationMs           int64             `json:"durationMs"`
	Attempts             int               `json:"attempts"`
	CandidateExecutionID string            `json:"candidateExecutionId,omitempty"`
	JudgeExecutionIDs    []string          `json:"judgeExecutionIds"`
	Usage                Usage             `json:"usage,omitempty"`
	Artifacts            []ArtifactRef     `json:"artifacts,omitempty"`
	Criteria             []CriterionResult `json:"criteria"`
	CreatedAt            time.Time         `json:"createdAt"`
	StartedAt            *time.Time        `json:"startedAt,omitempty"`
	CompletedAt          *time.Time        `json:"completedAt,omitempty"`
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

type RunEvent struct {
	ID               int64     `json:"sequence"`
	RunID            string    `json:"-"`
	Type             string    `json:"type"`
	At               time.Time `json:"at"`
	CaseID           string    `json:"caseId,omitempty"`
	TrialID          string    `json:"trialId,omitempty"`
	TrialIndex       int       `json:"trialIndex,omitempty"`
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
	ID                 string           `json:"id"`
	ExperimentID       string           `json:"experimentId"`
	GroupID            string           `json:"groupId,omitempty"`
	IdempotencyKey     string           `json:"-"`
	Status             string           `json:"status"`
	DatasetSnapshot    DatasetVersion   `json:"datasetSnapshot"`
	AgentSnapshot      AgentVersion     `json:"agentSnapshot"`
	EvaluatorSnapshot  EvaluatorVersion `json:"evaluatorSnapshot"`
	TrialCount         int              `json:"trialCount"`
	Concurrency        int              `json:"concurrency"`
	CreatedAt          time.Time        `json:"createdAt"`
	StartedAt          *time.Time       `json:"startedAt,omitempty"`
	CompletedAt        *time.Time       `json:"completedAt,omitempty"`
	DurationMs         int64            `json:"durationMs"`
	Passed             int              `json:"passed"`
	Total              int              `json:"total"`
	PassRate           int              `json:"passRate"`
	ScheduledTrials    int              `json:"scheduledTrials"`
	CompletedTrials    int              `json:"completedTrials"`
	ValidTrials        int              `json:"validTrials"`
	InfraFailures      int              `json:"infraFailures"`
	GradingFailures    int              `json:"gradingFailures"`
	ReliableCases      int              `json:"reliableCases"`
	CaseCount          int              `json:"caseCount"`
	PassHat3           float64          `json:"passHat3"`
	EvaluationComplete bool             `json:"evaluationComplete"`
	Cost               float64          `json:"cost"`
	CandidateCost      float64          `json:"candidateCost"`
	EvaluationCost     float64          `json:"evaluationCost"`
	CostKnown          bool             `json:"costKnown"`
	CostEstimated      bool             `json:"costEstimated"`
	Error              string           `json:"error,omitempty"`
	Results            []CaseResult     `json:"results"`
	Events             []RunEvent       `json:"events"`
}

// RunGroup is a comparison request. Each child Run still freezes and executes
// exactly one Agent version, so runners and executors remain single-task APIs.
type RunGroup struct {
	ID               string    `json:"id"`
	ExperimentID     string    `json:"experimentId"`
	IdempotencyKey   string    `json:"-"`
	DatasetVersionID string    `json:"datasetVersionId"`
	AgentVersionIDs  []string  `json:"agentVersionIds"`
	RunIDs           []string  `json:"runIds"`
	TrialCount       int       `json:"trialCount"`
	Status           string    `json:"status"`
	CreatedAt        time.Time `json:"createdAt"`
}

type RunSummary struct {
	ID            string     `json:"id"`
	Status        string     `json:"status"`
	Passed        int        `json:"passed"`
	Total         int        `json:"total"`
	PassRate      int        `json:"passRate"`
	TrialCount    int        `json:"trialCount"`
	ReliableCases int        `json:"reliableCases"`
	CaseCount     int        `json:"caseCount"`
	Cost          float64    `json:"cost"`
	CostKnown     bool       `json:"costKnown"`
	CostEstimated bool       `json:"costEstimated"`
	DurationMs    int64      `json:"durationMs"`
	CreatedAt     time.Time  `json:"createdAt"`
	CompletedAt   *time.Time `json:"completedAt,omitempty"`
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

type RunTrial struct {
	ID          string
	RunID       string
	ItemID      string
	CaseID      string
	TrialIndex  int
	Ordinal     int
	Status      string
	ResultKey   string
	Result      *TrialResult
	CreatedAt   time.Time
	StartedAt   *time.Time
	CompletedAt *time.Time
}

type Bootstrap struct {
	Datasets         []DatasetAsset                 `json:"datasets"`
	Agents           []AgentAsset                   `json:"agents"`
	Experiments      []ExperimentSummary            `json:"experiments"`
	ModelConnections []contract.ModelConnection     `json:"modelConnections"`
	ModelCatalog     []contract.ModelProvider       `json:"modelCatalog"`
	SystemModels     []contract.SystemModelBinding  `json:"systemModels"`
	Runtimes         map[string]RuntimeAvailability `json:"runtimes"`
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

	TrialQueued    = "queued"
	TrialRunning   = "running"
	TrialComplete  = "complete"
	TrialCancelled = "cancelled"

	FailureQuality = "quality_failed"
	FailureInfra   = "infra_failed"
	FailureGrading = "grading_failed"
)

func IsActiveRun(status string) bool {
	return status == RunQueued || status == RunPreparing || status == RunRunning || status == RunScoring
}
