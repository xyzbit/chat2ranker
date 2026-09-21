package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/execution/backend/internal/app"
	"github.com/xyzbit/agentkiln/execution/backend/internal/domain"
	"github.com/xyzbit/agentkiln/execution/backend/internal/httpapi"
	"github.com/xyzbit/agentkiln/execution/backend/internal/sqlite"
)

type capacityRepository struct {
	domain.Repository
	state domain.CapacityState
	err   error
}

func (r capacityRepository) CapacityState(context.Context) (domain.CapacityState, error) {
	return r.state, r.err
}

type capacityRuntime struct{ runtimeOutcome }

func (*capacityRuntime) Probe(_ context.Context, name string) contract.Availability {
	return contract.Availability{Installed: name != "missing", Available: name == "mock", StructuredHistory: name != "no-history"}
}

func TestCapacityReservesSharedQuotaBeforeAdvertisingDemand(t *testing.T) {
	now := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	entry := func(id, queue, status string) domain.CapacityEntry {
		return domain.CapacityEntry{QueueExpiresAt: now.Add(time.Minute), ID: id, Queue: queue, Status: status, Executor: "test", Harness: "mock", Connection: "private-connection", Model: "private-model", Tenant: "private-tenant", DeliveryReady: true, CreatedAt: now.Add(-10 * time.Second)}
	}
	active := entry("0", "online", contract.StatusCancelling)
	unknown := entry("1", "evaluation", contract.StatusFailed)
	unknown.CleanupPending = true
	first := entry("2", "online", contract.StatusQueued)
	second := entry("3", "evaluation", contract.StatusQueued)
	third := entry("4", "online", contract.StatusQueued)
	repo := capacityRepository{state: domain.CapacityState{MaxModelConcurrent: 3, MaxTenantConcurrent: 8, Entries: []domain.CapacityEntry{third, second, unknown, first, active}}}
	service := app.NewService(repo, &capacityRuntime{}, app.Options{Clock: func() time.Time { return now }, OnlineSlots: 1, EvaluationSlots: 1})
	value, err := service.Capacity(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if value.Online.Active != 1 || value.Online.Ready != 1 || value.Online.QuotaBlocked != 1 || value.Online.Demand != 2 || value.Online.OldestReadyWaitSeconds != 10 || value.Evaluation.Ready != 0 || value.Evaluation.QuotaBlocked != 1 || value.Evaluation.CleanupPending != 1 || value.Evaluation.Demand != 0 {
		t.Fatalf("shared budget: %+v", value)
	}
	encoded, _ := json.Marshal(value)
	if strings.Contains(string(encoded), "private-") {
		t.Fatalf("capacity exposes model or tenant identifiers: %s", encoded)
	}
	// Terminal cleanup from a different backend still retains the global quota.
	for i := range repo.state.Entries {
		if repo.state.Entries[i].ID == "1" {
			repo.state.Entries[i].Executor = "other"
		}
	}
	service = app.NewService(repo, &capacityRuntime{}, app.Options{Clock: func() time.Time { return now }, OnlineSlots: 1, EvaluationSlots: 1})
	value, err = service.Capacity(t.Context())
	if err != nil || value.Evaluation.CleanupPending != 0 || value.Online.Ready+value.Evaluation.Ready != 1 {
		t.Fatalf("cross-backend reservation: %+v, %v", value, err)
	}
}

func TestCapacityDistinguishesCapabilitiesDeliveryAndTenantLimits(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name, harness, backend, queue string
		history, delivery             bool
		connection                    string
		want                          string
	}{
		{"request credentials", "needs-model", "test", "online", false, true, "connection", "ready"},
		{"missing credentials", "needs-model", "test", "online", false, true, "", "capability"},
		{"missing executable", "missing", "test", "online", false, true, "connection", "capability"},
		{"unsupported history", "no-history", "test", "online", true, true, "connection", "capability"},
		{"other backend", "mock", "other", "online", false, true, "", "capability"},
		{"disabled queue", "mock", "test", "evaluation", false, true, "", "capability"},
		{"delivery backoff", "mock", "test", "online", false, false, "", "delivery"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			e := domain.CapacityEntry{QueueExpiresAt: now.Add(time.Minute), ID: "a", Status: contract.StatusQueued, Executor: test.backend, Harness: test.harness, Queue: test.queue, Connection: test.connection, StructuredHistory: test.history, DeliveryReady: test.delivery, CreatedAt: now.Add(time.Second)}
			repo := capacityRepository{state: domain.CapacityState{Entries: []domain.CapacityEntry{e}}}
			value, err := app.NewService(repo, &capacityRuntime{}, app.Options{OnlineSlots: 1, Clock: func() time.Time { return now }}).Capacity(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			q := value.Online
			if test.queue == "evaluation" {
				q = value.Evaluation
			}
			if q.Ready+q.CapabilityBlocked+q.DeliveryBlocked != 1 || (test.want == "ready" && q.Ready != 1) || (test.want == "capability" && q.CapabilityBlocked != 1) || (test.want == "delivery" && q.DeliveryBlocked != 1) || q.OldestReadyWaitSeconds != 0 {
				t.Fatalf("unexpected capacity: %+v", value)
			}
		})
	}
	// No model connection bypasses the model limit, but not the default tenant.
	e := domain.CapacityEntry{QueueExpiresAt: now.Add(time.Minute), ID: "a", Status: contract.StatusQueued, Executor: "test", Harness: "mock", Queue: "online", DeliveryReady: true, CreatedAt: now}
	other := e
	other.ID = "b"
	other.Queue = "evaluation"
	repo := capacityRepository{state: domain.CapacityState{MaxModelConcurrent: 1, MaxTenantConcurrent: 1, Entries: []domain.CapacityEntry{other, e}}}
	value, err := app.NewService(repo, &capacityRuntime{}, app.Options{OnlineSlots: 1, EvaluationSlots: 1}).Capacity(t.Context())
	if err != nil || value.Online.Ready != 1 || value.Evaluation.QuotaBlocked != 1 {
		t.Fatalf("default tenant limit: %+v %v", value, err)
	}
}

