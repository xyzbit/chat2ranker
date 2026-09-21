package domain

import "time"

// AgentEvaluationSummary identifies the frozen inputs of a completed or active Run.
type AgentEvaluationSummary struct {
	ID             string    `json:"id"`
	AgentVersionID string    `json:"agentVersionId"`
	DatasetName    string    `json:"datasetName"`
	DatasetVersion int       `json:"datasetVersion"`
	Total          int       `json:"total"`
	Passed         int       `json:"passed"`
	Status         string    `json:"status"`
	Cost           float64   `json:"cost"`
	CostKnown      bool      `json:"costKnown"`
	CreatedAt      time.Time `json:"createdAt"`
}

// ResponseListFilter restricts operator history to a tenant, optional deployment
// and status. Before is an opaque Response identifier for descending keyset pagination.
type ResponseListFilter struct {
	TenantID       string
	DeploymentID   DeploymentID
	Status         string
	Query          string
	ConversationID ConversationID
	Before         ResponseID
	Limit          int
}

// ResponsePage excludes inputs, outputs, and events from the list projection.
// Those fields are available only when inspecting one Response.
type ResponsePage struct {
	Items      []ServingResponse `json:"items"`
	NextCursor ResponseID        `json:"nextCursor,omitempty"`
}
