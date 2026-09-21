package harness

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
)

func TestDSHModelBudgetCountsActualProviderRequests(t *testing.T) {
	if os.Getenv("EXECUTION_TEST_DSH") != "1" {
		t.Skip("requires built DSH runtime and EXECUTION_TEST_DSH=1")
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("RANK_DSH_BIN", "")
	for _, test := range []struct {
		name             string
		limit, status    int
		history, recover bool
		requests         int
		tool             bool
	}{
		{"prompt-cap-one", 1, 429, false, false, 1, false},
		{"history-cap-two", 2, 429, true, false, 2, false},
		{"authentication-no-retry", 4, 401, true, false, 1, false},
		{"transient-recovers", 2, 429, true, true, 2, false},
		{"tool-cycle-keeps-usage", 1, 429, true, false, 1, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/chat/completions" {
					t.Errorf("unexpected model path: %s", r.URL.Path)
					http.NotFound(w, r)
					return
				}
				count := calls.Add(1)
				if test.tool && count == 1 {
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, `data: {"id":"budget","model":"fixture-model","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_budget_tool","type":"function","function":{"name":"bash","arguments":"{\"command\":\"true\",\"description\":\"Budget test\"}"}}]},"finish_reason":null}]}`+"\n\n")
					fmt.Fprint(w, `data: {"id":"budget","model":"fixture-model","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":12,"completion_tokens":3}}`+"\n\ndata: [DONE]\n\n")
					return
				}
				if !test.recover || count == 1 {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(test.status)
					fmt.Fprint(w, `{"error":{"message":"request rate limited","type":"rate_limit_error"}}`)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"id\":\"budget\",\"model\":\"fixture-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Recovered\"},\"finish_reason\":null}]}\n\n")
				fmt.Fprint(w, "data: {\"id\":\"budget\",\"model\":\"fixture-model\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":3}}\n\ndata: [DONE]\n\n")
			}))
			defer server.Close()
			registry, err := NewRegistry(NewDSH(root))
			if err != nil {
				t.Fatal(err)
			}
			spec := contract.Spec{ProtocolVersion: contract.ProtocolVersion, Kind: contract.KindAgent, Harness: "dsh", Model: "fixture-model", ModelConnectionID: "fixture", Prompt: "Hello"}
			if test.history {
				spec.Prompt = ""
				spec.Messages = []contract.Message{{Role: "user", Content: "Hello"}}
			}
			artifacts := t.TempDir()
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			result, err := registry.Run(ctx, Invocation{MaxModelRequests: test.limit, ExecutionID: "budget-" + test.name, Spec: spec, HarnessHome: t.TempDir(), Workspace: t.TempDir(), ArtifactDir: artifacts, ModelConnection: &contract.ModelConnection{ID: "fixture", Protocol: contract.ProtocolOpenAIChat, Provider: "custom", BaseURL: server.URL + "/v1"}, Credential: "fixture-key"})
			if int(calls.Load()) != test.requests {
				logs, _ := os.ReadFile(filepath.Join(artifacts, "stderr.txt"))
				t.Fatalf("requests=%d expected=%d, error=%v, stderr=%s", calls.Load(), test.requests, err, logs)
			}
			if test.recover {
				if err != nil || strings.TrimSpace(result.Output) != "Recovered" {
					t.Fatalf("recovery failed: %+v %v", result, err)
				}
			} else if err == nil {
				t.Fatal("model failure returned success")
			}
			if test.tool && (result.Usage.InputTokens != 12 || result.Usage.OutputTokens != 3) {
				t.Fatalf("budget failure discarded previous usage: %+v %v", result, err)
			}
			if test.status == 429 && !test.recover {
				logs, _ := os.ReadFile(filepath.Join(artifacts, "stderr.txt"))
				if !strings.Contains(string(logs), "execution_model_budget_exhausted") {
					t.Fatalf("missing budget diagnostic: %v %s", err, logs)
				}
			}
		})
	}
}
