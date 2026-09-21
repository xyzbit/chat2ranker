package domain

import "encoding/json"

// InvocationID identifies a logical candidate or criterion call, independent of
// HTTP attempts and process restarts. A user rerun owns new Trial identifiers.
type InvocationID string

// EvaluationInvocation stores credential-free dispatch and the original outcome.
// Sequence advances in the same transaction as projected Run events.
type EvaluationInvocation struct {
	ID                InvocationID
	RunID             string
	TrialID           string
	Request           json.RawMessage
	ExecutionID       string
	Result            json.RawMessage
	Sequence          int64
	SubmissionStarted bool
}
