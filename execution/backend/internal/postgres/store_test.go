package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/execution/backend/internal/domain"
	"github.com/xyzbit/agentkiln/execution/backend/internal/secret"
)

func testStore(t *testing.T, maxQueued int) *Store {
	t.Helper()
	url := os.Getenv("EXECUTION_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("EXECUTION_TEST_DATABASE_URL is required for PostgreSQL integration tests")
	}
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatal(err)
	}
	store, err := Open(t.Context(), Config{OperationTimeout: 5 * time.Second, URL: url, Schema: "execution_test_" + hex.EncodeToString(suffix[:]), MaxAttempts: 3, MaxQueued: maxQueued})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := store.pool.Exec(ctx, "DROP SCHEMA IF EXISTS "+pgx.Identifier{store.Schema()}.Sanitize()+" CASCADE"); err != nil {
			t.Error(err)
		}
		store.Close()
	})
	if err = store.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(t.Context()); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
	return store
}
func executionValue(id string) contract.Execution {
	return contract.Execution{QueueExpiresAt: time.Now().Add(time.Minute).Truncate(time.Millisecond), ID: id, IdempotencyKey: id, Status: contract.StatusQueued, Executor: "local-process", WorkerVersion: "test", Spec: contract.Spec{ProtocolVersion: contract.ProtocolVersion, Kind: contract.KindAgent, Harness: "mock", Prompt: "test"}, CreatedAt: time.Now().UTC()}
}
func queuedEvent(value contract.Execution) contract.Event {
	return contract.Event{ExecutionID: value.ID, Type: "execution.queued", Status: contract.StatusQueued, At: value.CreatedAt}
}

