package app

import (
	"context"
	"sort"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/execution/backend/internal/domain"
	"github.com/xyzbit/agentkiln/execution/backend/internal/jobs"
)

// Capacity estimates next-poll demand using the API's worker configuration.
// API and consumers must use the same executor, capabilities, limits and slots.
// The result is an observation, never an admission grant or a worker heartbeat.
func (s *Service) Capacity(ctx context.Context) (contract.Capacity, error) {
	state, err := s.repo.CapacityState(ctx)
	if err != nil {
		return contract.Capacity{}, err
	}
	result := contract.Capacity{ObservedAt: s.now().UTC(), Executor: s.executor.Name(), Online: contract.QueueCapacity{SlotsPerWorker: s.onlineSlots}, Evaluation: contract.QueueCapacity{SlotsPerWorker: s.evaluationSlots}}
	queue := func(entry domain.CapacityEntry) *contract.QueueCapacity {
		if entry.Queue == jobs.Online {
			return &result.Online
		}
		return &result.Evaluation
	}
	type modelKey struct{ connection, model string }
	pools := map[string]domain.RuntimePool{}
	used := map[string]contract.RuntimeResources{}
	missingRuntimePlan := false
	for _, pool := range state.Pools {
		pools[pool.ID] = pool
	}
	models := map[modelKey]int{}
	tenants := map[string]int{}
	reserve := func(entry domain.CapacityEntry) {
		if entry.Executor == "opensandbox" {
			if entry.RuntimePlan == nil {
				missingRuntimePlan = true
			} else {
				plan := entry.RuntimePlan
				r := used[plan.Pool]
				r.CPUMillis += plan.Resources.CPUMillis
				r.MemoryBytes += plan.Resources.MemoryBytes
				used[plan.Pool] = r
			}
		}
		if entry.Connection != "" {
			models[modelKey{entry.Connection, entry.Model}]++
		}
		tenants[entry.Tenant]++
	}
	for _, entry := range state.Entries {
		active := entry.Status == contract.StatusRunning || entry.Status == contract.StatusCancelling
		if active || entry.CleanupPending {
			reserve(entry)
		}
		if entry.Executor != result.Executor {
			continue
		}
		if planner, ok := s.executor.(domain.RuntimePlanner); ok && !planner.SupportsPlan(entry.RuntimePlan) {
			continue
		}
		if active {
			queue(entry).Active++
		} else if entry.CleanupPending {
			queue(entry).CleanupPending++
		}
	}
	// Allocate virtual quota across both queues so ten waiting requests cannot
	// report ten runnable slots when their shared model budget admits only two.
	sort.Slice(state.Entries, func(i, j int) bool {
		left, right := state.Entries[i], state.Entries[j]
		if left.CreatedAt.Equal(right.CreatedAt) {
			return left.ID < right.ID
		}
		return left.CreatedAt.Before(right.CreatedAt)
	})
	probes := map[string]contract.Availability{}
	for _, entry := range state.Entries {
		if entry.Status != contract.StatusQueued {
			continue
		}
		q := queue(entry)
		if entry.QueueExpiresAt.IsZero() || !result.ObservedAt.Before(entry.QueueExpiresAt) {
			q.Expired++
			continue
		}
		if entry.Executor != result.Executor || q.SlotsPerWorker <= 0 {
			q.CapabilityBlocked++
			continue
		}
		probe, exists := probes[entry.Harness]
		if !exists {
			probe = s.executor.Probe(ctx, entry.Harness)
			probes[entry.Harness] = probe
		}
		// Request-scoped credentials satisfy a missing default model connection,
		// but cannot install a missing executable or add history support.
		if (entry.MaxModelRequests > 0 && !probe.ModelRequestBudget) || (probe.ModelRequestBudget && entry.MaxModelRequests <= 0) || !probe.Installed || (!probe.Available && entry.Connection == "") || (entry.StructuredHistory && !probe.StructuredHistory) {
			q.CapabilityBlocked++
			continue
		}
		if planner, ok := s.executor.(domain.RuntimePlanner); ok && !planner.SupportsPlan(entry.RuntimePlan) {
			q.CapabilityBlocked++
			continue
		}
		if entry.ModelQuota != nil && !probe.ModelQuota {
			q.CapabilityBlocked++
			continue
		}
		if !entry.DeliveryReady {
			q.DeliveryBlocked++
			continue
		}
		if entry.CleanupPending || (state.MaxModelConcurrent > 0 && entry.Connection != "" && models[modelKey{entry.Connection, entry.Model}] >= state.MaxModelConcurrent) || (state.MaxTenantConcurrent > 0 && tenants[entry.Tenant] >= state.MaxTenantConcurrent) {
			q.QuotaBlocked++
			continue
		}
		if entry.Executor == "opensandbox" {
			plan := entry.RuntimePlan
			if plan == nil {
				q.CapabilityBlocked++
				continue
			}
			pool, ok := pools[plan.Pool]
			if !ok || pool.Endpoint != plan.Endpoint || pool.Scope != plan.Scope {
				q.CapabilityBlocked++
				continue
			}
			if missingRuntimePlan || !domain.FitsRuntime(pool.Capacity, used[plan.Pool], plan.Resources) {
				q.ResourceBlocked++
				continue
			}
		}
		if entry.ModelQuota != nil {
			remaining, exists := state.ModelQuotas[entry.ModelQuota.Group]
			minimum := entry.ModelQuota.MaxOutputTokens + 1
			if !exists || remaining.Requests == 0 || (remaining.Tokens >= 0 && remaining.Tokens < minimum) {
				q.QuotaBlocked++
				continue
			}
			if remaining.Requests > 0 {
				remaining.Requests--
			}
			if remaining.Tokens > 0 {
				remaining.Tokens -= minimum
			}
			state.ModelQuotas[entry.ModelQuota.Group] = remaining
		}
		reserve(entry)
		q.Ready++
		q.OldestReadyWaitSeconds = max(q.OldestReadyWaitSeconds, result.ObservedAt.Sub(entry.CreatedAt).Seconds())
	}
	for _, q := range []*contract.QueueCapacity{&result.Online, &result.Evaluation} {
		if q.SlotsPerWorker > 0 {
			q.Demand = q.Active + q.Ready
		}
	}
	return result, nil
}
