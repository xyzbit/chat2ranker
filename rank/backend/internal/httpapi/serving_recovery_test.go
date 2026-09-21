package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/rank/backend/internal/app"
	"github.com/xyzbit/agentkiln/rank/backend/internal/domain"
)

// lostSubmit simulates a stopped observer before or after remote acceptance.
type lostSubmit struct {
	*fakeServingExecution
	accepted bool
	entered  chan struct{}
	once     sync.Once
}

func (e *lostSubmit) Submit(ctx context.Context, request contract.SubmitRequest) (contract.Execution, error) {
	if e.accepted {
		if _, err := e.fakeServingExecution.Submit(ctx, request); err != nil {
			return contract.Execution{}, err
		}
	}
	e.once.Do(func() { close(e.entered) })
	<-ctx.Done()
	return contract.Execution{}, ctx.Err()
}

type lostIDCommit struct {
	domain.Repository
	entered chan struct{}
	once    sync.Once
}

func (r *lostIDCommit) UpdateResponse(ctx context.Context, response domain.ServingResponse, expected string, events []domain.ResponseEvent) error {
	if expected == domain.ResponseQueued {
		r.once.Do(func() { close(r.entered) })
		<-ctx.Done()
		return ctx.Err()
	}
	return r.Repository.UpdateResponse(ctx, response, expected, events)
}

func TestServingRecoveryFreezesDispatchAndCancellationBeforeExecutionID(t *testing.T) {
	for _, window := range []string{"before_submit", "accepted_reply_lost", "before_id_commit"} {
		for _, cancel := range []bool{false, true} {
			t.Run(window+map[bool]string{false: "/complete", true: "/cancel"}[cancel], func(t *testing.T) {
				remote := newFakeServingExecution(false)
				_, _, deployment, key, store := setupServingHTTP(t, remote, domain.DeploymentPolicy{RequestsPerMinute: 100, MaxConcurrentResponses: 4, ExecutionTimeoutMs: 30000})
				entered := make(chan struct{})
				var repo domain.Repository = store
				var execution app.ServingExecutionClient = &lostSubmit{fakeServingExecution: remote, accepted: window == "accepted_reply_lost", entered: entered}
				if window == "before_id_commit" {
					repo = &lostIDCommit{Repository: store, entered: entered}
					execution = remote
				}
				old := app.NewService(repo, app.Options{Execution: execution})
				t.Cleanup(old.StopServingResponses)
				credential, err := old.AuthenticateServingCredential(t.Context(), key, domain.ScopeResponsesCreate)
				if err != nil {
					t.Fatal(err)
				}
				response, _, err := old.CreateServingResponse(t.Context(), credential, app.CreateServingResponseInput{Model: "research-public@production", Input: json.RawMessage(`"keep original settings"`), Background: true, IdempotencyKey: "recovery"})
				if err != nil {
					t.Fatal(err)
				}
				select {
				case <-entered:
				case <-time.After(3 * time.Second):
					t.Fatal("fault window not reached")
				}
				old.StopServingResponses()
				persisted, err := store.GetResponse(t.Context(), response.ID)
				if err != nil || persisted.ExecutionID != "" || persisted.Status != domain.ResponseQueued || len(persisted.ExecutionRequest) == 0 {
					t.Fatalf("lost dispatch intent: %+v %v", persisted, err)
				}
				// Updating a live endpoint cannot change an already accepted invocation.
				changed, _ := json.Marshal(domain.DeploymentPolicy{RequestsPerMinute: 100, MaxConcurrentResponses: 4, ExecutionTimeoutMs: 99999})
				if _, err := store.DB().ExecContext(t.Context(), `UPDATE deployments SET policy_json=$1 WHERE public_id=$2`, string(changed), deployment.ID); err != nil {
					t.Fatal(err)
				}
				if cancel {
					pending, err := old.CancelServingResponse(t.Context(), credential, response.ID)
					if err != nil || !pending.CancelRequested || domain.IsResponseTerminal(pending.Status) {
						t.Fatalf("cancel claimed unconfirmed termination: %+v %v", pending, err)
					}
				}
				remote.mu.Lock()
				remote.autoComplete = true
				remote.mu.Unlock()
				resumed := app.NewService(store, app.Options{Execution: remote, DispatchRetryInterval: time.Millisecond})
				t.Cleanup(resumed.StopServingResponses)
				if err := resumed.ResumeActiveResponses(t.Context()); err != nil {
					t.Fatal(err)
				}
				ctx, stop := context.WithTimeout(t.Context(), 3*time.Second)
				defer stop()
				final, err := resumed.WaitServingResponse(ctx, response.ID)
				expected := domain.ResponseCompleted
				if cancel {
					expected = domain.ResponseCancelled
				}
				if err != nil || final.Status != expected {
					t.Fatalf("recovery outcome=%s err=%v", final.Status, err)
				}
				remote.mu.Lock()
				count := remote.next
				spec := remote.executions[final.ExecutionID].Spec
				remote.mu.Unlock()
				if count != 1 || spec.TimeoutMs != 30000 {
					t.Fatalf("replayed or changed invocation: count=%d timeout=%d", count, spec.TimeoutMs)
				}
				events, err := store.ListResponseEvents(t.Context(), response.ID, 0)
				if err != nil {
					t.Fatal(err)
				}
				terminals := 0
				for _, event := range events {
					if event.Type == "response."+expected {
						terminals++
					}
				}
				if terminals != 1 {
					t.Fatalf("terminal event count=%d", terminals)
				}
			})
		}
	}
}