func TestCreateRollsBackRecordAndRiverJobWhenEventCannotBeStored(t *testing.T) {
	store := testStore(t, 2)
	value := executionValue("atomic-create")
	event := queuedEvent(value)
	event.Data = json.RawMessage(`{`)
	if err := store.Create(t.Context(), value, event); err == nil {
		t.Fatal("invalid event was accepted")
	}
	if _, err := store.Get(t.Context(), value.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("record survived rollback: %v", err)
	}
	var jobs int
	if err := store.pool.QueryRow(t.Context(), `SELECT count(*) FROM river_job`).Scan(&jobs); err != nil || jobs != 0 {
		t.Fatalf("jobs after rollback=%d: %v", jobs, err)
	}
	if err := store.Create(t.Context(), value, queuedEvent(value)); err != nil {
		t.Fatal(err)
	}
	var arguments json.RawMessage
	if err := store.pool.QueryRow(t.Context(), `SELECT args FROM river_job`).Scan(&arguments); err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(arguments, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 1 || decoded["executionId"] != value.ID {
		t.Fatalf("queue payload contains more than identity: %s", arguments)
	}
}

func TestConcurrentIdempotentCreationAndQueueAdmission(t *testing.T) {
	store := testStore(t, 1)
	value := executionValue("same-key")
	errorsCh := make(chan error, 8)
	var wait sync.WaitGroup
	for range 8 {
		wait.Add(1)
		go func() { defer wait.Done(); errorsCh <- store.Create(t.Context(), value, queuedEvent(value)) }()
	}
	wait.Wait()
	close(errorsCh)
	successes := 0
	for err := range errorsCh {
		if err == nil {
			successes++
		} else if !errors.Is(err, domain.ErrConflict) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatalf("accepted %d copies", successes)
	}
	other := executionValue("over-capacity")
	if err := store.Create(t.Context(), other, queuedEvent(other)); !errors.Is(err, domain.ErrOverloaded) {
		t.Fatalf("capacity result: %v", err)
	}
	var jobs int
	if err := store.pool.QueryRow(t.Context(), `SELECT count(*) FROM river_job`).Scan(&jobs); err != nil || jobs != 1 {
		t.Fatalf("jobs=%d: %v", jobs, err)
	}
	if err := store.RequestCancel(t.Context(), value.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(t.Context(), other, queuedEvent(other)); err != nil {
		t.Fatal(err)
	}
}

func TestCancellationWaitsForRuntimeStopAndRejectsStaleEvents(t *testing.T) {
	store := testStore(t, 5)
	value := executionValue("cancel-running")
	if err := store.Create(t.Context(), value, queuedEvent(value)); err != nil {
		t.Fatal(err)
	}
	event := contract.Event{ExecutionID: value.ID, Attempt: 1, Type: "execution.running", Status: contract.StatusRunning, At: time.Now().UTC()}
	if err := store.MarkRunning(t.Context(), value.ID, 1, "local-process", "worker-a", "test", event.At, event); err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	errorsCh := make(chan error, 12)
	for i := range 12 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			progress := event
			progress.Type = fmt.Sprintf("progress.%d", i)
			errorsCh <- store.AppendEvent(t.Context(), progress)
		}()
	}
	wait.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := store.RequestCancel(t.Context(), value.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	current, err := store.Get(t.Context(), value.ID)
	if err != nil || current.Status != contract.StatusCancelling || current.CompletedAt != nil {
		t.Fatalf("premature cancellation: %#v, %v", current, err)
	}
	if err := store.Complete(t.Context(), value.ID, contract.Result{Output: "late"}, time.Now().UTC(), event); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("late completion: %v", err)
	}
	event.Attempt = 2
	if err := store.AppendEvent(t.Context(), event); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale event: %v", err)
	}
	event.Attempt = 1
	event.Type = "execution.cancelled"
	event.Status = contract.StatusCancelled
	if err := store.ConfirmRuntimeStopped(t.Context(), value.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err := store.Cancel(t.Context(), value.ID, time.Now().UTC(), event); err != nil {
		t.Fatal(err)
	}
	if err := store.RequestCancel(t.Context(), value.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListEvents(t.Context(), value.ID, 0)
	if err != nil || len(events) != 16 {
		t.Fatalf("events=%d: %v", len(events), err)
	}
	for i, event := range events {
		if event.Sequence != int64(i+1) {
			t.Fatalf("event order: %#v", events)
		}
	}
}

func TestModelCredentialReferencesAndEncryptedValuesSurviveReload(t *testing.T) {
	store := testStore(t, 2)
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	credentials, err := secret.NewEncryptedStore(store.pool, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := credentials.Put(t.Context(), "connection-credential", "test-provider-key"); err != nil {
		t.Fatal(err)
	}
	value := contract.ModelConnection{ID: "connection", Name: "Model", Protocol: contract.ProtocolOpenAIChat, BaseURL: "https://example.invalid/v1", Models: []string{"test"}, CredentialRef: "connection-credential", Status: "verified", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := store.SaveModelConnection(t.Context(), value); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSystemModelBinding(t.Context(), contract.SystemModelBinding{Role: contract.SystemModelControl, ConnectionID: value.ID, Model: "test", UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	binding, err := store.GetSystemModelBinding(t.Context(), contract.SystemModelControl)
	if err != nil || binding.Connection.CredentialRef != value.CredentialRef || !binding.Connection.HasCredential {
		t.Fatalf("binding: %#v %v", binding, err)
	}
	loaded, err := secret.NewEncryptedStore(store.pool, key)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := loaded.Get(t.Context(), value.CredentialRef); err != nil || got != "test-provider-key" {
		t.Fatalf("credential reload failed: %v", err)
	}
	var stored []byte
	if err := store.pool.QueryRow(t.Context(), `SELECT ciphertext FROM credentials WHERE ref=$1`, value.CredentialRef).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if string(stored) == "test-provider-key" {
		t.Fatal("credential stored as plaintext")
	}
	if _, err := store.pool.Exec(t.Context(), `INSERT INTO credentials(ref,ciphertext) VALUES('another-reference',$1)`, stored); err != nil {
		t.Fatal(err)
	}
	if _, err := loaded.Get(t.Context(), "another-reference"); err == nil {
		t.Fatal("credential ciphertext accepted for a different reference")
	}
	if err := store.DeleteModelConnection(t.Context(), value.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("bound connection deletion: %v", err)
	}
}
