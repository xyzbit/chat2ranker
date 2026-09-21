package app_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/execution/backend/internal/app"
	"github.com/xyzbit/agentkiln/execution/backend/internal/domain"
	"github.com/xyzbit/agentkiln/execution/backend/internal/sqlite"
)

type runtimeOutcome struct{ err error }

func (*runtimeOutcome) Name() string { return "test" }
func (*runtimeOutcome) Probe(context.Context, string) contract.Availability {
	return contract.Availability{Available: true}
}
func (r *runtimeOutcome) Run(context.Context, contract.Execution, domain.EventSink) (contract.Result, error) {
	return contract.Result{Output: "done"}, r.err
}

func TestServiceReleasesOnlyConfirmedRuntimeOutcomes(t *testing.T) {
	for _, test := range []struct {
		name    string
		err     error
		pending bool
		status  string
	}{
		{"complete", nil, false, contract.StatusCompleted},
		{"stopped with failure", errors.New("runtime exited"), false, contract.StatusFailed},
		{"unknown stop", fmt.Errorf("cleanup: %w", domain.ErrRuntimeStopUnconfirmed), true, contract.StatusFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "execution.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			service := app.NewService(store, &runtimeOutcome{err: test.err}, app.Options{ExecutionTimeout: time.Second})
			value, err := service.Submit(t.Context(), contract.SubmitRequest{IdempotencyKey: "once", Spec: contract.Spec{ProtocolVersion: contract.ProtocolVersion, Kind: contract.KindAgent, Harness: "mock", Prompt: "hello"}})
			if err != nil {
				t.Fatal(err)
			}
			if err := service.Execute(t.Context(), value.ID); err != nil {
				t.Fatal(err)
			}
			current, err := store.Get(t.Context(), value.ID)
			if err != nil || current.Status != test.status || current.RuntimeCleanupPending != test.pending {
				t.Fatalf("outcome: %#v %v", current, err)
			}
			// A redelivery acknowledges the business outcome, without inventing cleanup evidence.
			if err := service.Execute(t.Context(), value.ID); err != nil {
				t.Fatal(err)
			}
			current, err = store.Get(t.Context(), value.ID)
			if err != nil || current.RuntimeCleanupPending != test.pending || current.Attempt != 1 {
				t.Fatalf("redelivery changed cleanup: %#v %v", current, err)
			}
		})
	}
}

func (*runtimeOutcome) Cleanup(context.Context, contract.Execution) error {
	return domain.ErrRuntimeStopUnconfirmed
}
