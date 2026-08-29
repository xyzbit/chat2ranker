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
	ProtocolVersion   int               `json:"protocolVersion"`
	Kind              string            `json:"kind"`
	Harness           string            `json:"harness"`
	ModelConnectionID string            `json:"modelConnectionId,omitempty"`
	Model             string            `json:"model,omitempty"`
	Preset            string            `json:"preset,omitempty"`
	SystemPrompt      string            `json:"systemPrompt,omitempty"`
	Tools             []string          `json:"tools,omitempty"`
	Skills            []string          `json:"skills,omitempty"`
	Prompt            string            `json:"prompt"`
	Expected          map[string]any    `json:"expected,omitempty"`
	CandidateOutput   string            `json:"candidateOutput,omitempty"`
	TimeoutMs         int64             `json:"timeoutMs,omitempty"`
	Metadata          map[string]string `json:"metadata,omitempty"`
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
	Output           string           `json:"output,omitempty"`
	Passed           bool             `json:"passed,omitempty"`
	Score            float64          `json:"score,omitempty"`
	Reason           string           `json:"reason,omitempty"`
	Cost             float64          `json:"cost,omitempty"`
	CostKnown        bool             `json:"costKnown"`
	CostSource       string           `json:"costSource,omitempty"`
	ResolvedProvider string           `json:"resolvedProvider,omitempty"`
	ResolvedModel    string           `json:"resolvedModel,omitempty"`
	Pricing          *PricingSnapshot `json:"pricing,omitempty"`
	DurationMs       int64            `json:"durationMs"`
	Usage            Usage            `json:"usage,omitempty"`
	Artifacts        []ArtifactRef    `json:"artifacts,omitempty"`
	Native           json.RawMessage  `json:"native,omitempty"`
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
	Available  bool   `json:"available"`
	Installed  bool   `json:"installed"`
	Configured bool   `json:"configured"`
	Label      string `json:"label,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

const (
	ProtocolOpenAIChat      = "openai-chat-completions"
	ProtocolOpenAIResponses = "openai-responses"
	ProtocolAnthropic       = "anthropic-messages"
	CostSourceProvider      = "provider"
	CostSourceConnection    = "connection"
	CostSourceCatalog       = "catalog"
)

// ModelPrice contains USD prices per one million tokens. Zero means that the
// provider does not charge for or does not expose that token class.
type ModelPrice struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead,omitempty"`
	CacheWrite float64 `json:"cacheWrite,omitempty"`
}

// CatalogModel is local default metadata. A connection may inherit its price
// or override it without mutating the built-in catalog.
type CatalogModel struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Price     ModelPrice `json:"price"`
	PriceNote string     `json:"priceNote,omitempty"`
	SourceURL string     `json:"sourceUrl,omitempty"`
	UpdatedAt string     `json:"updatedAt"`
}

// ModelProvider is one built-in connection template and its known models.
type ModelProvider struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	BaseURL   string         `json:"baseUrl"`
	Protocol  string         `json:"protocol"`
	SourceURL string         `json:"sourceUrl"`
	Models    []CatalogModel `json:"models"`
}

// PricingSnapshot records the immutable rate used for one execution.
type PricingSnapshot struct {
	Provider string     `json:"provider"`
	Model    string     `json:"model"`
	Source   string     `json:"source"`
	Price    ModelPrice `json:"price"`
}

// ModelConnection is public provider metadata. CredentialRef is internal and
// APIKey is accepted only on write requests; neither is serialized.
type ModelConnection struct {
	ID             string                `json:"id"`
	Name           string                `json:"name"`
	Provider       string                `json:"provider"`
	Protocol       string                `json:"protocol"`
	BaseURL        string                `json:"baseUrl"`
	DefaultModel   string                `json:"defaultModel,omitempty"`
	Models         []string              `json:"models"`
	Prices         map[string]ModelPrice `json:"prices,omitempty"`
	Status         string                `json:"status"`
	StatusMessage  string                `json:"statusMessage,omitempty"`
	HasCredential  bool                  `json:"hasCredential"`
	LastVerifiedAt *time.Time            `json:"lastVerifiedAt,omitempty"`
	CreatedAt      time.Time             `json:"createdAt"`
	UpdatedAt      time.Time             `json:"updatedAt"`
	CredentialRef  string                `json:"-"`
	APIKey         string                `json:"-"`
}

type ModelConnectionInput struct {
	Name         string                `json:"name"`
	Provider     string                `json:"provider,omitempty"`
	Protocol     string                `json:"protocol"`
	BaseURL      string                `json:"baseUrl"`
	APIKey       string                `json:"apiKey,omitempty"`
	DefaultModel string                `json:"defaultModel,omitempty"`
	Prices       map[string]ModelPrice `json:"prices,omitempty"`
}

const (
	SystemModelControl = "control"
	SystemModelJudge   = "judge"
)

// SystemModelBinding is a stable platform role backed by one verified model
// connection. Credentials remain owned by executiond and are never serialized.
type SystemModelBinding struct {
	Role         string          `json:"role"`
	ConnectionID string          `json:"connectionId"`
	Model        string          `json:"model"`
	Connection   ModelConnection `json:"connection"`
	UpdatedAt    time.Time       `json:"updatedAt"`
}

type SystemModelBindingInput struct {
	ConnectionID string `json:"connectionId"`
	Model        string `json:"model,omitempty"`
}

// SystemModelRuntime is returned only by executiond's authenticated internal
// endpoint to a trusted runtime host.
type SystemModelRuntime struct {
	Binding    SystemModelBinding `json:"binding"`
	Credential string             `json:"credential"`
}

type ArtifactContent struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
}

func IsTerminal(status string) bool {
	return status == StatusCompleted || status == StatusFailed || status == StatusCancelled
}
