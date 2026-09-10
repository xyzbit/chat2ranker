package domain

import (
	"encoding/json"
	"time"
)

type APICredentialID string
type ConversationID string
type ResponseID string

const (
	ScopeResponsesCreate  = "responses:create"
	ScopeResponsesRead    = "responses:read"
	ScopeResponsesCancel  = "responses:cancel"
	ScopeResponsesApprove = "responses:approve"

	ResponseQueued         = "queued"
	ResponseInProgress     = "in_progress"
	ResponseCompleted      = "completed"
	ResponseFailed         = "failed"
	ResponseCancelled      = "cancelled"
	ResponseRequiresAction = "requires_action"
)

// APICredential authorizes public serving operations for selected Deployments.
// SecretHash never crosses the repository boundary through JSON.
type APICredential struct {
	ID            APICredentialID `json:"id"`
	TenantID      string          `json:"tenantId"`
	Name          string          `json:"name"`
	KeyPrefix     string          `json:"keyPrefix"`
	SecretHash    string          `json:"-"`
	Scopes        []string        `json:"scopes"`
	DeploymentIDs []DeploymentID  `json:"deploymentIds"`
	CreatedAt     time.Time       `json:"createdAt"`
	RevokedAt     *time.Time      `json:"revokedAt,omitempty"`
}

type ResponseUsage struct {
	InputTokens     int64 `json:"inputTokens"`
	OutputTokens    int64 `json:"outputTokens"`
	ReasoningTokens int64 `json:"reasoningTokens"`
	TotalTokens     int64 `json:"totalTokens"`
}

type ResponseFailure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ResponseTextFormat is the accepted text-output requirement. Schema is set
// only for the json_schema format and is validated before Response admission.
type ResponseTextFormat struct {
	Type        string          `json:"type"`
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Schema      json.RawMessage `json:"schema,omitempty"`
}

// ResponseInputItem is one normalized user input. Binary content is frozen at
// admission; SourceDigest identifies the caller source without retaining URLs.
type ResponseInputItem struct {
	Type          string `json:"type"`
	Text          string `json:"text,omitempty"`
	Name          string `json:"name,omitempty"`
	MediaType     string `json:"mediaType,omitempty"`
	Detail        string `json:"detail,omitempty"`
	SourceDigest  string `json:"sourceDigest,omitempty"`
	ContentDigest string `json:"contentDigest,omitempty"`
	Data          []byte `json:"data,omitempty"`
	CallID        string `json:"callId,omitempty"`
	Output        string `json:"output,omitempty"`
}

// ResponseFunctionTool is one caller-owned function available to a Response.
// Its JSON Schema is frozen with the public record before execution.
type ResponseFunctionTool struct {
	Type            string          `json:"type"`
	Name            string          `json:"name"`
	Description     string          `json:"description,omitempty"`
	Parameters      json.RawMessage `json:"parameters"`
	Strict          bool            `json:"strict"`
	RequireApproval bool            `json:"require_approval,omitempty"`
}

// ResponseToolCall is one validated caller function request emitted by the
// Agent. Arguments contains exactly one JSON object matching Parameters.
type ResponseToolCall struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	Arguments        json.RawMessage `json:"arguments"`
	ApprovalRequired bool            `json:"approvalRequired,omitempty"`
	ApprovalStatus   string          `json:"approvalStatus,omitempty"`
}

// Conversation pins one ordered Response chain to a Deployment and immutable
// Agent version. LastResponseID is the compare-and-swap head for continuation.
type Conversation struct {
	ID                ConversationID    `json:"id"`
	TenantID          string            `json:"tenantId"`
	DeploymentID      DeploymentID      `json:"deploymentId"`
	AgentVersionID    string            `json:"agentVersionId"`
	ReleaseRevisionID ReleaseRevisionID `json:"releaseRevisionId,omitempty"`
	LastResponseID    ResponseID        `json:"lastResponseId,omitempty"`
	CreatedAt         time.Time         `json:"createdAt"`
	UpdatedAt         time.Time         `json:"updatedAt"`
}

// ServingResponse pins the accepted public request to one exact Agent version
// before dispatch. Alias movement cannot change an active or replayed Response.
type ServingResponse struct {
	ID                 ResponseID             `json:"id"`
	TenantID           string                 `json:"tenantId"`
	CredentialID       APICredentialID        `json:"credentialId"`
	DeploymentID       DeploymentID           `json:"deploymentId"`
	DeploymentName     string                 `json:"deploymentName"`
	RequestedReference string                 `json:"requestedReference"`
	ResolutionKind     string                 `json:"resolutionKind"`
	ReleaseRevisionID  ReleaseRevisionID      `json:"releaseRevisionId,omitempty"`
	AgentVersionID     string                 `json:"agentVersionId"`
	ConversationID     ConversationID         `json:"conversationId"`
	PreviousResponseID ResponseID             `json:"previousResponseId,omitempty"`
	IdempotencyKey     string                 `json:"-"`
	RequestHash        string                 `json:"-"`
	Input              string                 `json:"-"`
	InputItems         []ResponseInputItem    `json:"-"`
	CallerTools        []ResponseFunctionTool `json:"-"`
	ToolCall           *ResponseToolCall      `json:"-"`
	Instructions       string                 `json:"-"`
	Metadata           map[string]string      `json:"metadata"`
	TextFormat         ResponseTextFormat     `json:"textFormat"`
	Background         bool                   `json:"background"`
	Status             string                 `json:"status"`
	ExecutionID        string                 `json:"executionId,omitempty"`
	OutputItemID       string                 `json:"outputItemId"`
	OutputText         string                 `json:"outputText,omitempty"`
	Usage              ResponseUsage          `json:"usage"`
	Cost               float64                `json:"cost"`
	CostKnown          bool                   `json:"costKnown"`
	Failure            *ResponseFailure       `json:"failure,omitempty"`
	CreatedAt          time.Time              `json:"createdAt"`
	StartedAt          *time.Time             `json:"startedAt,omitempty"`
	CompletedAt        *time.Time             `json:"completedAt,omitempty"`
}

// ResponseEvent is one durable Responses-compatible event payload. Sequence is
// monotonic within a Response and becomes the SSE event ID.
type ResponseEvent struct {
	Sequence   int64           `json:"sequence"`
	ResponseID ResponseID      `json:"responseId"`
	Type       string          `json:"type"`
	Data       json.RawMessage `json:"data"`
	CreatedAt  time.Time       `json:"createdAt"`
}

func IsResponseTerminal(status string) bool {
	return status == ResponseCompleted || status == ResponseFailed || status == ResponseCancelled || status == ResponseRequiresAction
}
