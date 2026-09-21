package executor_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/xyzbit/agentkiln/execution/backend/artifacts"
	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/execution/backend/internal/app"
	"github.com/xyzbit/agentkiln/execution/backend/internal/domain"
	"github.com/xyzbit/agentkiln/execution/backend/internal/executor"
	"github.com/xyzbit/agentkiln/execution/backend/internal/sqlite"
)

func TestOpenSandboxRunsWorkerAndRetainsArtifactsAfterCleanup(t *testing.T) {
	runOpenSandboxIntegration(t, "mock", false, false)
}

func TestOpenSandboxRunsDSHWithStructuredHistory(t *testing.T) {
	if os.Getenv("EXECUTION_TEST_SANDBOX_MODEL_URL") == "" {
		t.Skip("EXECUTION_TEST_SANDBOX_MODEL_URL is required")
	}
	runOpenSandboxIntegration(t, "dsh", false, false)
}

func TestOpenSandboxReportsDSHModelBudgetAfterCleanup(t *testing.T) {
	if os.Getenv("EXECUTION_TEST_SANDBOX_MODEL_URL") == "" {
		t.Skip("EXECUTION_TEST_SANDBOX_MODEL_URL is required")
	}
	runOpenSandboxIntegration(t, "dsh", false, true)
}

type rejectSandboxArtifacts struct{ artifacts.Store }

func (rejectSandboxArtifacts) Put(context.Context, string, io.Reader, int64) error {
	return errors.New("artifact destination unavailable")
}

func TestOpenSandboxCleansUpAfterArtifactFailureWithoutReplay(t *testing.T) {
	runOpenSandboxIntegration(t, "mock", true, false)
}

