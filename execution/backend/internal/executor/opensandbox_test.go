package executor_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xyzbit/agentkiln/execution/backend/artifacts"
	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/execution/backend/internal/app"
	"github.com/xyzbit/agentkiln/execution/backend/internal/domain"
	"github.com/xyzbit/agentkiln/execution/backend/internal/executor"
	"github.com/xyzbit/agentkiln/execution/backend/internal/sqlite"
)

type rejectRuntimeAttachment struct{ domain.Repository }

func (rejectRuntimeAttachment) AttachRuntime(context.Context, string, int, string) error {
	return errors.New("database unavailable")
}

func TestSandboxPersistsOwnershipBeforeCommandsAndRejectsForeignProxy(t *testing.T) {
	for _, rejectAttachment := range []bool{false, true} {
		t.Run(map[bool]string{false: "foreign proxy", true: "attachment failure"}[rejectAttachment], func(t *testing.T) {
			var foreignRequests, endpointRequests atomic.Int32
			foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { foreignRequests.Add(1); w.WriteHeader(200) }))
			defer foreign.Close()
			var store *sqlite.Store
			var metadata map[string]string
			removed := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == "POST" && r.URL.Path == "/v1/sandboxes":
					var request struct {
						Metadata       map[string]string `json:"metadata"`
						ResourceLimits map[string]string `json:"resourceLimits"`
						Env            map[string]string `json:"env"`
					}
					json.NewDecoder(r.Body).Decode(&request)
					metadata = request.Metadata
					policy, err := contract.ParseEgressPolicy(request.Env["RANK_RUNTIME_EGRESS_POLICY"])
					if err != nil || request.Env["RANK_RUNTIME_EGRESS_POLICY"] == "" || len(policy.Allow) != 0 {
						t.Errorf("runtime did not receive explicit deny-all policy: %+v %v", policy, err)
					}
					if request.ResourceLimits["cpu"] != "500m" || request.ResourceLimits["memory"] != "536870912" {
						t.Errorf("queued resource limits changed: %v", request.ResourceLimits)
					}
					json.NewEncoder(w).Encode(map[string]any{"id": "owned", "metadata": metadata})
				case strings.HasSuffix(r.URL.Path, "/endpoints/44772"):
					endpointRequests.Add(1)
					current, err := store.Get(r.Context(), metadata["rank.execution"])
					if err != nil || current.Runtime == nil || current.Runtime.Handle != "owned" {
						t.Error("runtime handle was not persisted before endpoint resolution")
					}
					json.NewEncoder(w).Encode(map[string]string{"endpoint": foreign.URL})
				case r.Method == "DELETE" && r.URL.Path == "/v1/sandboxes/owned":
					removed = true
					w.WriteHeader(204)
				case r.Method == "GET" && r.URL.Path == "/v1/sandboxes/owned":
					if removed {
						w.WriteHeader(404)
					} else {
						json.NewEncoder(w).Encode(map[string]any{"id": "owned", "metadata": metadata})
					}
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(400)
				}
			}))
			defer server.Close()
			var config executor.OpenSandboxConfig
			config, store = sandboxTestConfig(t, server.URL)
			if rejectAttachment {
				config.Repository = rejectRuntimeAttachment{store}
			}
			remote, err := executor.NewOpenSandbox(config)
			if err != nil {
				t.Fatal(err)
			}
			service := app.NewService(store, remote, app.Options{ExecutionTimeout: time.Second, CleanupGrace: time.Second})
			value, err := service.Submit(t.Context(), contract.SubmitRequest{IdempotencyKey: "owned", Spec: contract.Spec{ProtocolVersion: contract.ProtocolVersion, Kind: contract.KindAgent, Harness: "mock", Prompt: "hello"}})
			if err != nil {
				t.Fatal(err)
			}
			config.CPU = "250m"
			config.Memory = "256Mi"
			remote, err = executor.NewOpenSandbox(config)
			if err != nil {
				t.Fatal(err)
			}
			service = app.NewService(store, remote, app.Options{ExecutionTimeout: time.Second, CleanupGrace: time.Second})
			if err := service.Execute(t.Context(), value.ID); err != nil {
				t.Fatal(err)
			}
			current, err := store.Get(t.Context(), value.ID)
			if err != nil || current.Status != contract.StatusFailed || current.RuntimeCleanupPending {
				t.Fatalf("failed invocation was not cleaned: %#v %v", current, err)
			}
			if foreignRequests.Load() != 0 {
				t.Fatal("credential-bearing request reached a foreign origin")
			}
			if rejectAttachment && endpointRequests.Load() != 0 {
				t.Fatal("runtime was accessed without persisted ownership")
			}
		})
	}
}

