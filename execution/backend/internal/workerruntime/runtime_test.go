package workerruntime

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

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