func runOpenSandboxIntegration(t *testing.T, harness string, artifactFailure, modelBudget bool) {
	t.Helper()
	endpoint := os.Getenv("EXECUTION_TEST_SANDBOX_URL")
	if endpoint == "" {
		t.Skip("EXECUTION_TEST_SANDBOX_URL is required")
	}
	directory := t.TempDir()
	if err := os.MkdirAll(filepath.Join(directory, "artifacts"), 0700); err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(t.Context(), filepath.Join(directory, "execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.ConfigureRuntimePool(t.Context(), domain.RuntimePool{ID: "integration", Endpoint: endpoint, Scope: "rank-integration", Capacity: contract.RuntimeResources{CPUMillis: 1000, MemoryBytes: 1 << 30}}, false); err != nil {
		t.Fatal(err)
	}
	config := executor.OpenSandboxConfig{Pool: "integration", Endpoint: endpoint, APIKey: os.Getenv("EXECUTION_TEST_SANDBOX_API_KEY"), Scope: "rank-integration", Image: os.Getenv("EXECUTION_TEST_SANDBOX_IMAGE"), CPU: "500m", Memory: "512Mi", RequestTimeout: 5 * time.Second, PollInterval: 50 * time.Millisecond, CleanupGrace: 3 * time.Second, MaxArtifactBytes: 8 << 20, MaxArtifacts: 32, Repository: store, ArtifactStore: artifacts.Local{Root: filepath.Join(directory, "artifacts")}}
	config.Egress, err = contract.ParseEgressPolicy(os.Getenv("EXECUTION_SANDBOX_EGRESS_POLICY"))
	if err != nil {
		t.Fatal(err)
	}
	if artifactFailure {
		config.ArtifactStore = rejectSandboxArtifacts{config.ArtifactStore}
	}
	spec := contract.Spec{ProtocolVersion: contract.ProtocolVersion, Kind: contract.KindAgent, Harness: harness, Prompt: "sandbox acceptance"}
	want := "sandbox acceptance"
	if harness == "dsh" {
		config.ResolveModelConnection = func(context.Context, string) (contract.ModelConnection, string, error) {
			return contract.ModelConnection{ID: "fixture", Provider: "custom", Protocol: contract.ProtocolOpenAIChat, BaseURL: os.Getenv("EXECUTION_TEST_SANDBOX_MODEL_URL"), Status: "verified"}, "rank-private-fixture-credential", nil
		}
		spec.ModelConnectionID, spec.Model = "fixture", "rank-lab"
		spec.Prompt = ""
		spec.Messages = []contract.Message{{Role: "user", Content: "My name is Ada."}, {Role: "assistant", Content: "Hello Ada."}, {Role: "user", Content: "What is my name?"}}
		want = "Ada"
	}
	if modelBudget {
		spec.Model = "rank-budget-429-integration"
		spec.MaxModelRequests = 1
	}
	remote, err := executor.NewOpenSandbox(config)
	if err != nil {
		t.Fatal(err)
	}
	service := app.NewService(store, remote, app.Options{ExecutionTimeout: 45 * time.Second, CleanupGrace: 3 * time.Second, ArtifactRoot: filepath.Join(directory, "artifacts")})
	value, err := service.Submit(t.Context(), contract.SubmitRequest{IdempotencyKey: "remote", Spec: spec})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 55*time.Second)
	defer cancel()
	if err := service.Execute(ctx, value.ID); err != nil {
		t.Fatal(err)
	}
	current, err := store.Get(t.Context(), value.ID)
	if modelBudget {
		if current.Result == nil {
			t.Fatalf("missing budget result: %s", current.Error)
		}
		if err != nil || current.Status != contract.StatusFailed || current.RuntimeCleanupPending || !strings.Contains(current.Error, "execution_model_budget_exhausted") || len(current.Result.Artifacts) == 0 {
			t.Fatalf("budget failure did not retain known outcome and evidence: status=%s error=%s cleanup=%v artifacts=%d read=%v", current.Status, current.Error, current.RuntimeCleanupPending, len(current.Result.Artifacts), err)
		}
		if err := service.Execute(t.Context(), value.ID); err != nil {
			t.Fatal(err)
		}
		repeated, err := store.Get(t.Context(), value.ID)
		if err != nil || repeated.Attempt != 1 || repeated.MaxModelRequests != 1 {
			t.Fatal("budget failure replayed or lost frozen budget")
		}
		return
	}
	if artifactFailure {
		if err != nil || current.Status != contract.StatusFailed || current.RuntimeCleanupPending || current.Runtime == nil || current.Runtime.Handle == "" || !strings.Contains(current.Error, "artifact storage failed") {
			t.Fatalf("failed artifact publication did not clean runtime: %#v %v", current, err)
		}
		if err := service.Execute(t.Context(), value.ID); err != nil {
			t.Fatal(err)
		}
		repeated, err := store.Get(t.Context(), value.ID)
		if err != nil || repeated.Attempt != 1 || repeated.Runtime.Handle != current.Runtime.Handle {
			t.Fatal("artifact failure replayed the invocation")
		}
		return
	}
	if err != nil || current.Status != contract.StatusCompleted || current.RuntimeCleanupPending || current.Runtime == nil || current.Runtime.Handle == "" {
		t.Fatalf("remote outcome: %#v %v", current, err)
	}
	if current.Attempt != 1 || !strings.Contains(current.Result.Output, want) {
		t.Fatalf("unexpected remote result: %#v", current.Result)
	}
	if len(current.Result.Artifacts) < 2 {
		t.Fatalf("missing artifacts: %#v", current.Result.Artifacts)
	}
	for _, ref := range current.Result.Artifacts {
		if len(ref.SHA256) != 64 {
			t.Fatal("artifact digest missing")
		}
		content, err := service.ReadArtifact(t.Context(), value.ID, ref.Path)
		if err != nil || content.Content == "" || content.Truncated {
			t.Fatalf("artifact missing after runtime deletion: %v", err)
		}
		data := []byte(content.Content)
		if content.Encoding == "base64" {
			data, err = base64.StdEncoding.DecodeString(content.Content)
			if err != nil {
				t.Fatal(err)
			}
		}
		digest := sha256.Sum256(data)
		if hex.EncodeToString(digest[:]) != ref.SHA256 {
			t.Fatal("artifact bytes changed during HTTP encoding")
		}
		if strings.HasSuffix(ref.Path, ".zstd") {
			decoder, err := zstd.NewReader(nil)
			if err != nil {
				t.Fatal(err)
			}
			data, err = decoder.DecodeAll(data, nil)
			decoder.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
		if bytes.Contains(data, []byte("rank-private-fixture-credential")) {
			t.Fatal("model credential was retained in artifacts")
		}
	}
	events, err := service.Events(t.Context(), value.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	wantEvent := "harness.output"
	if harness == "dsh" {
		wantEvent = "harness.stdout"
	}
	for _, event := range events {
		found = found || event.Type == wantEvent
	}
	if !found {
		t.Fatalf("remote progress not persisted: %#v", events)
	}
}