func TestCapacityHTTPUsesPersistentSQLiteStateAndRequiresServiceToken(t *testing.T) {
	repo, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	service := app.NewService(repo, &capacityRuntime{}, app.Options{OnlineSlots: 1})
	execution, err := service.Submit(t.Context(), contract.SubmitRequest{IdempotencyKey: "queued", Spec: contract.Spec{ProtocolVersion: contract.ProtocolVersion, Kind: contract.KindAgent, Harness: "mock", Prompt: "private prompt"}})
	if err != nil {
		t.Fatal(err)
	}
	handler := httpapi.New(service, httpapi.Options{ServiceToken: "test-service-token", CapacityToken: "capacity-only"})
	read := func(token string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, "/v1/capacity", nil)
		request.Header.Set("Authorization", "Bearer "+token)
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, request)
		return result
	}
	if got := read(""); got.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status %d", got.Code)
	}
	if got := read("capacity-only"); got.Code != http.StatusOK {
		t.Fatalf("capacity token status %d", got.Code)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/executions/"+execution.ID, nil)
	request.Header.Set("Authorization", "Bearer capacity-only")
	denied := httptest.NewRecorder()
	handler.ServeHTTP(denied, request)
	if denied.Code != http.StatusUnauthorized {
		t.Fatalf("capacity token read private execution: %d", denied.Code)
	}
	response := read("test-service-token")
	var value contract.Capacity
	if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if response.Code != 200 || response.Header().Get("Cache-Control") != "no-store" || value.Evaluation.CapabilityBlocked != 1 || value.Evaluation.Demand != 0 || strings.Contains(response.Body.String(), "private prompt") {
		t.Fatalf("response %s", response.Body.String())
	}
	if _, err = service.Cancel(t.Context(), execution.ID); err != nil {
		t.Fatal(err)
	}
	value, err = service.Capacity(t.Context())
	if err != nil || value.Evaluation.CapabilityBlocked != 0 {
		t.Fatalf("cancelled work: %+v %v", value, err)
	}
}

