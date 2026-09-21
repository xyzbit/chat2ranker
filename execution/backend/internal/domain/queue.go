package domain

import (
	"time"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
)

// QueueExpiryEvent distinguishes elapsed queue time from missing admission data.
// Neither failure authorizes a Harness invocation or reports a quality result.
func QueueExpiryEvent(id string, attempt int, deadline, at time.Time) contract.Event {
	message := "execution_queue_expired: queue wait limit exceeded before runtime start"
	if deadline.IsZero() {
		message = "execution_limits_missing: queue deadline is absent; submit a new execution"
	}
	return contract.Event{ExecutionID: id, Attempt: attempt, Type: "execution.failed", Status: contract.StatusFailed, Message: message, At: at}
}
