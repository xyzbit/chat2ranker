package contract

// ModelQuota groups connections consuming the same provider account allowance.
// Limits count starts and estimated input plus bounded output over a rolling minute.
// Zero disables that dimension; at least one dimension must be enabled.
type ModelQuota struct {
	ID                string   `json:"id"`
	Connections       []string `json:"connections"`
	RequestsPerMinute int64    `json:"requestsPerMinute"`
	TokensPerMinute   int64    `json:"tokensPerMinute"`
	MaxOutputTokens   int64    `json:"maxOutputTokens"`
}

// ModelQuotaPlan fixes group membership and the model output cap at admission.
// The group's shared rates remain operator-controlled; changing them does not
// reset reservations or authorize another execution attempt.
type ModelQuotaPlan struct {
	Group           string `json:"group"`
	MaxOutputTokens int64  `json:"maxOutputTokens"`
}

// ModelQuotaRuntime is transient callback configuration for one invocation.
// Token must be redacted from output and must not be persisted with Execution.
type ModelQuotaRuntime struct {
	Endpoint         string `json:"endpoint"`
	RequestTimeoutMs int64  `json:"requestTimeoutMs"`
	Token            string `json:"token"`
	MaxOutputTokens  int64  `json:"maxOutputTokens"`
}

// ModelPermitRequest names one model attempt inside a running Execution.
// InputTokens is a conservative client estimate, not provider-reported billing.
type ModelPermitRequest struct {
	Sequence    int   `json:"sequence"`
	InputTokens int64 `json:"inputTokens"`
}

// ModelPermit either grants a short start interval or asks the caller to wait.
// A repeated granted sequence never extends its interval or resets quota.
type ModelPermit struct {
	Granted      bool  `json:"granted"`
	ValidForMs   int64 `json:"validForMs,omitempty"`
	RetryAfterMs int64 `json:"retryAfterMs,omitempty"`
}
