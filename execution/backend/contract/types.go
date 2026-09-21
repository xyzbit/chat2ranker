// Package contract defines the versioned Execution Service HTTP payloads.
package contract

import (
	"encoding/json"
	"time"
)

const ProtocolVersion = 2

const (
	KindAgent = "agent"
	KindJudge = "judge"

	StatusQueued     = "queued"
	StatusRunning    = "running"
	StatusCancelling = "cancelling"
	StatusCompleted  = "completed"
	StatusFailed     = "failed"
	StatusCancelled  = "cancelled"
)

// Spec is an immutable request for one harness invocation.
type Spec struct {
	// MaxModelRequests optionally tightens the service's DSH invocation budget.
	MaxModelRequests  int            `json:"maxModelRequests,omitempty"`
	ProtocolVersion   int            `json:"protocolVersion"`
	Kind              string         `json:"kind"`
	Harness           string         `json:"harness"`
	ModelConnectionID string         `json:"modelConnectionId,omitempty"`
	Model             string         `json:"model,omitempty"`
	Preset            string         `json:"preset,omitempty"`
	SystemPrompt      string         `json:"systemPrompt,omitempty"`
	Tools             []string       `json:"tools,omitempty"`
	Skills            []string       `json:"skills,omitempty"`
	Prompt            string         `json:"prompt,omitempty"`
	Messages          []Message      `json:"messages,omitempty"`
	Expected          map[string]any `json:"expected,omitempty"`
	CandidateOutput   string         `json:"candidateOutput,omitempty"`
	// QueueTimeoutMs shortens the service queue-wait limit; zero uses its default.
	QueueTimeoutMs   int64             `json:"queueTimeoutMs,omitempty"`
	TimeoutMs        int64             `json:"timeoutMs,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
	StructuredOutput *StructuredOutput `json:"structuredOutput,omitempty"`
	InputAttachments []InputAttachment `json:"inputAttachments,omitempty"`
}

// StructuredOutput requires one JSON value matching Schema. Execution
// workers validate the final Harness output before reporting success.
type StructuredOutput struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Schema      json.RawMessage `json:"schema"`
}

// InputAttachment is one immutable user-supplied image or file. Data is
// content-addressed and materialized into the private worker workspace.
type InputAttachment struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Name      string `json:"name"`
	MediaType string `json:"mediaType"`
	Detail    string `json:"detail,omitempty"`
	Data      []byte `json:"data"`
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
	SHA256      string `json:"sha256,omitempty"`
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
	Structured       json.RawMessage  `json:"structured,omitempty"`
}

// RuntimeAllocation records creation intent before contacting a remote service.
// Handle remains empty if the creation response was lost. It never holds credentials.
type RuntimeAllocation struct {
	Endpoint  string    `json:"endpoint"`
	Scope     string    `json:"scope"`
	Image     string    `json:"image"`
	ExpiresAt time.Time `json:"expiresAt"`
	Handle    string    `json:"handle,omitempty"`
}

// Execution is the durable control-plane record for one harness invocation.
type Execution struct {
	ModelQuota *ModelQuotaPlan `json:"modelQuota,omitempty"`
	// MaxModelRequests is frozen at admission; zero means the adapter lacks support.
	MaxModelRequests int          `json:"maxModelRequests,omitempty"`
	RuntimePlan      *RuntimePlan `json:"runtimePlan,omitempty"`
	// RuntimeCleanupPending retains capacity until the executor confirms shutdown;
	// a failed business outcome alone does not release this obligation.
	Runtime               *RuntimeAllocation `json:"runtime,omitempty"`
	RuntimeCleanupPending bool               `json:"runtimeCleanupPending,omitempty"`
	ID                    string             `json:"id"`
	IdempotencyKey        string             `json:"-"`
	Status                string             `json:"status"`
	Executor              string             `json:"executor"`
	Attempt               int                `json:"attempt"`
	ExternalHandle        string             `json:"externalHandle,omitempty"`
	WorkerVersion         string             `json:"workerVersion"`
	RuntimeTimeoutMs      int64              `json:"runtimeTimeoutMs"`
	CleanupGraceMs        int64              `json:"cleanupGraceMs"`
	Spec                  Spec               `json:"spec"`
	// Result can contain observed output, usage and confirmed artifacts before
	// terminal success, including on failure. Status alone determines completion.
	Result *Result `json:"result,omitempty"`
	Error  string  `json:"error,omitempty"`
	// QueueExpiresAt is frozen at admission and never extended by delivery retries.
	QueueExpiresAt time.Time  `json:"queueExpiresAt"`
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
	ModelQuota         bool   `json:"modelQuota,omitempty"`
	ModelRequestBudget bool   `json:"modelRequestBudget,omitempty"`
	StructuredHistory  bool   `json:"structuredHistory"`
	Available          bool   `json:"available"`
	Installed          bool   `json:"installed"`
	Configured         bool   `json:"configured"`
	Label              string `json:"label,omitempty"`
	Reason             string `json:"reason,omitempty"`
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
	Path    string `json:"path"`
	Content string `json:"content"`
	// Encoding is utf-8 for text, or base64 when the returned bytes are not valid UTF-8.
	Encoding  string `json:"encoding"`
	Truncated bool   `json:"truncated"`
}

func IsTerminal(status string) bool {
	return status == StatusCompleted || status == StatusFailed || status == StatusCancelled
}