type disconnectWatch struct {
	*fakeServingExecution
	entered chan struct{}
	once    sync.Once
}

func (e *disconnectWatch) Watch(context.Context, string, int64, func(contract.Event) error) (int64, error) {
	e.once.Do(func() { close(e.entered) })
	return 0, errors.New("connection reset")
}

func TestServingDisconnectedObserverLeavesExecutionActive(t *testing.T) {
	remote := newFakeServingExecution(false)
	_, _, _, key, store := setupServingHTTP(t, remote, domain.DeploymentPolicy{RequestsPerMinute: 100, MaxConcurrentResponses: 4, ExecutionTimeoutMs: 30000})
	execution := &disconnectWatch{fakeServingExecution: remote, entered: make(chan struct{})}
	service := app.NewService(store, app.Options{Execution: execution, DispatchRetryInterval: time.Millisecond})
	t.Cleanup(service.StopServingResponses)
	credential, err := service.AuthenticateServingCredential(t.Context(), key, domain.ScopeResponsesCreate)
	if err != nil {
		t.Fatal(err)
	}
	response, _, err := service.CreateServingResponse(t.Context(), credential, app.CreateServingResponseInput{Model: "research-public@production", Input: json.RawMessage(`"watch disconnect"`), Background: true})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-execution.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("observer not entered")
	}
	service.StopServingResponses()
	current, err := store.GetResponse(t.Context(), response.ID)
	if err != nil || current.Status != domain.ResponseInProgress {
		t.Fatalf("transport error became terminal: %+v %v", current, err)
	}
	value, err := remote.Get(t.Context(), current.ExecutionID)
	if err != nil || contract.IsTerminal(value.Status) {
		t.Fatalf("observation stopped remote execution: %+v %v", value, err)
	}
}

type replayProgress struct {
	*fakeServingExecution
	cursors []int64
}

func (e *replayProgress) Watch(ctx context.Context, id string, after int64, handle func(contract.Event) error) (int64, error) {
	e.cursors = append(e.cursors, after)
	sequences := []int64{1, 1, 2}
	if len(e.cursors) > 1 {
		sequences = []int64{2, 3}
	}
	for _, sequence := range sequences {
		if err := handle(contract.Event{Sequence: sequence, ExecutionID: id, Type: "harness.stdout", Message: "part", At: time.Now().UTC()}); err != nil {
			return sequence, err
		}
	}
	if len(e.cursors) == 1 {
		return 2, errors.New("event connection reset")
	}
	e.mu.Lock()
	e.completeLocked(id)
	e.mu.Unlock()
	return 3, nil
}

