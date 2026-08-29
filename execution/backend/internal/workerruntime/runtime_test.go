package workerruntime

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/xyzbit/chat2ranker/execution/backend/contract"
	"github.com/xyzbit/chat2ranker/execution/backend/internal/workerprotocol"
)

func TestCollectArtifactsExcludesHarnessRuntimeButKeepsHistoryAndOutputs(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{
		"request.json":                      "{}",
		"result.json":                       "{}",
		"trace.jsonl":                       "{}",
		"stdout.txt":                        "answer",
		"stderr.txt":                        "",
		"outputs/report.md":                 "report",
		"harness-home/rank-agent.patch.yml": "[]",
		"harness-home/profiles/node_modules/dsh/package.json":            "{}",
		"harness-home/sessions/workspace/session-one/session.jsonl.zstd": "history",
	} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	artifacts := collectArtifacts(workerprotocol.Request{ExecutionID: "exec-one", ArtifactDir: root})
	paths := make([]string, 0, len(artifacts))
	kinds := map[string]string{}
	for _, artifact := range artifacts {
		paths = append(paths, artifact.Path)
		kinds[artifact.Path] = artifact.Kind
	}
	want := []string{
		"exec-one/harness-home/sessions/workspace/session-one/session.jsonl.zstd",
		"exec-one/outputs/report.md",
		"exec-one/request.json",
		"exec-one/result.json",
		"exec-one/stderr.txt",
		"exec-one/stdout.txt",
		"exec-one/trace.jsonl",
	}
	if !slices.Equal(paths, want) {
		t.Fatalf("unexpected artifacts:\n got %#v\nwant %#v", paths, want)
	}
	if kinds[want[0]] != "session-history" || kinds[want[1]] != "artifact" || kinds[want[2]] != "request" {
		t.Fatalf("unexpected artifact kinds: %#v", kinds)
	}
}

func TestExecuteRedactsTransientCredentialFromRequestArtifact(t *testing.T) {
	root := t.TempDir()
	request := workerprotocol.Request{ProtocolVersion: workerprotocol.Version, ExecutionID: "exec-secret", Spec: contract.Spec{ProtocolVersion: contract.ProtocolVersion, Kind: contract.KindAgent, Harness: "mock", Prompt: "hello"}, WorkspaceDir: filepath.Join(root, "workspace"), ArtifactDir: filepath.Join(root, "artifacts"), HarnessHome: filepath.Join(root, "artifacts", "home"), ModelConnection: &contract.ModelConnection{ID: "model-one", CredentialRef: "private-ref"}, Credential: "do-not-persist"}
	response := Execute(context.Background(), request, nil)
	if response.Status != contract.StatusCompleted {
		t.Fatalf("execution failed: %#v", response)
	}
	payload, err := os.ReadFile(filepath.Join(request.ArtifactDir, "request.json"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	if strings.Contains(text, "do-not-persist") || strings.Contains(text, "private-ref") || strings.Contains(text, "credential\"") {
		t.Fatalf("credential leaked: %s", text)
	}
}