func sandboxTestConfig(t *testing.T, endpoint string) (executor.OpenSandboxConfig, *sqlite.Store) {
	t.Helper()
	root := t.TempDir()
	store, err := sqlite.Open(t.Context(), filepath.Join(root, "execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if err := store.ConfigureRuntimePool(t.Context(), domain.RuntimePool{ID: "test", Endpoint: endpoint + "/v1", Scope: "test", Capacity: contract.RuntimeResources{CPUMillis: 1000, MemoryBytes: 1 << 30}}, false); err != nil {
		t.Fatal(err)
	}
	return executor.OpenSandboxConfig{Pool: "test", Endpoint: endpoint + "/v1", APIKey: "private-lifecycle-key", Scope: "test", Image: "sha256:" + strings.Repeat("a", 64), CPU: "500m", Memory: "512Mi", RequestTimeout: time.Second, PollInterval: time.Millisecond, CleanupGrace: time.Second, MaxArtifactBytes: 1 << 20, MaxArtifacts: 4, Repository: store, ArtifactStore: artifacts.Local{Root: filepath.Join(root, "artifacts")}}, store
}

func TestSandboxLostCreateResponseIsNotReplayedAndCleanupCanRecover(t *testing.T) {
	var lock sync.Mutex
	creates, commands := 0, 0
	visible, removed := false, false
	var metadata map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lock.Lock()
		defer lock.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/sandboxes":
			creates++
			var request struct {
				Metadata map[string]string `json:"metadata"`
				Timeout  int               `json:"timeout"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
			}
			metadata = request.Metadata
			if request.Timeout != 60 {
				t.Errorf("short invocation violates provider minimum TTL: %d", request.Timeout)
			}
			http.Error(w, "creation response lost", http.StatusServiceUnavailable)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/sandboxes":
			items := []any{}
			if visible && !removed {
				items = append(items, map[string]any{"id": "remote", "metadata": metadata})
			}
			json.NewEncoder(w).Encode(map[string]any{"items": items})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/sandboxes/remote":
			if removed {
				http.Error(w, "gone", http.StatusNotFound)
			} else {
				json.NewEncoder(w).Encode(map[string]any{"id": "remote", "metadata": metadata})
			}
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/sandboxes/remote":
			removed = true
			w.WriteHeader(http.StatusNoContent)
		default:
			commands++
			http.Error(w, "unexpected runtime operation", http.StatusBadRequest)
		}
	}))
	defer server.Close()
	config, store := sandboxTestConfig(t, server.URL)
	remote, err := executor.NewOpenSandbox(config)
	if err != nil {
		t.Fatal(err)
	}
	service := app.NewService(store, remote, app.Options{ExecutionTimeout: time.Second, CleanupGrace: time.Second})
	value, err := service.Submit(t.Context(), contract.SubmitRequest{IdempotencyKey: "once", Spec: contract.Spec{ProtocolVersion: contract.ProtocolVersion, Kind: contract.KindAgent, Harness: "mock", Prompt: "once"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Execute(t.Context(), value.ID); err != nil {
		t.Fatal(err)
	}
	current, err := store.Get(t.Context(), value.ID)
	if err != nil || current.Status != contract.StatusFailed || !current.RuntimeCleanupPending || current.Runtime == nil || current.Runtime.Handle != "" {
		t.Fatalf("uncertain create must retain reservation: %#v %v", current, err)
	}
	if err := service.Execute(t.Context(), value.ID); err != nil {
		t.Fatal(err)
	}
	if err := service.ReconcileCleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	current, _ = store.Get(t.Context(), value.ID)
	if !current.RuntimeCleanupPending {
		t.Fatal("an empty provider list proved a late creation absent")
	}
	lock.Lock()
	visible = true
	lock.Unlock()
	// A new service owner recovers the persisted intent without replaying commands.
	replacement := app.NewService(store, remote, app.Options{CleanupGrace: time.Second})
	if err := replacement.ReconcileCleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	current, err = store.Get(t.Context(), value.ID)
	if err != nil || current.RuntimeCleanupPending || current.Runtime.Handle != "remote" || current.Attempt != 1 || current.Status != contract.StatusFailed {
		t.Fatalf("late resource cleanup: %#v %v", current, err)
	}
	lock.Lock()
	defer lock.Unlock()
	if creates != 1 || commands != 0 || !removed {
		t.Fatalf("create=%d command=%d removed=%v", creates, commands, removed)
	}
}

func TestSandboxRejectsInvalidConfiguration(t *testing.T) {
	base, _ := sandboxTestConfig(t, "https://sandbox.example")
	for _, test := range []struct {
		name   string
		change func(*executor.OpenSandboxConfig)
	}{
		{"mutable image", func(c *executor.OpenSandboxConfig) { c.Image = "rank:latest" }},
		{"userinfo", func(c *executor.OpenSandboxConfig) { c.Endpoint = "https://secret@sandbox.example/v1" }},
		{"missing key", func(c *executor.OpenSandboxConfig) { c.APIKey = "" }},
		{"zero CPU", func(c *executor.OpenSandboxConfig) { c.CPU = "0m" }},
		{"nonfinite CPU", func(c *executor.OpenSandboxConfig) { c.CPU = "NaN" }},
		{"invalid memory", func(c *executor.OpenSandboxConfig) { c.Memory = "unlimited" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := base
			test.change(&c)
			if _, err := executor.NewOpenSandbox(c); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}

func TestQueuedSandboxPlanSurvivesDefaultChangesAndRejectsAnotherImage(t *testing.T) {
	config, store := sandboxTestConfig(t, "http://sandbox.invalid")
	remote, err := executor.NewOpenSandbox(config)
	if err != nil {
		t.Fatal(err)
	}
	service := app.NewService(store, remote, app.Options{})
	input := contract.SubmitRequest{IdempotencyKey: "frozen-plan", Spec: contract.Spec{ProtocolVersion: contract.ProtocolVersion, Kind: contract.KindAgent, Harness: "mock", Prompt: "hello"}}
	first, err := service.Submit(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	original := *first.RuntimePlan
	config.Image = "sha256:" + strings.Repeat("b", 64)
	config.CPU = "250m"
	changed, err := executor.NewOpenSandbox(config)
	if err != nil {
		t.Fatal(err)
	}
	restarted := app.NewService(store, changed, app.Options{})
	replay, err := restarted.Submit(t.Context(), input)
	if err != nil || !reflect.DeepEqual(*replay.RuntimePlan, original) {
		t.Fatalf("replay changed runtime plan: %+v %v", replay.RuntimePlan, err)
	}
	if err := restarted.Execute(t.Context(), first.ID); !errors.Is(err, domain.ErrExecutorUnavailable) {
		t.Fatalf("another image claimed queued work: %v", err)
	}
	current, err := store.Get(t.Context(), first.ID)
	if err != nil || current.Status != contract.StatusQueued || current.Attempt != 0 || current.Runtime != nil {
		t.Fatalf("incompatible worker allocated a runtime: %+v %v", current, err)
	}
}

func TestQueuedSandboxNetworkPolicyRejectsChangedWorkerAndCallerMutation(t *testing.T) {
	config, store := sandboxTestConfig(t, "http://sandbox.invalid")
	config.Egress = contract.EgressPolicy{Allow: []contract.EgressRule{{Protocol: "tcp", CIDR: "192.0.2.1/32", Port: 443}}}
	remote, err := executor.NewOpenSandbox(config)
	if err != nil {
		t.Fatal(err)
	}
	service := app.NewService(store, remote, app.Options{})
	input := contract.SubmitRequest{IdempotencyKey: "frozen-network", Spec: contract.Spec{ProtocolVersion: contract.ProtocolVersion, Kind: contract.KindAgent, Harness: "mock", Prompt: "hello"}}
	first, err := service.Submit(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	config.Egress.Allow[0].CIDR = "0.0.0.0/0"
	plan := remote.Plan()
	plan.Egress.Allow[0].CIDR = "10.0.0.0/8"
	if remote.Plan().Egress.Allow[0].CIDR != "192.0.2.1/32" {
		t.Fatal("caller mutated executor policy")
	}
	changed, err := executor.NewOpenSandbox(config)
	if err != nil {
		t.Fatal(err)
	}
	restarted := app.NewService(store, changed, app.Options{})
	replay, err := restarted.Submit(t.Context(), input)
	if err != nil || !reflect.DeepEqual(replay.RuntimePlan, first.RuntimePlan) {
		t.Fatal("replay replaced frozen network policy")
	}
	if err := restarted.Execute(t.Context(), first.ID); !errors.Is(err, domain.ErrExecutorUnavailable) {
		t.Fatalf("changed policy claimed work: %v", err)
	}
	current, err := store.Get(t.Context(), first.ID)
	if err != nil || current.Status != contract.StatusQueued || current.Attempt != 0 || current.Runtime != nil {
		t.Fatalf("changed policy allocated a runtime: %+v %v", current, err)
	}
}
