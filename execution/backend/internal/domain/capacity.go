package domain

import (
	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"time"
)

// CapacityState is one database snapshot of queued and resource-retaining work.
// Entries omit prompts, results, runtime locations and credentials. Limits are
// the same values used by repository admission; zero disables that limit.
type CapacityState struct {
	ModelQuotas         map[string]ModelQuotaCapacity
	Pools               []RuntimePool
	Entries             []CapacityEntry
	MaxModelConcurrent  int
	MaxTenantConcurrent int
}

type CapacityEntry struct {
	ModelQuota        *contract.ModelQuotaPlan
	MaxModelRequests  int
	RuntimePlan       *contract.RuntimePlan
	ID                string
	Queue             string
	Status            string
	Executor          string
	Harness           string
	Connection        string
	Model             string
	Tenant            string
	StructuredHistory bool
	QueueExpiresAt    time.Time
	CreatedAt         time.Time
	CleanupPending    bool
	DeliveryReady     bool
}

// ModelQuotaCapacity contains remaining minute allowances from the database
// snapshot. Negative values mean a disabled rate dimension.
type ModelQuotaCapacity struct{ Requests, Tokens int64 }
