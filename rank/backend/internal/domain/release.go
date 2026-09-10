package domain

import "time"

type DeploymentID string
type DeploymentAliasID string
type ReleaseRevisionID string

type DeploymentPolicy struct {
	RequestsPerMinute      int   `json:"requestsPerMinute"`
	MaxConcurrentResponses int   `json:"maxConcurrentResponses"`
	ExecutionTimeoutMs     int64 `json:"executionTimeoutMs"`
	AllowInstructions      bool  `json:"allowInstructions"`
	AllowCallerTools       bool  `json:"allowCallerTools"`
}

const (
	ReleasePolicyPassed         = "passed"
	ReleasePolicyManualOverride = "manual_override"
	ReleasePolicySystem         = "system"

	ResolutionExact        = "exact"
	ResolutionAlias        = "alias"
	ResolutionConversation = "conversation"
)

// Deployment is a tenant-scoped public serving target for one Agent family.
// Its policy remains stable while aliases move between immutable versions.
type Deployment struct {
	ID            DeploymentID      `json:"id"`
	TenantID      string            `json:"tenantId"`
	AgentFamilyID string            `json:"agentFamilyId"`
	Name          string            `json:"name"`
	PolicyDigest  string            `json:"policyDigest"`
	Policy        DeploymentPolicy  `json:"policy"`
	Aliases       []DeploymentAlias `json:"aliases,omitempty"`
	CreatedAt     time.Time         `json:"createdAt"`
}

// DeploymentAlias is a named routing lane whose head identifies the current
// append-only release revision.
type DeploymentAlias struct {
	ID                       DeploymentAliasID `json:"id"`
	DeploymentID             DeploymentID      `json:"deploymentId"`
	Name                     string            `json:"name"`
	CurrentReleaseRevisionID ReleaseRevisionID `json:"currentReleaseRevisionId,omitempty"`
	CurrentRelease           *ReleaseRevision  `json:"currentRelease,omitempty"`
	CreatedAt                time.Time         `json:"createdAt"`
	UpdatedAt                time.Time         `json:"updatedAt"`
}

// ReleaseRevision is one immutable alias movement. Rollback appends another
// revision instead of changing or deleting prior history.
type ReleaseRevision struct {
	ID                        ReleaseRevisionID `json:"id"`
	DeploymentAliasID         DeploymentAliasID `json:"deploymentAliasId"`
	Revision                  int64             `json:"revision"`
	AgentVersionID            string            `json:"agentVersionId"`
	PreviousReleaseRevisionID ReleaseRevisionID `json:"previousReleaseRevisionId,omitempty"`
	EvidenceRunID             string            `json:"evidenceRunId,omitempty"`
	PolicyResult              string            `json:"policyResult"`
	Actor                     string            `json:"actor"`
	Reason                    string            `json:"reason"`
	CreatedAt                 time.Time         `json:"createdAt"`
}

// ReleaseHistoryItem includes the previous Agent version without resolving a moving alias.
type ReleaseHistoryItem struct {
	ReleaseRevision
	PreviousAgentVersionID string `json:"previousAgentVersionId,omitempty"`
}

type ReleaseHistoryPage struct {
	Items      []ReleaseHistoryItem `json:"items"`
	NextCursor int64                `json:"nextCursor,omitempty"`
}

// DeploymentResolution freezes the exact Agent version selected for one
// accepted reference. Exact selectors do not have a release revision.
type DeploymentResolution struct {
	Deployment      Deployment       `json:"deployment"`
	RequestedRef    string           `json:"requestedRef"`
	Kind            string           `json:"kind"`
	Alias           *DeploymentAlias `json:"alias,omitempty"`
	ReleaseRevision *ReleaseRevision `json:"releaseRevision,omitempty"`
	AgentVersion    AgentVersion     `json:"agentVersion"`
}
