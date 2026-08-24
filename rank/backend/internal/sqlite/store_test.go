package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/xyzbit/chat2ranker/rank/backend/internal/domain"
	"github.com/xyzbit/chat2ranker/rank/backend/internal/sqlite"
)

func openStore(t *testing.T) (*sqlite.Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rank.db")
	store, err := sqlite.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, path
}

func fixture(t *testing.T, repo domain.Repository) (domain.DatasetVersion, domain.AgentVersion, domain.Experiment) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	dataset := domain.DatasetVersion{ID: "dataset-contract-v1", FamilyID: "dataset-contract", Name: "Contract cases", Version: 1, Source: "test", Description: "v1", Schema: []byte(`{}`), Rubric: []byte(`{}`), Cases: []domain.Case{{ID: "case-1", Title: "One", Input: "Do one", Expected: map[string]any{"summary": "done"}}}, CreatedAt: now, CaseCount: 1}
	agent := domain.AgentVersion{ID: "agent-contract-v1", FamilyID: "agent-contract", Name: "Contract agent", Handle: "@test/contract", Version: 1, RunnerType: "mock", Description: "v1", Model: "demo", Tools: []string{"files"}, CreatedAt: now}
	experiment := domain.Experiment{ID: "exp-contract", Title: "Contract", DatasetVersionID: dataset.ID, AgentVersionID: agent.ID, CreatedAt: now, UpdatedAt: now}
	err := repo.WithinTx(ctx, func(tx domain.Repository) error {
		if err := tx.CreateDatasetFamily(ctx, domain.DatasetFamily{ID: dataset.FamilyID, Name: dataset.Name, Description: dataset.Description, LatestVersionID: dataset.ID, CreatedAt: now}, dataset); err != nil {
			return err
		}
		if err := tx.CreateAgentFamily(ctx, domain.AgentFamily{ID: agent.FamilyID, Name: agent.Name, Handle: agent.Handle, Description: agent.Description, LatestVersionID: agent.ID, CreatedAt: now}, agent); err != nil {
			return err
		}
		return tx.CreateExperiment(ctx, experiment, domain.Message{ID: "msg-initial", ExperimentID: experiment.ID, Role: "assistant", Content: "start", CreatedAt: now})
	})
	if err != nil {
		t.Fatal(err)
	}
	return dataset, agent, experiment
}

func TestRepositoryTransactionRollback(t *testing.T) {
	store, _ := openStore(t)
	ctx := context.Background()
	var foreignKeys int
	if err := store.DB().QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign keys must be enabled, got %d", foreignKeys)
	}
	sentinel := errors.New("rollback")
	err := store.WithinTx(ctx, func(repo domain.Repository) error {
		now := time.Now().UTC()
		version := domain.DatasetVersion{ID: "rolled-back-v1", FamilyID: "rolled-back", Name: "Rolled back", Version: 1, Source: "test", Description: "test", Schema: []byte(`{}`), Rubric: []byte(`{}`), Cases: []domain.Case{{ID: "c", Title: "c", Input: "c", Expected: map[string]any{}}}, CreatedAt: now}
		if err := repo.CreateDatasetFamily(ctx, domain.DatasetFamily{ID: "rolled-back", Name: "Rolled back", Description: "test", LatestVersionID: version.ID, CreatedAt: now}, version); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected rollback error, got %v", err)
	}
	families, err := store.ListDatasetFamilies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(families) != 0 {
		t.Fatalf("rollback persisted %d families", len(families))
	}
}

func TestRepositoryContractPersistsSnapshotsAndIdempotentCallbacks(t *testing.T) {
	store, path := openStore(t)
	ctx := context.Background()
	dataset, agent, experiment := fixture(t, store)
	now := time.Now().UTC()
	run := domain.Run{ID: "run-contract", ExperimentID: experiment.ID, IdempotencyKey: "start-1", Status: domain.RunQueued, DatasetSnapshot: dataset, AgentSnapshot: agent, TrialCount: 1, Concurrency: 1, CreatedAt: now, Total: 1, ScheduledTrials: 1, CaseCount: 1, CostKnown: true}
	item := domain.RunItem{ID: "item-contract", RunID: run.ID, CaseID: "case-1", Title: "One", Ordinal: 0, Status: domain.ItemQueued, CreatedAt: now}
	trial := domain.RunTrial{ID: "trial-contract", RunID: run.ID, ItemID: item.ID, CaseID: item.CaseID, TrialIndex: 1, Ordinal: 0, Status: domain.TrialQueued, CreatedAt: now}
	if err := store.WithinTx(ctx, func(repo domain.Repository) error {
		return repo.CreateRun(ctx, run, []domain.RunItem{item}, []domain.RunTrial{trial}, domain.RunEvent{RunID: run.ID, Type: "run.created", At: now, DatasetVersionID: dataset.ID, AgentVersionID: agent.ID})
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.WithinTx(ctx, func(repo domain.Repository) error {
		claimed, err := repo.ClaimRunTrial(ctx, run.ID, trial.ID, now)
		if err != nil {
			return err
		}
		if !claimed {
			t.Fatal("item was not claimed")
		}
		result := domain.TrialResult{ID: trial.ID, RunID: run.ID, CaseID: item.CaseID, TrialIndex: 1, Status: domain.TrialComplete, Valid: true, Passed: true, Cost: .02, CostKnown: true, Score: 1, Output: "done", Reason: "ok", CreatedAt: now}
		inserted, err := repo.CompleteRunTrial(ctx, trial.ID, "callback-1", result, now, []domain.RunEvent{{RunID: run.ID, Type: "trial.completed", CaseID: item.CaseID, TrialID: trial.ID, TrialIndex: 1, At: now}})
		if err != nil {
			return err
		}
		if !inserted {
			t.Fatal("first callback was not inserted")
		}
		replayed, err := repo.CompleteRunTrial(ctx, trial.ID, "callback-1", result, now, nil)
		if err != nil {
			return err
		}
		if replayed {
			t.Fatal("replayed callback inserted a second result")
		}
		if _, err = repo.CompleteRunTrial(ctx, trial.ID, "different-key", result, now, nil); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("different callback key should conflict: %v", err)
		}
		aggregate := domain.CaseResult{CaseID: item.CaseID, Title: item.Title, Passed: true, Reliable: true, TrialCount: 1, ValidTrials: 1, PassCount: 1, PassRate: 100, Cost: .02, CostKnown: true, Score: 1, Output: "done", Reason: "1/1 次均通过"}
		if err := repo.SaveRunItemAggregate(ctx, item.ID, aggregate, now); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.GetRunByIdempotencyKey(ctx, experiment.ID, "start-1")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID != run.ID || len(loaded.Results) != 1 || loaded.DatasetSnapshot.ID != dataset.ID {
		t.Fatalf("unexpected loaded run: %#v", loaded)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	loaded, err = reopened.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Results) != 1 || loaded.AgentSnapshot.ID != agent.ID {
		t.Fatalf("restart lost persisted state: %#v", loaded)
	}
}

var _ domain.Repository = (*sqlite.Store)(nil)
