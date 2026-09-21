package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
)

// This built-runtime smoke exercises the real DSH CLI and provider serializer.
func TestDSHStructuredHistory(t *testing.T) {
	if os.Getenv("EXECUTION_TEST_DSH") != "1" {
		t.Skip("requires pnpm run build:lib:host and EXECUTION_TEST_DSH=1")
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	if packaged := os.Getenv("EXECUTION_TEST_RUNTIME"); packaged != "" {
		root = packaged
		t.Setenv("RANK_DSH_BIN", filepath.Join(root, "node_modules/@deepseek-ai/dsh/lib/bin.js"))
	} else {
		t.Setenv("RANK_DSH_BIN", "")
	}
	type wireMessage struct {
		Role      string          `json:"role"`
		Content   json.RawMessage `json:"content"`
		ToolCalls []struct {
			ID       string `json:"id"`
			Function struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"function"`
		} `json:"tool_calls"`
		ToolCallID string `json:"tool_call_id"`
	}
	for _, scenario := range []struct {
		name     string
		messages []contract.Message
	}{
		{"conversation", []contract.Message{{Role: "user", Content: "Remember Ada"}, {Role: "assistant", Content: "Remembered Ada"}, {Role: "user", Content: "Who did I mention?"}}},
		{"tool", []contract.Message{{Role: "user", Content: "Look up order"}, {Role: "assistant", ToolCalls: []contract.FunctionCall{{ID: "call_external_1", Name: "lookup_order", Arguments: json.RawMessage(`{"id":123}`)}}}, {Role: "tool", ToolCallID: "call_external_1", Content: `{"status":"shipped"}`}}},
		{"tool-and-user", []contract.Message{{Role: "user", Content: "Look up order"}, {Role: "assistant", ToolCalls: []contract.FunctionCall{{ID: "call_external_1", Name: "lookup_order", Arguments: json.RawMessage(`{"id":123}`)}}}, {Role: "tool", ToolCallID: "call_external_1", Content: `{"status":"shipped"}`}, {Role: "user", Content: "Answer briefly"}}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var mu sync.Mutex
			var requests [][]wireMessage
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/chat/completions" {
					http.Error(w, "unexpected provider path", 404)
					return
				}
				var body struct {
					Messages []wireMessage `json:"messages"`
					Stream   bool          `json:"stream"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					http.Error(w, err.Error(), 400)
					return
				}
				mu.Lock()
				requests = append(requests, body.Messages)
				mu.Unlock()
				if body.Stream {
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, `data: {"id":"chat-fixture","object":"chat.completion.chunk","model":"fixture-model","choices":[{"index":0,"delta":{"role":"assistant","content":"Verified history"},"finish_reason":null}]}`+"\n\n")
					fmt.Fprint(w, `data: {"id":"chat-fixture","object":"chat.completion.chunk","model":"fixture-model","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":3,"total_tokens":15}}`+"\n\n")
					fmt.Fprint(w, "data: [DONE]\n\n")
				} else {
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, `{"id":"chat-fixture","model":"fixture-model","choices":[{"index":0,"message":{"role":"assistant","content":"Verified history"},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":3,"total_tokens":15}}`)
				}
			}))
			defer server.Close()
			home, workspace, artifacts := t.TempDir(), t.TempDir(), t.TempDir()
			registry, err := NewRegistry(NewDSH(root))
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
			defer cancel()
			result, err := registry.Run(ctx, Invocation{MaxModelRequests: 32, ExecutionID: "history-" + scenario.name,
				Spec:        contract.Spec{Kind: contract.KindAgent, Harness: "dsh", Preset: "headless", Model: "fixture-model", ModelConnectionID: "fixture", Messages: scenario.messages},
				HarnessHome: home, Workspace: workspace, ArtifactDir: artifacts,
				ModelConnection: &contract.ModelConnection{ID: "fixture", Protocol: contract.ProtocolOpenAIChat, Provider: "custom", BaseURL: server.URL + "/v1"}, Credential: "fixture-key",
			})
			if err != nil {
				logs, _ := os.ReadFile(filepath.Join(artifacts, "stderr.txt"))
				t.Fatalf("native history execution: %v\n%s\n%+v", err, logs, result)
			}
			if strings.TrimSpace(result.Output) != "Verified history" {
				t.Fatalf("output: %+v", result)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(requests) != 1 {
				t.Fatalf("got %d provider requests, want exactly one", len(requests))
			}
			actual := requests[0]
			cursor := -1
			for _, expected := range scenario.messages {
				found := -1
				for i := cursor + 1; i < len(actual); i++ {
					msg := actual[i]
					if msg.Role != expected.Role {
						continue
					}
					var content string
					_ = json.Unmarshal(msg.Content, &content)
					if content != expected.Content {
						continue
					}
					if len(expected.ToolCalls) > 0 && (len(msg.ToolCalls) != 1 || msg.ToolCalls[0].ID != expected.ToolCalls[0].ID || msg.ToolCalls[0].Function.Name != expected.ToolCalls[0].Name || msg.ToolCalls[0].Function.Arguments != string(expected.ToolCalls[0].Arguments)) {
						continue
					}
					if msg.ToolCallID != expected.ToolCallID {
						continue
					}
					found = i
					break
				}
				if found < 0 {
					encoded, _ := json.Marshal(actual)
					t.Fatalf("missing message %+v in %s", expected, encoded)
				}
				if expected.Role == "tool" && found != cursor+1 {
					t.Fatalf("context inserted between tool call and result: %+v", actual)
				}
				cursor = found
			}
			if result.Usage.InputTokens != 12 || result.Usage.OutputTokens != 3 {
				t.Fatalf("usage counts imported history as a new execution: %+v", result.Usage)
			}
		})
	}
}
