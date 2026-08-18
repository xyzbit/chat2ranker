package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/xyzbit/chat2ranker/rank/backend/internal/domain"
)

func TestProcessRunnerIsolatesCandidateJudgeAndRetainsArtifacts(t *testing.T) {
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	backendRoot := filepath.Clean(filepath.Join(workingDirectory, "../.."))
	temporaryRoot := t.TempDir()
	workerBinary := filepath.Join(temporaryRoot, "rank-worker")
	build := exec.Command("go", "build", "-o", workerBinary, "./cmd/rank-worker")
	build.Dir = backendRoot
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build rank-worker: %v\n%s", err, output)
	}
	artifactRoot := filepath.Join(temporaryRoot, "artifacts")
	sandboxRoot := filepath.Join(temporaryRoot, "sandboxes")
	runner := ProcessRunner{
		runnerType: "mock",
		config:     ProcessRunnerConfig{WorkerBinary: workerBinary, RepositoryRoot: filepath.Clean(filepath.Join(backendRoot, "../..")), ArtifactRoot: artifactRoot, SandboxRoot: sandboxRoot, Timeout: 10 * time.Second, JudgeRunner: "mock"},
	}
	result, err := runner.RunCase(context.Background(), ExecutionSpec{
		RunID: "run-process-test",
		Case:  domain.Case{ID: "case-1", Title: "isolated", Input: "complete the task", Expected: map[string]any{"summary": "structured result", "demoOutcome": "pass"}},
		Agent: domain.AgentVersion{ID: "agent-v1", RunnerType: "mock", Model: "deterministic-demo"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed || result.ExecutionID == "" || result.JudgeExecutionID == "" || result.ExecutionID == result.JudgeExecutionID {
		t.Fatalf("candidate and judge were not isolated: %#v", result)
	}
	if len(result.Artifacts) < 4 {
		t.Fatalf("expected retained candidate and judge artifacts, got %#v", result.Artifacts)
	}
	if entries, err := os.ReadDir(filepath.Join(sandboxRoot, "run-process-test")); err == nil && len(entries) != 0 {
		t.Fatalf("sandbox workspaces were not removed: %#v", entries)
	}
	for _, artifact := range result.Artifacts {
		if _, err := os.Stat(filepath.Join(artifactRoot, filepath.FromSlash(artifact.Path))); err != nil {
			t.Fatalf("artifact %s is not readable: %v", artifact.Path, err)
		}
	}
}
