// Package jobs defines durable queue payloads. Credentials and prompts remain
// outside River's task arguments.
package jobs

import "github.com/xyzbit/agentkiln/execution/backend/contract"

const (
	Online     = "online"
	Evaluation = "evaluation"
)

// Execution identifies one immutable invocation, independent of delivery attempts.
type Execution struct {
	ExecutionID string `json:"executionId"`
}

func (Execution) Kind() string { return "rank_execution" }

// Queue selects the workload pool from the owning Rank request identity.
func Queue(spec contract.Spec) string {
	if spec.Metadata["responseId"] != "" {
		return Online
	}
	return Evaluation
}
