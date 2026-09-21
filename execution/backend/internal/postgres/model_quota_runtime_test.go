package postgres

import (
	"encoding/json"
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
	"github.com/xyzbit/agentkiln/execution/backend/harness"
	"github.com/xyzbit/agentkiln/execution/backend/internal/modelquota"
)

func TestModelQuotaRealDSHRequestsAndUsage(t *testing.T) {
	if os.Getenv("EXECUTION_TEST_DSH") != "1" {
		t.Skip("requires built DSH and EXECUTION_TEST_DSH=1")
	}
	s := testStore(t, 10)
	quotaFixture(t, s, 2, 1_000_000)
	value := runningQuotaExecution(t, s, "dsh-quota", "quota-a")
	var authority *modelquota.Authority
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { authority.Handler(s).ServeHTTP(w, r) }))
	defer callback.Close()
	var err error
	authority, err = modelquota.NewAuthority(callback.URL+"/v1/model-permits", strings.Repeat("x", 32), time.Second, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		if request["max_tokens"] != float64(10) && request["max_completion_tokens"] != float64(10) {
			t.Errorf("output cap absent: %v", request["max_tokens"])
		}
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "0.01")
			w.WriteHeader(429)
			fmt.Fprint(w, `{"error":{"message":"rate limit","type":"rate_limit_error"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"id\":\"quota\",\"model\":\"fixture\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Quota verified\"},\"finish_reason\":null}]}\n\n")
		fmt.Fprint(w, "data: {\"id\":\"quota\",\"model\":\"fixture\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":3}}\n\ndata: [DONE]\n\n")
	}))
	defer provider.Close()
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("RANK_DSH_BIN", "")
	registry, err := harness.NewRegistry(harness.NewDSH(root))
	if err != nil {
		t.Fatal(err)
	}
	quota, err := authority.Issue(value)
	if err != nil {
		t.Fatal(err)
	}
	value.Spec.Harness = "dsh"
	value.Spec.Messages = []contract.Message{{Role: "user", Content: "Reply briefly"}}
	value.Spec.Prompt = ""
	result, err := registry.Run(t.Context(), harness.Invocation{ModelQuota: quota, MaxModelRequests: 4, ExecutionID: value.ID, Spec: value.Spec, HarnessHome: t.TempDir(), Workspace: t.TempDir(), ArtifactDir: t.TempDir(), ModelConnection: &contract.ModelConnection{ID: "quota-a", Protocol: contract.ProtocolOpenAIChat, Provider: "custom", BaseURL: provider.URL + "/v1"}, Credential: "fixture"})
	if err != nil || strings.TrimSpace(result.Output) != "Quota verified" {
		t.Fatalf("DSH result=%+v error=%v", result, err)
	}
	var permits, known int
	var reported int64
	if err = s.pool.QueryRow(t.Context(), `SELECT count(*),count(actual_tokens),COALESCE(sum(actual_tokens),0) FROM model_permits WHERE execution_id=$1`, value.ID).Scan(&permits, &known, &reported); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || permits != 2 || known != 1 || reported != 15 {
		t.Fatalf("requests=%d permits=%d reportedCalls=%d tokens=%d", calls.Load(), permits, known, reported)
	}
}
