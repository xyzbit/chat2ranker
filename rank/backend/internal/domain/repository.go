package domain

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrNotFound          = errors.New("not found")
	ErrConflict          = errors.New("conflict")
	ErrIdempotencyReplay = errors.New("idempotency replay")
	ErrRateLimited       = errors.New("rate limited")
	ErrConcurrencyLimit  = errors.New("concurrency limit")
	ErrConversationBusy  = errors.New("conversation has an active response")
	ErrConversationHead  = errors.New("conversation head changed")
)

type UnitOfWork interface {
	// WithinTx supplies the transaction context and repository. Every operation
	// in fn must use that context; callbacks must not perform external work.
	WithinTx(ctx context.Context, fn func(context.Context, Repository) error) error
}

type DatasetRepository interface {
	ListDatasetFamilies(ctx context.Context) ([]DatasetFamily, error)
	ListDatasetVersions(ctx context.Context, familyID string) ([]DatasetVersion, error)
	GetDatasetVersion(ctx context.Context, id string) (DatasetVersion, error)
	CreateDatasetFamily(ctx context.Context, family DatasetFamily, version DatasetVersion) error
	CreateDatasetVersion(ctx context.Context, version DatasetVersion) error
}

type AgentRepository interface {
	GetAgentEvaluationDefaults(context.Context, string) (AgentEvaluationDefaults, error)
	SaveAgentEvaluationDefaults(context.Context, string, AgentEvaluationDefaults, int64) error
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
	CreateEvaluationInvocation(context.Context, EvaluationInvocation) error
	GetEvaluationInvocation(context.Context, InvocationID) (EvaluationInvocation, error)
	ListEvaluationInvocations(context.Context, string) ([]EvaluationInvocation, error)
	BeginInvocationSubmission(context.Context, InvocationID) error
	SaveInvocationExecution(context.Context, InvocationID, string) error
	CompleteInvocation(context.Context, InvocationID, json.RawMessage) error
	AdvanceInvocationSequence(context.Context, InvocationID, int64, int64) error
	RequestRunCancellation(context.Context, string) error
	AppendRunEventOnce(context.Context, string, RunEvent) error
	ListAgentEvaluationSummaries(ctx context.Context, agentVersionID string) ([]AgentEvaluationSummary, error)
	GetRun(ctx context.Context, id string) (Run, error)
	GetRunByIdempotencyKey(ctx context.Context, experimentID, key string) (Run, error)
	ListRunsByExperiment(ctx context.Context, experimentID string) ([]Run, error)
	ListActiveRuns(ctx context.Context) ([]Run, error)
	ListRunEvents(ctx context.Context, runID string, afterSequence int64) ([]RunEvent, error)
	CreateRun(ctx context.Context, run Run, items []RunItem, trials []RunTrial, created RunEvent) error
	UpdateRunStatus(ctx context.Context, runID, status string, at time.Time, runError string) error
	ListRunItems(ctx context.Context, runID string) ([]RunItem, error)
	ListRunTrials(ctx context.Context, runID string) ([]RunTrial, error)
	ResetActiveRunTrials(ctx context.Context, runID string) error
	ClaimRunTrial(ctx context.Context, runID, trialID string, startedAt time.Time) (bool, error)
	CompleteRunTrial(ctx context.Context, trialID, resultKey string, result TrialResult, completedAt time.Time, events []RunEvent) (bool, error)
	SaveRunItemAggregate(ctx context.Context, itemID string, result CaseResult, completedAt time.Time) error
	FinishRun(ctx context.Context, run Run, message Message, event RunEvent) error
	CancelRun(ctx context.Context, runID string, at time.Time, event RunEvent) error
	AppendRunEvent(ctx context.Context, event RunEvent) error
}

type RunGroupRepository interface {
	GetRunGroup(ctx context.Context, id string) (RunGroup, error)
	GetRunGroupByIdempotencyKey(ctx context.Context, experimentID, key string) (RunGroup, error)
	CreateRunGroup(ctx context.Context, group RunGroup) error
}

