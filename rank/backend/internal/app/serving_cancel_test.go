package app_test

import (
	"context"
	"testing"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/rank/backend/internal/app"
	"github.com/xyzbit/agentkiln/rank/backend/internal/domain"
)

type cancellationRaceRepo struct {
	domain.Repository
	response   domain.ServingResponse
	nextStatus string
	events     []domain.ResponseEvent
}

func (r *cancellationRaceRepo) GetResponse(ctx context.Context, _ domain.ResponseID) (domain.ServingResponse, error) {
	return r.response, ctx.Err()
}

func (r *cancellationRaceRepo) WithinTx(ctx context.Context, fn func(context.Context, domain.Repository) error) error {
	return fn(ctx, r)
}

func (r *cancellationRaceRepo) UpdateResponse(_ context.Context, response domain.ServingResponse, expected string, events []domain.ResponseEvent) error {
	if r.response.Status != expected {
		return domain.ErrConflict
	}
	r.response = response
	r.events = append(r.events, events...)
	return nil
}

func (r *cancellationRaceRepo) RequestResponseCancellation(_ context.Context, _ domain.ResponseID) error {
	if r.nextStatus != "" {
		r.response.Status, r.response.ExecutionID = r.nextStatus, "execution-started"
		r.nextStatus = ""
	}
	if !domain.IsResponseTerminal(r.response.Status) {
		r.response.CancelRequested = true
	}
	return nil
}

type cancellationExecution struct {
	app.ServingExecutionClient
	cancelled []string
}

func (e *cancellationExecution) Cancel(_ context.Context, id string) (contract.Execution, error) {
	e.cancelled = append(e.cancelled, id)
	return contract.Execution{ID: id, Status: contract.StatusCancelled}, nil
}

func TestCancelResponseFollowsExecutionStartedDuringCancellation(t *testing.T) {
	for _, next := range []string{domain.ResponseInProgress, domain.ResponseCompleted} {
		t.Run(next, func(t *testing.T) {
			repo := &cancellationRaceRepo{response: domain.ServingResponse{ID: "response-race", TenantID: "local", Status: domain.ResponseQueued}, nextStatus: next}
			execution := &cancellationExecution{}
			service := app.NewService(repo, app.Options{Execution: execution})
			t.Cleanup(service.StopServingResponses)
			response, err := service.CancelServingResponse(context.Background(), domain.APICredential{TenantID: "local"}, repo.response.ID)
			if err != nil {
				t.Fatal(err)
			}
			if next == domain.ResponseCompleted {
				if response.Status != next || len(execution.cancelled) != 0 || len(repo.events) != 0 {
					t.Fatal("completed response was overwritten")
				}
				return
			}
			if response.Status != domain.ResponseCancelled || len(execution.cancelled) != 1 || execution.cancelled[0] != "execution-started" {
				t.Fatalf("cancellation lost the running execution: response=%s cancelled=%v", response.Status, execution.cancelled)
			}
			if len(repo.events) != 1 || repo.events[0].Type != "response.cancelled" {
				t.Fatalf("events=%v", repo.events)
			}
		})
	}
}