func TestCapacityReturnsStorageFailureInsteadOfZeroDemand(t *testing.T) {
	failure := errors.New("database unavailable")
	_, err := app.NewService(capacityRepository{err: failure}, &capacityRuntime{}, app.Options{}).Capacity(t.Context())
	if !errors.Is(err, failure) {
		t.Fatalf("storage failure hidden: %v", err)
	}
}

type poolRuntime struct {
	capacityRuntime
	plan contract.RuntimePlan
}

func (*poolRuntime) Name() string                 { return "opensandbox" }
func (r *poolRuntime) Plan() contract.RuntimePlan { return r.plan }
func (r *poolRuntime) SupportsPlan(p *contract.RuntimePlan) bool {
	return p != nil && p.Pool == r.plan.Pool && p.Endpoint == r.plan.Endpoint && p.Scope == r.plan.Scope && p.Image == r.plan.Image
}

func TestSandboxDemandExcludesHostResourcesAndRetainsCleanup(t *testing.T) {
	now := time.Now().UTC()
	pool := domain.RuntimePool{ID: "private-pool", Endpoint: "http://private-host/v1", Scope: "private-scope", Capacity: contract.RuntimeResources{CPUMillis: 4000, MemoryBytes: 1024}}
	plan := contract.RuntimePlan{Pool: pool.ID, Endpoint: pool.Endpoint, Scope: pool.Scope, Image: "sha256:image", Resources: contract.RuntimeResources{CPUMillis: 500, MemoryBytes: 512}}
	makeEntry := func(id, queue, status string) domain.CapacityEntry {
		return domain.CapacityEntry{ID: id, Queue: queue, Status: status, Executor: "opensandbox", Harness: "mock", RuntimePlan: &plan, DeliveryReady: true, CreatedAt: now.Add(-time.Second), QueueExpiresAt: now.Add(time.Minute)}
	}
	unknown := makeEntry("0", "evaluation", contract.StatusFailed)
	unknown.CleanupPending = true
	first := makeEntry("1", "online", contract.StatusQueued)
	second := makeEntry("2", "evaluation", contract.StatusQueued)
	repo := capacityRepository{state: domain.CapacityState{Pools: []domain.RuntimePool{pool}, Entries: []domain.CapacityEntry{second, unknown, first}}}
	runtime := &poolRuntime{plan: plan}
	value, err := app.NewService(repo, runtime, app.Options{OnlineSlots: 1, EvaluationSlots: 1}).Capacity(t.Context())
	if err != nil || value.Online.Ready != 1 || value.Online.Demand != 1 || value.Evaluation.ResourceBlocked != 1 || value.Evaluation.CleanupPending != 1 || value.Evaluation.Demand != 0 {
		t.Fatalf("memory budget inflated demand: %+v %v", value, err)
	}
	payload, _ := json.Marshal(value)
	if strings.Contains(string(payload), "private-") {
		t.Fatal("capacity exposed pool location")
	}
	repo.state.Pools[0].Capacity = contract.RuntimeResources{CPUMillis: 500, MemoryBytes: 4096}
	value, err = app.NewService(repo, runtime, app.Options{OnlineSlots: 1, EvaluationSlots: 1}).Capacity(t.Context())
	if err != nil || value.Online.ResourceBlocked != 1 || value.Evaluation.ResourceBlocked != 1 || value.Online.Ready != 0 {
		t.Fatalf("CPU budget inflated demand: %+v %v", value, err)
	}
	foreign := first
	foreign.ID = "old-image"
	foreign.Status = contract.StatusRunning
	foreignPlan := plan
	foreignPlan.Image = "sha256:old-image"
	foreign.RuntimePlan = &foreignPlan
	repo.state.Entries = []domain.CapacityEntry{foreign, first}
	value, err = app.NewService(repo, runtime, app.Options{OnlineSlots: 1, EvaluationSlots: 1}).Capacity(t.Context())
	if err != nil || value.Online.Active != 0 || value.Online.Demand != 0 || value.Online.ResourceBlocked != 1 {
		t.Fatalf("another worker image inflated demand or released shared resources: %+v %v", value, err)
	}
}