func TestTrialRecoveryCommitsProgressAndCursorTogether(t *testing.T) {
	remote := newFakeServingExecution(false)
	_, _, _, _, store := setupServingHTTP(t, remote, domain.DeploymentPolicy{RequestsPerMinute: 100, MaxConcurrentResponses: 4, ExecutionTimeoutMs: 30000})
	execution := &replayProgress{fakeServingExecution: remote}
	service := app.NewService(store, app.Options{Execution: execution, DispatchRetryInterval: time.Millisecond})
	t.Cleanup(service.StopServingResponses)
	response, _, err := service.CreateTrialResponse(t.Context(), app.TrialResponseInput{AgentVersionID: "agent-research-demo-v1", Input: "progress replay"}, "trial-replay")
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithTimeout(t.Context(), 3*time.Second)
	defer stop()
	final, err := service.WaitServingResponse(ctx, response.ID)
	if err != nil || final.Status != domain.ResponseCompleted {
		t.Fatalf("trial outcome=%s err=%v", final.Status, err)
	}
	service.StopServingResponses()
	if len(execution.cursors) != 2 || execution.cursors[0] != 0 || execution.cursors[1] != 2 {
		t.Fatalf("replay cursors=%v", execution.cursors)
	}
	events, err := store.ListResponseEvents(t.Context(), response.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var progress []string
	for _, event := range events {
		if event.Type == "trial.progress" {
			var data struct{ Text string }
			if err := json.Unmarshal(event.Data, &data); err != nil {
				t.Fatal(err)
			}
			progress = append(progress, data.Text)
		}
	}
	if len(progress) != 3 || progress[0] != "part" || progress[1] != "partpart" || progress[2] != "partpartpart" {
		t.Fatalf("duplicate replay changed text: %v", progress)
	}
	if final.ExecutionSequence != 3 {
		t.Fatalf("cursor not durable: %d", final.ExecutionSequence)
	}
}

func TestTrialProgressRollbackPreservesOutputAndCursor(t *testing.T) {
	remote := newFakeServingExecution(false)
	_, _, _, _, store := setupServingHTTP(t, remote, domain.DeploymentPolicy{RequestsPerMinute: 100, MaxConcurrentResponses: 4, ExecutionTimeoutMs: 30000})
	service := app.NewService(store, app.Options{Execution: remote})
	service.StopServingResponses()
	response, _, err := service.CreateTrialResponse(t.Context(), app.TrialResponseInput{AgentVersionID: "agent-research-demo-v1", Input: "transactional progress"}, "progress-rollback")
	if err != nil {
		t.Fatal(err)
	}
	next := response
	next.ExecutionSequence = 1
	next.OutputText = "part"
	event := domain.ResponseEvent{ResponseID: response.ID, Type: "trial.progress", Data: json.RawMessage(`{"text":"part","executionSequence":1}`), CreatedAt: time.Now().UTC()}
	aborted := errors.New("abort progress transaction")
	err = store.WithinTx(t.Context(), func(ctx context.Context, repo domain.Repository) error {
		if err := repo.UpdateResponseProgress(t.Context(), next, 0, []domain.ResponseEvent{event}); err != nil {
			return err
		}
		return aborted
	})
	if !errors.Is(err, aborted) {
		t.Fatal(err)
	}
	current, err := store.GetResponse(t.Context(), response.ID)
	if err != nil || current.ExecutionSequence != 0 || current.OutputText != "" {
		t.Fatalf("partial progress survived: %+v %v", current, err)
	}
	events, err := store.ListResponseEvents(t.Context(), response.ID, 0)
	if err != nil || len(events) != 1 {
		t.Fatalf("partial event survived: %d %v", len(events), err)
	}
	if err := store.UpdateResponseProgress(t.Context(), next, 0, []domain.ResponseEvent{event}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateResponseProgress(t.Context(), next, 0, []domain.ResponseEvent{event}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale cursor accepted: %v", err)
	}
	events, err = store.ListResponseEvents(t.Context(), response.ID, 0)
	if err != nil || len(events) != 2 {
		t.Fatalf("duplicate event committed: %d %v", len(events), err)
	}
}