type ReleaseRepository interface {
	CreateReleaseTask(context.Context, ReleaseTask) error
	GetReleaseTask(context.Context, ReleaseTaskID) (ReleaseTask, error)
	GetReleaseTaskByKey(context.Context, DeploymentID, string) (ReleaseTask, error)
	ListReleaseTasks(context.Context, DeploymentID, string) ([]ReleaseTask, error)
	SaveReleaseTask(context.Context, ReleaseTask, string) error
	GetReleasePolicy(ctx context.Context, id DeploymentID) (ReleasePolicy, error)
	SaveReleasePolicy(ctx context.Context, id DeploymentID, policy ReleasePolicy, expectedRevision int64) error
	AppendReleaseDecision(ctx context.Context, decision ReleaseDecision) error
	ListReleaseDecisions(ctx context.Context, id DeploymentID, alias string, before int64, limit int) (ReleaseDecisionPage, error)
	ListReleaseHistory(ctx context.Context, aliasID DeploymentAliasID, before int64, limit int) (ReleaseHistoryPage, error)
	CreateDeployment(ctx context.Context, deployment Deployment, aliases []DeploymentAlias) error
	GetDeployment(ctx context.Context, id DeploymentID) (Deployment, error)
	GetDeploymentByName(ctx context.Context, tenantID, name string) (Deployment, error)
	ListDeployments(ctx context.Context, tenantID string) ([]Deployment, error)
	ListDeploymentsByAgentFamily(ctx context.Context, familyID string) ([]Deployment, error)
	ListDeploymentsByAgentVersion(ctx context.Context, tenantID, versionID string) ([]Deployment, error)
	GetDeploymentAlias(ctx context.Context, deploymentID DeploymentID, name string) (DeploymentAlias, error)
	AppendReleaseRevision(ctx context.Context, revision ReleaseRevision, expectedReleaseRevisionID ReleaseRevisionID) (ReleaseRevision, error)
}

type ServingRepository interface {
	ListAPICredentials(ctx context.Context, tenantID string) ([]APICredential, error)
	RevokeAPICredential(ctx context.Context, id APICredentialID, tenantID string, at time.Time) error
	ListResponsesPage(ctx context.Context, filter ResponseListFilter) (ResponsePage, error)
	CreateAPICredential(ctx context.Context, credential APICredential) error
	GetAPICredentialBySecretHash(ctx context.Context, secretHash string) (APICredential, error)
	AdmitResponse(ctx context.Context, response ServingResponse, conversation *Conversation, created ResponseEvent, requestsPerMinute, maxConcurrent int) error
	GetConversation(ctx context.Context, id ConversationID) (Conversation, error)
	GetResponse(ctx context.Context, id ResponseID) (ServingResponse, error)
	GetResponseByIdempotencyKey(ctx context.Context, credentialID APICredentialID, key, tenantID string) (ServingResponse, error)
	ListTrialConversations(ctx context.Context, tenantID string, filter TrialConversationFilter) ([]TrialConversation, error)
	ListConversationResponses(ctx context.Context, conversationID ConversationID) ([]ServingResponse, error)
	ListActiveResponses(ctx context.Context) ([]ServingResponse, error)
	RequestResponseCancellation(ctx context.Context, id ResponseID) error
	UpdateResponseProgress(ctx context.Context, response ServingResponse, expectedSequence int64, events []ResponseEvent) error
	UpdateResponse(ctx context.Context, response ServingResponse, expectedStatus string, events []ResponseEvent) error
	ListResponseEvents(ctx context.Context, responseID ResponseID, afterSequence int64) ([]ResponseEvent, error)
}

type WebhookRepository interface {
	ReplayWebhookDelivery(ctx context.Context, endpointID WebhookEndpointID, deliveryID WebhookDeliveryID, at time.Time) error
	CreateWebhookEndpoint(ctx context.Context, endpoint WebhookEndpoint) error
	GetWebhookEndpoint(ctx context.Context, id WebhookEndpointID) (WebhookEndpoint, error)
	ListWebhookEndpoints(ctx context.Context, tenantID string) ([]WebhookEndpoint, error)
	SetWebhookEndpointEnabled(ctx context.Context, id WebhookEndpointID, enabled bool, updatedAt time.Time) error
	CreateWebhookDelivery(ctx context.Context, delivery WebhookDelivery) error
	ListWebhookDeliveries(ctx context.Context, endpointID WebhookEndpointID) ([]WebhookDelivery, error)
	ListDueWebhookDeliveries(ctx context.Context, now time.Time, limit int) ([]WebhookDelivery, error)
	ClaimWebhookDelivery(ctx context.Context, id WebhookDeliveryID, now, leaseUntil time.Time, leaseToken string) (WebhookDelivery, bool, error)
	CompleteWebhookDelivery(ctx context.Context, id WebhookDeliveryID, leaseToken string, deliveredAt time.Time) error
	RetryWebhookDelivery(ctx context.Context, id WebhookDeliveryID, leaseToken string, nextAttemptAt time.Time, terminal bool, message string) error
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
	RunGroupRepository
	ReleaseRepository
	ServingRepository
	WebhookRepository
}
