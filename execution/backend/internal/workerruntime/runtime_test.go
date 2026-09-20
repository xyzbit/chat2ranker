package workerruntime

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/execution/backend/internal/workerprotocol"
)

func workerAttachment(name, mediaType, value string) contract.InputAttachment {
	data := []byte(value)
	return contract.InputAttachment{ID: fmt.Sprintf("sha256:%x", sha256.Sum256(data)), Type: "input_file", Name: name, MediaType: mediaType, Data: data}
}

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

func TestExecuteValidatesStructuredOutput(t *testing.T) {
	root := t.TempDir()
	request := workerprotocol.Request{
		ProtocolVersion: workerprotocol.Version, ExecutionID: "exec-structured",
		Spec: contract.Spec{
			ProtocolVersion: contract.ProtocolVersion, Kind: contract.KindAgent, Harness: "mock", Prompt: "status",
			StructuredOutput: &contract.StructuredOutput{Name: "status", Schema: json.RawMessage(`{"type":"object","properties":{"status":{"const":"ok"}},"required":["status"],"additionalProperties":false}`)},
		},
		WorkspaceDir: filepath.Join(root, "workspace"), ArtifactDir: filepath.Join(root, "artifacts"), HarnessHome: filepath.Join(root, "artifacts", "home"),
	}
	response := Execute(context.Background(), request, nil)
	if response.Status != contract.StatusCompleted || string(response.Result.Structured) != `{"status":"ok"}` {
		t.Fatalf("valid structured output failed: %#v", response)
	}
	request.ExecutionID = "exec-structured-invalid"
	request.ArtifactDir = filepath.Join(root, "invalid-artifacts")
	request.HarnessHome = filepath.Join(request.ArtifactDir, "home")
	request.Spec.StructuredOutput.Schema = json.RawMessage(`{"type":"object","properties":{"status":{"const":"not-ok"}},"required":["status"]}`)
	response = Execute(context.Background(), request, nil)
	if response.Status != contract.StatusFailed || !strings.Contains(response.Error, "structured output") {
		t.Fatalf("invalid structured output was accepted: %#v", response)
	}
}

func TestExecuteMaterializesFrozenInputAttachments(t *testing.T) {
	root := t.TempDir()
	attachment := workerAttachment("brief.txt", "text/plain", "frozen input")
	request := workerprotocol.Request{
		ProtocolVersion: workerprotocol.Version, ExecutionID: "exec-attachment",
		Spec:         contract.Spec{ProtocolVersion: contract.ProtocolVersion, Kind: contract.KindAgent, Harness: "mock", Prompt: "read it", InputAttachments: []contract.InputAttachment{attachment}},
		WorkspaceDir: filepath.Join(root, "workspace"), ArtifactDir: filepath.Join(root, "artifacts"), HarnessHome: filepath.Join(root, "artifacts", "home"),
	}
	response := Execute(context.Background(), request, nil)
	if response.Status != contract.StatusCompleted {
		t.Fatalf("attachment execution failed: %#v", response)
	}
	relative := filepath.FromSlash(contract.InputAttachmentRelativePath(0, attachment))
	for _, root := range []string{request.WorkspaceDir, request.ArtifactDir} {
		content, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil || string(content) != "frozen input" {
			t.Fatalf("materialized attachment at %s: %q, %v", root, content, err)
		}
	}
	var safe workerprotocol.Request
	payload, err := os.ReadFile(filepath.Join(request.ArtifactDir, "request.json"))
	if err != nil || json.Unmarshal(payload, &safe) != nil || len(safe.Spec.InputAttachments[0].Data) != 0 {
		t.Fatalf("request artifact retained attachment bytes: %s, %v", payload, err)
	}
	if !slices.ContainsFunc(response.Result.Artifacts, func(item contract.ArtifactRef) bool {
		return strings.HasSuffix(item.Path, "/"+filepath.ToSlash(relative))
	}) {
		t.Fatalf("input attachment missing from artifacts: %#v", response.Result.Artifacts)
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
