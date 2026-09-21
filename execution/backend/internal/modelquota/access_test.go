package modelquota

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
)

type recorder struct {
	Repository
	requests int
	id       string
	attempt  int
}

func (r *recorder) ReserveModelPermit(_ context.Context, id string, attempt int, _ contract.ModelPermitRequest, _ time.Duration) (contract.ModelPermit, error) {
	r.requests++
	r.id = id
	r.attempt = attempt
	return contract.ModelPermit{Granted: true, ValidForMs: 500}, nil
}

func TestPermitIdentityCannotChangeExecutionOrAccessWithServiceToken(t *testing.T) {
	a, err := NewAuthority("http://localhost/v1/model-permits", strings.Repeat("a", 32), time.Second, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	runtime, err := a.Issue(contract.Execution{ID: "owned", Attempt: 2, StartedAt: &now, RuntimeTimeoutMs: 60_000, ModelQuota: &contract.ModelQuotaPlan{Group: "shared", MaxOutputTokens: 10}})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, token, body string
		status            int
	}{
		{"allowed", "Bearer " + runtime.Token, `{"sequence":1,"inputTokens":2}`, 200},
		{"service token", "Bearer " + strings.Repeat("b", 32), `{"sequence":1}`, 401},
		{"tampered", "Bearer " + runtime.Token + "x", `{"sequence":1}`, 401},
		{"missing scheme", runtime.Token, `{"sequence":1}`, 401},
		{"cross execution", "Bearer " + runtime.Token, `{"sequence":1,"executionId":"other"}`, 400},
		{"trailing json", "Bearer " + runtime.Token, `{"sequence":1}{}`, 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := &recorder{}
			request := httptest.NewRequest(http.MethodPost, "/v1/model-permits", strings.NewReader(test.body))
			request.Header.Set("Authorization", test.token)
			response := httptest.NewRecorder()
			a.Handler(repo).ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status=%d want=%d", response.Code, test.status)
			}
			if test.status == 200 {
				if repo.requests != 1 || repo.id != "owned" || repo.attempt != 2 {
					t.Fatal("identity not bound")
				}
			} else if repo.requests != 0 {
				t.Fatal("unauthorized request reached repository")
			}
		})
	}
	expired := now.Add(-2 * time.Minute)
	runtime, err = a.Issue(contract.Execution{ID: "owned", Attempt: 2, StartedAt: &expired, RuntimeTimeoutMs: 60_000, ModelQuota: &contract.ModelQuotaPlan{Group: "shared", MaxOutputTokens: 10}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.authorize(runtime.Token); err == nil {
		t.Fatal("expired identity authorized")
	}
}

func TestPermitConfigurationRejectsAmbiguousURLAndKey(t *testing.T) {
	for _, endpoint := range []string{"file:///v1/model-permits", "https://a/v1/model-permits?x=y", "https://a/v1/model-permits#x", "https://u:p@a/v1/model-permits", "https://a/other"} {
		if _, err := NewAuthority(endpoint, strings.Repeat("a", 32), time.Second, 5*time.Second); err == nil {
			t.Fatal("invalid endpoint accepted")
		}
	}
	if _, err := NewAuthority("https://a/v1/model-permits", "short", time.Second, 5*time.Second); err == nil {
		t.Fatal("short key accepted")
	}
}
