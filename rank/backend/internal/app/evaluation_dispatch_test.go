package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	executionclient "github.com/xyzbit/agentkiln/execution/backend/client"
	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/rank/backend/internal/domain"
	"github.com/xyzbit/agentkiln/rank/backend/internal/sqlstore"
	"github.com/xyzbit/agentkiln/rank/backend/internal/testdb"
)

type invocationRemote struct {
	mu                       sync.Mutex
	execution                contract.Execution
	starts, submits, cancels int
	loseReply                bool
	holdReply                chan struct{}
	accepted                 chan struct{}
	disconnect               bool
	failed                   bool
}

func TestEvaluationAdmissionFreezesModelRoutingAndCreatesIntent(t *testing.T) {
	store, _, _ := invocationFixture(t)
	var mu sync.Mutex
	model, bindingCalls := "judge-original", 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/runtimes/codex" {
			json.NewEncoder(w).Encode(contract.Availability{Available: true, Installed: true, Configured: true})
			return
		}
		if r.URL.Path == "/v1/system-model-bindings/judge" {
			mu.Lock()
			defer mu.Unlock()
			bindingCalls++
			json.NewEncoder(w).Encode(contract.SystemModelBinding{ConnectionID: "connection-original", Model: model, Connection: contract.ModelConnection{Status: "verified"}})
			return
		}
		http.NotFound(w, r)
	}))
	defer upstream.Close()
	client := executionclient.New(upstream.URL, time.Second)
	newService := func(tenant string, timeout time.Duration) *Service {
		service := NewService(store, Options{Runners: ExecutionRunners(ExecutionRunnerConfig{Client: client, TenantID: tenant, Timeout: timeout})})
		t.Cleanup(service.StopRunWorkers)
		t.Cleanup(service.StopServingResponses)
		return service
	}
	service := newService("tenant-original", 30*time.Second)
	dataset, err := service.CreateDataset(t.Context(), CreateDatasetInput{Name: "Judge snapshot", Rubric: json.RawMessage(`{"criteria":[{"id":"quality","name":"Quality","description":"Complete task","weight":1,"threshold":0.7,"critical":true}]}`), Cases: []domain.Case{{ID: "one", Input: "freeze me"}}})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := store.GetAgentVersion(t.Context(), "agent-research-demo-v1")
	if err != nil {
		t.Fatal(err)
	}
	agent.ID = "agent-config-frozen"
	agent.Version = 2
	agent.RunnerType = "codex"
	if err := store.CreateAgentVersion(t.Context(), agent); err != nil {
		t.Fatal(err)
	}
	experiment, err := service.CreateExperiment(t.Context(), "Frozen routing")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateExperiment(t.Context(), experiment.ID, ExperimentPatch{DatasetVersionID: &dataset.ID, AgentVersionID: &agent.ID}); err != nil {
		t.Fatal(err)
	}
	run, err := service.StartRun(t.Context(), experiment.ID, "frozen-routing", RunOptions{TrialCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	if run.EvaluatorSnapshot.Judge.Model != "judge-original" || run.EvaluatorSnapshot.Judge.ModelConnectionID != "connection-original" {
		t.Fatalf("judge binding not frozen: %+v", run.EvaluatorSnapshot.Judge)
	}
	intents, err := store.ListEvaluationInvocations(t.Context(), run.ID)
	if err != nil || len(intents) != 1 {
		t.Fatalf("admission omitted candidate intent: %d %v", len(intents), err)
	}
	if intents[0].SubmissionStarted {
		t.Fatal("admission launched runtime")
	}
	mu.Lock()
	model = "judge-changed"
	mu.Unlock()
	resumed := newService("tenant-changed", 99*time.Second)
	replay, err := resumed.StartRun(t.Context(), experiment.ID, "frozen-routing", RunOptions{TrialCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	runner := resumed.runners["codex"].(ExecutionRunner)
	var received contract.SubmitRequest
	runner.invoke = func(_ context.Context, request contract.SubmitRequest, _ func(contract.Event) error) (contract.Execution, error) {
		received = request
		return contract.Execution{ID: "judge", Status: contract.StatusCompleted, Result: &contract.Result{Score: 1, Reason: "passed"}}, nil
	}
	_, err = runner.RunJudge(t.Context(), JudgeSpec{RunID: replay.ID, TrialID: intents[0].TrialID, Settings: replay.ExecutionSettings, Agent: replay.AgentSnapshot, Evaluator: replay.EvaluatorSnapshot, Criterion: replay.EvaluatorSnapshot.Rubric[0], Case: replay.DatasetSnapshot.Cases[0], Candidate: CandidateResult{Output: "candidate"}})
	if err != nil {
		t.Fatal(err)
	}
	if received.Spec.Model != "judge-original" || received.Spec.ModelConnectionID != "connection-original" || received.Spec.TimeoutMs != 30000 || received.Spec.Metadata["tenantId"] != "tenant-original" {
		t.Fatalf("recovery used mutable settings: %+v", received.Spec)
	}
	mu.Lock()
	calls := bindingCalls
	mu.Unlock()
	if calls != 1 {
		t.Fatalf("recovery reread model binding: %d", calls)
	}
}

func (e *invocationRemote) Submit(ctx context.Context, request contract.SubmitRequest) (contract.Execution, error) {
	e.mu.Lock()
	e.submits++
	if e.execution.ID == "" {
		e.starts++
		e.execution = contract.Execution{ID: "execution-original", Spec: request.Spec, Status: contract.StatusRunning}
	}
	value := e.execution
	lose := e.loseReply
	e.loseReply = false
	hold, accepted := e.holdReply, e.accepted
	e.holdReply = nil
	e.accepted = nil
	e.mu.Unlock()
	if accepted != nil {
		close(accepted)
	}
	if hold != nil {
		select {
		case <-ctx.Done():
			return contract.Execution{}, ctx.Err()
		case <-hold:
		}
	}
	if lose {
		return contract.Execution{}, errors.New("accepted reply lost")
	}
	return value, nil
}

func (e *invocationRemote) Get(context.Context, string) (contract.Execution, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.execution, nil
}

func (e *invocationRemote) Watch(ctx context.Context, id string, after int64, emit func(contract.Event) error) (int64, error) {
	for _, sequence := range []int64{1, 1, 2} {
		if emit != nil {
			if err := emit(contract.Event{ExecutionID: id, Sequence: sequence, Type: "execution.running", At: time.Now().UTC()}); err != nil {
				return after, err
			}
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.disconnect {
		e.disconnect = false
		return 2, errors.New("event stream disconnected")
	}
	if e.execution.Status != contract.StatusCancelled {
		e.execution.Status = contract.StatusCompleted
		e.execution.Result = &contract.Result{Output: "original output", Cost: 0.01, CostKnown: true}
		if e.failed {
			e.execution.Status = contract.StatusFailed
			e.execution.Error = "unknown_outcome"
		}
	}
	return 2, ctx.Err()
}

func (e *invocationRemote) Cancel(context.Context, string) (contract.Execution, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cancels++
	e.execution.Status = contract.StatusCancelled
	return e.execution, nil
}

type invocationCommitFault struct {
	domain.Repository
	idOnce, resultOnce bool
}

func (r *invocationCommitFault) SaveInvocationExecution(ctx context.Context, id domain.InvocationID, executionID string) error {
	if r.idOnce {
		r.idOnce = false
		return errors.New("execution ID commit unavailable")
	}
	return r.Repository.SaveInvocationExecution(ctx, id, executionID)
}

func (r *invocationCommitFault) CompleteInvocation(ctx context.Context, id domain.InvocationID, result json.RawMessage) error {
	if r.resultOnce {
		r.resultOnce = false
		return errors.New("result commit unavailable")
	}
	return r.Repository.CompleteInvocation(ctx, id, result)
}

func invocationFixture(t *testing.T) (*sqlstore.Store, domain.Run, contract.SubmitRequest) {
	t.Helper()
	store, err := testdb.Open(t, filepath.Join(t.TempDir(), "rank.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	service := NewService(store, Options{})
	t.Cleanup(service.StopRunWorkers)
	t.Cleanup(service.StopServingResponses)
	if err := service.EnsureSeed(t.Context()); err != nil {
		t.Fatal(err)
	}
	dataset, err := service.CreateDataset(t.Context(), CreateDatasetInput{Name: "Recovery", Cases: []domain.Case{{ID: "one", Input: "original input"}}})
	if err != nil {
		t.Fatal(err)
	}
	experiment, err := service.CreateExperiment(t.Context(), "Recovery")
	if err != nil {
		t.Fatal(err)
	}
	agent := "agent-research-demo-v1"
	if _, err := service.UpdateExperiment(t.Context(), experiment.ID, ExperimentPatch{DatasetVersionID: &dataset.ID, AgentVersionID: &agent}); err != nil {
		t.Fatal(err)
	}
	run, err := service.StartRun(t.Context(), experiment.ID, "recovery", RunOptions{TrialCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	trials, err := store.ListRunTrials(t.Context(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	request := contract.SubmitRequest{IdempotencyKey: run.ID + ":" + trials[0].ID + ":candidate", Spec: contract.Spec{ProtocolVersion: contract.ProtocolVersion, Kind: contract.KindAgent, Harness: "mock", Prompt: "original input", TimeoutMs: 30000, Metadata: map[string]string{"runId": run.ID, "trialId": trials[0].ID}}}
	payload, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WithinTx(t.Context(), func(ctx context.Context, repo domain.Repository) error {
		return repo.CreateEvaluationInvocation(ctx, domain.EvaluationInvocation{ID: domain.InvocationID(request.IdempotencyKey), RunID: run.ID, TrialID: trials[0].ID, Request: payload})
	}); err != nil {
		t.Fatal(err)
	}
	return store, run, request
}

func TestEvaluationDispatchReconcilesOriginalInvocation(t *testing.T) {
	for _, window := range []string{"reply_lost", "id_commit", "stream_disconnected", "result_commit", "unknown_outcome"} {
		t.Run(window, func(t *testing.T) {
			store, run, request := invocationFixture(t)
			remote := &invocationRemote{loseReply: window == "reply_lost", disconnect: window == "stream_disconnected", failed: window == "unknown_outcome"}
			repo := &invocationCommitFault{Repository: store, idOnce: window == "id_commit", resultOnce: window == "result_commit"}
			dispatcher := evaluationDispatcher{repo: repo, client: remote, retry: time.Millisecond, observe: time.Second}
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			id := domain.InvocationID(request.IdempotencyKey)
			emit := func(event contract.Event) error {
				return store.WithinTx(ctx, func(ctx context.Context, repo domain.Repository) error {
					invocation, err := repo.GetEvaluationInvocation(ctx, id)
					if err != nil {
						return err
					}
					if event.Sequence <= invocation.Sequence {
						return nil
					}
					if err := repo.AdvanceInvocationSequence(ctx, id, invocation.Sequence, event.Sequence); err != nil {
						return err
					}
					return repo.AppendRunEvent(ctx, domain.RunEvent{RunID: run.ID, Type: "candidate.running", At: event.At})
				})
			}
			first, err := dispatcher.invoke(ctx, request, emit)
			if window == "unknown_outcome" {
				if err == nil || first.Error != "unknown_outcome" {
					t.Fatalf("unknown outcome lost: %+v %v", first, err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			originalSubmits := remote.submits
			request.Spec.TimeoutMs = 99999
			replay, replayErr := dispatcher.invoke(ctx, request, emit)
			if (replayErr != nil) != (err != nil) || replay.ID != first.ID || replay.Spec.TimeoutMs != 30000 || remote.starts != 1 || remote.submits != originalSubmits {
				t.Fatalf("replayed or changed accepted work: %+v starts=%d", replay, remote.starts)
			}
			events, err := store.ListRunEvents(ctx, run.ID, 0)
			if err != nil {
				t.Fatal(err)
			}
			if len(events) != 3 {
				t.Fatalf("duplicate progress events: %d", len(events))
			}
			if remote.cancels != 0 {
				t.Fatal("observation failure cancelled remote work")
			}
		})
	}
}

func TestEvaluationCancellationBeforeAndAfterPossibleAcceptance(t *testing.T) {
	for _, accepted := range []bool{false, true} {
		t.Run(map[bool]string{false: "before_submit", true: "reply_lost"}[accepted], func(t *testing.T) {
			store, run, request := invocationFixture(t)
			remote := &invocationRemote{}
			dispatcher := evaluationDispatcher{repo: store, client: remote, retry: time.Millisecond, observe: time.Second}
			if accepted {
				remote.holdReply = make(chan struct{})
				remote.accepted = make(chan struct{})
				reached := remote.accepted
				ctx, stop := context.WithCancel(t.Context())
				done := make(chan struct{})
				go func() { defer close(done); _, _ = dispatcher.invoke(ctx, request, nil) }()
				select {
				case <-reached:
				case <-time.After(3 * time.Second):
					t.Fatal("acceptance not reached")
				}
				stop()
				<-done
			}
			if err := store.RequestRunCancellation(t.Context(), run.ID); err != nil {
				t.Fatal(err)
			}
			resumed := NewService(store, Options{Workers: true, DispatchRetryInterval: time.Millisecond})
			resumed.runners["mock"] = ExecutionRunner{invoke: dispatcher.invoke}
			t.Cleanup(resumed.StopRunWorkers)
			t.Cleanup(resumed.StopServingResponses)
			if err := resumed.ResumeActiveRuns(t.Context()); err != nil {
				t.Fatal(err)
			}
			final := awaitEvaluationRun(t, resumed, run.ID)
			resumed.StopRunWorkers()
			if final.Status != domain.RunCancelled {
				t.Fatalf("unconfirmed cancellation: %s", final.Status)
			}
			invocation, err := store.GetEvaluationInvocation(t.Context(), domain.InvocationID(request.IdempotencyKey))
			if err != nil {
				t.Fatal(err)
			}
			outcome, err := invocationOutcome(invocation.Result)
			if outcome.Status != contract.StatusCancelled || err == nil {
				t.Fatalf("missing cancellation evidence: %+v %v", outcome, err)
			}
			want := 0
			if accepted {
				want = 1
			}
			if remote.starts != want || remote.cancels != want {
				t.Fatalf("unexpected runtime lifecycle: starts=%d cancels=%d", remote.starts, remote.cancels)
			}
		})
	}
}
