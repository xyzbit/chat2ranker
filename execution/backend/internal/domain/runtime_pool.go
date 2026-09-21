package domain

import (
	"errors"
	"net/url"
	"strings"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
)

// RuntimePool is the operator's exclusive CPU/memory budget on one sandbox service.
// All clients allocating from that budget must share this Execution database.
type RuntimePool struct {
	ID       string                    `json:"id"`
	Endpoint string                    `json:"endpoint"`
	Scope    string                    `json:"scope"`
	Capacity contract.RuntimeResources `json:"capacity"`
}

// Validate rejects incomplete service identities and nonpositive assigned budgets.
func (p RuntimePool) Validate() error {
	endpoint, err := url.Parse(p.Endpoint)
	if err != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || !strings.HasSuffix(p.Endpoint, "/v1") || strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Scope) == "" || p.Capacity.CPUMillis <= 0 || p.Capacity.MemoryBytes <= 0 {
		return errors.New("runtime pool requires an ID, /v1 service URL, scope and positive CPU/memory capacity")
	}
	return nil
}

// RuntimePlanner supplies immutable allocation input; only remote executors need it.
type RuntimePlanner interface {
	// Plan returns resolved allocation input for a new submission.
	Plan() contract.RuntimePlan
	// SupportsPlan matches the frozen target/image without replacing queued resource limits.
	SupportsPlan(*contract.RuntimePlan) bool
}

// FitsRuntime uses subtraction to avoid overflow while accounting retained work.
func FitsRuntime(capacity, used, requested contract.RuntimeResources) bool {
	return requested.CPUMillis > 0 && requested.MemoryBytes > 0 && used.CPUMillis >= 0 && used.MemoryBytes >= 0 && used.CPUMillis <= capacity.CPUMillis && used.MemoryBytes <= capacity.MemoryBytes && requested.CPUMillis <= capacity.CPUMillis-used.CPUMillis && requested.MemoryBytes <= capacity.MemoryBytes-used.MemoryBytes
}
