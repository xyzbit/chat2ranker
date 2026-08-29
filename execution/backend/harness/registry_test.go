package harness

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xyzbit/chat2ranker/execution/backend/contract"
)

func TestRegistryRejectsDuplicatesAndReportsUnknownAdapters(t *testing.T) {
	if _, err := NewRegistry(NewMock(), NewMock()); err == nil {
		t.Fatal("duplicate adapter was accepted")
	}
	registry, err := NewRegistry(NewMock())
	if err != nil {
		t.Fatal(err)
	}
	if availability := registry.Probe(context.Background(), "missing"); availability.Available || !strings.Contains(availability.Reason, "未知") {
		t.Fatalf("unexpected availability: %#v", availability)
	}
}

func TestCommandTemplateCarriesFrozenAgentConfiguration(t *testing.T) {
	invocation := Invocation{Spec: contract.Spec{Preset: "research", Model: "model-a", SystemPrompt: "cite sources", Tools: []string{"web_search"}, Skills: []string{"citation-check"}, Prompt: "find evidence"}, Workspace: "/workspace", HarnessHome: "/harness"}
	argv := expandArgv([]string{"runner", "{preset}", "{model}", "{systemPrompt}", "{toolsJson}", "{skillsJson}", "{prompt}"}, invocation)
	if argv[1] != "research" || argv[2] != "model-a" || argv[3] != "cite sources" || argv[4] != `["web_search"]` || argv[5] != `["citation-check"]` {
		t.Fatalf("agent configuration was not expanded: %#v", argv)
	}
	if !strings.Contains(argv[6], "System instructions:\ncite sources") || !strings.Contains(argv[6], "Task:\nfind evidence") {
		t.Fatalf("effective prompt omitted frozen instructions: %s", argv[6])
	}
}

func TestFirstPartyDSHProbeRequiresCredentialAndBuiltCLI(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "apps/cli/lib/bin.js")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("// fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEEPSEEK_API_KEY", "")
	adapter := NewDSH(root)
	if availability := adapter.Probe(context.Background()); availability.Available || !strings.Contains(availability.Reason, "DEEPSEEK_API_KEY") {
		t.Fatalf("credential-less DSH unexpectedly available: %#v", availability)
	}
	t.Setenv("DEEPSEEK_API_KEY", "test-key")
	if availability := adapter.Probe(context.Background()); !availability.Available {
		t.Fatalf("configured DSH unavailable: %#v", availability)
	}
}

func TestFirstPartyDSHUsesPackagedCLIOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dsh.js")
	if err := os.WriteFile(path, []byte("// packaged fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RANK_DSH_BIN", path)
	adapter := NewDSH("/missing").(*dshAdapter)
	if adapter.commandPath != path {
		t.Fatalf("packaged DSH path = %q, want %q", adapter.commandPath, path)
	}
}
