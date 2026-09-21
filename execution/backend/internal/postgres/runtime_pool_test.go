package postgres

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/execution/backend/internal/domain"
	"github.com/xyzbit/agentkiln/execution/backend/internal/sqlite"
)

func TestRuntimePoolAdmissionRetainsUnknownAllocationsAcrossWorkers(t *testing.T) {
	for _, backend := range []string{"sqlite-cpu", "sqlite-memory", "postgres-cpu", "postgres-memory"} {
		t.Run(backend, func(t *testing.T) {
			var repo domain.Repository
			if strings.HasPrefix(backend, "postgres") {
				repo = testStore(t, 30)
			} else {
				store, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "runtime.db"))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { store.Close() })
				repo = store
			}
			pool := domain.RuntimePool{ID: "host", Endpoint: "http://sandbox.test/v1", Scope: "shared", Capacity: contract.RuntimeResources{CPUMillis: 1000, MemoryBytes: 1024}}
			if strings.HasSuffix(backend, "cpu") {
				pool.Capacity.MemoryBytes = 4096
			} else {
				pool.Capacity.CPUMillis = 4000
			}
			if err := repo.ConfigureRuntimePool(t.Context(), pool, false); err != nil {
				t.Fatal(err)
			}
			duplicate := pool
			duplicate.ID = "duplicate-budget"
			if err := repo.ConfigureRuntimePool(t.Context(), duplicate, false); err == nil {
				t.Fatal("one service received duplicate budgets")
			}
			changed := pool
			changed.Capacity.CPUMillis += 1000
			if err := repo.ConfigureRuntimePool(t.Context(), changed, false); err == nil {
				t.Fatal("silent pool change accepted")
			}
			plan := contract.RuntimePlan{Pool: pool.ID, Endpoint: pool.Endpoint, Scope: pool.Scope, Image: "sha256:test", Resources: contract.RuntimeResources{CPUMillis: 500, MemoryBytes: 512}}
			plan.Egress = contract.EgressPolicy{Allow: []contract.EgressRule{{Protocol: "tcp", CIDR: "192.0.2.1/32", Port: 443}}}
			unfit := executionValue("unfit")
			unfit.Executor = "opensandbox"
			oversized := plan
			oversized.Resources.CPUMillis = pool.Capacity.CPUMillis + 1
			unfit.RuntimePlan = &oversized
			if err := repo.Create(t.Context(), unfit, queuedEvent(unfit)); !errors.Is(err, domain.ErrRuntimePlan) {
				t.Fatalf("oversized runtime accepted: %v", err)
			}
			pending := make([]contract.Execution, 8)
			for i := range pending {
				v := executionValue(fmt.Sprintf("resource-%d", i))
				v.Executor = "opensandbox"
				v.RuntimePlan = &plan
				v.Spec.ModelConnectionID = fmt.Sprintf("model-%d", i)
				v.Spec.Model = "model"
				v.Spec.Metadata = map[string]string{"tenantId": fmt.Sprintf("tenant-%d", i)}
				if err := repo.Create(t.Context(), v, queuedEvent(v)); err != nil {
					t.Fatal(err)
				}
				pending[i] = v
			}
			moved := pool
			moved.Endpoint = "http://another.test/v1"
			if err := repo.ConfigureRuntimePool(t.Context(), moved, true); err == nil {
				t.Fatal("retargeted queued work")
			}
			start := func(id string) error {
				at := time.Now().UTC()
				return repo.MarkRunning(t.Context(), id, 1, "opensandbox", "worker", "test", at, contract.Event{ExecutionID: id, Attempt: 1, Type: "execution.running", Status: contract.StatusRunning, At: at})
			}
			var wg sync.WaitGroup
			winners := make(chan string, 8)
			for _, v := range pending {
				wg.Go(func() {
					err := start(v.ID)
					if err == nil {
						winners <- v.ID
					} else if !errors.Is(err, domain.ErrResourceCapacity) {
						t.Error(err)
					}
				})
			}
			wg.Wait()
			close(winners)
			if len(winners) != 2 {
				t.Fatalf("admitted %d runtimes, wanted two", len(winners))
			}
			lost := <-winners
			other := <-winners
			waiting := ""
			for _, v := range pending {
				if v.ID != lost && v.ID != other {
					waiting = v.ID
					break
				}
			}
			at := time.Now().UTC()
			failed := contract.Event{ExecutionID: lost, Attempt: 1, Type: "execution.failed", Status: contract.StatusFailed, At: at}
			if err := repo.Fail(t.Context(), lost, "execution_outcome_unknown", at, failed); err != nil {
				t.Fatal(err)
			}
			if err := start(waiting); !errors.Is(err, domain.ErrResourceCapacity) {
				t.Fatalf("unknown runtime released resources: %v", err)
			}
			if err := repo.ConfirmRuntimeStopped(t.Context(), lost, 1); err != nil {
				t.Fatal(err)
			}
			if err := start(waiting); err != nil {
				t.Fatal(err)
			}
			// A live budget update is read by the existing repository/worker, not cached.
			changed.Capacity.MemoryBytes = 2048
			if err := repo.ConfigureRuntimePool(t.Context(), changed, true); err != nil {
				t.Fatal(err)
			}
			next := ""
			for _, v := range pending {
				if v.ID != lost && v.ID != other && v.ID != waiting {
					next = v.ID
					break
				}
			}
			if err := start(next); err != nil {
				t.Fatalf("updated durable budget not used: %v", err)
			}
			changed.Capacity = plan.Resources
			if err := repo.ConfigureRuntimePool(t.Context(), changed, true); err != nil {
				t.Fatal(err)
			}
			for _, v := range pending {
				if v.ID != lost && v.ID != other && v.ID != waiting && v.ID != next {
					if err := start(v.ID); !errors.Is(err, domain.ErrResourceCapacity) {
						t.Fatalf("shrinking a live budget admitted more work: %v", err)
					}
					break
				}
			}
			state, err := repo.CapacityState(t.Context())
			if err != nil || len(state.Pools) != 1 || state.Pools[0] != changed {
				t.Fatalf("capacity pool differs: %+v %v", state.Pools, err)
			}
			for _, entry := range state.Entries {
				if entry.RuntimePlan == nil || !reflect.DeepEqual(*entry.RuntimePlan, plan) {
					t.Fatal("frozen resources lost")
				}
			}
		})
	}
}
