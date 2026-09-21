package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/execution/backend/internal/modelquota"
)

func quotaFixture(t *testing.T, s *Store, requests, tokens int64) contract.ModelQuota {
	t.Helper()
	for _, id := range []string{"quota-a", "quota-b"} {
		if err := s.SaveModelConnection(t.Context(), contract.ModelConnection{ID: id, Name: id, Status: "verified"}); err != nil {
			t.Fatal(err)
		}
	}
	quota := contract.ModelQuota{ID: "shared", Connections: []string{"quota-a", "quota-b"}, RequestsPerMinute: requests, TokensPerMinute: tokens, MaxOutputTokens: 10}
	if err := s.ConfigureModelQuota(t.Context(), quota); err != nil {
		t.Fatal(err)
	}
	return quota
}

func runningQuotaExecution(t *testing.T, s *Store, id, connection string) contract.Execution {
	t.Helper()
	value := executionValue(id)
	value.Spec.ModelConnectionID = connection
	value.Spec.Model = "fixture"
	value.RuntimeTimeoutMs = time.Minute.Milliseconds()
	value.MaxModelRequests = 32
	var err error
	value.ModelQuota, err = s.ModelQuotaForConnection(t.Context(), connection)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Create(t.Context(), value, queuedEvent(value)); err != nil {
		t.Fatal(err)
	}
	started := time.Now().UTC()
	if err = s.MarkRunning(t.Context(), id, 1, "local-process", "owner", "v1", started, contract.Event{ExecutionID: id, Attempt: 1, Type: "execution.running", At: started}); err != nil {
		t.Fatal(err)
	}
	value, err = s.Get(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestModelQuotaSharedAcrossConnectionsAndIndependentPools(t *testing.T) {
	s := testStore(t, 50)
	quotaFixture(t, s, 3, 0)
	a := runningQuotaExecution(t, s, "a", "quota-a")
	b := runningQuotaExecution(t, s, "b", "quota-b")
	other, err := Open(t.Context(), s.config)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	type observation struct {
		permit contract.ModelPermit
		err    error
	}
	results := make(chan observation, 20)
	var wg sync.WaitGroup
	for n := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			owner, id := s, a.ID
			if n%2 == 1 {
				owner, id = other, b.ID
			}
			permit, err := owner.ReserveModelPermit(t.Context(), id, 1, contract.ModelPermitRequest{Sequence: n + 1, InputTokens: 2}, time.Second)
			results <- observation{permit, err}
		}()
	}
	wg.Wait()
	close(results)
	granted := 0
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.permit.Granted {
			granted++
		} else if result.permit.RetryAfterMs <= 0 {
			t.Fatal("denial has no retry delay")
		}
	}
	if granted != 3 {
		t.Fatalf("provider starts=%d, want 3 across both owners and connections", granted)
	}
	var retained int
	if err = s.pool.QueryRow(t.Context(), `SELECT count(*) FROM model_permits`).Scan(&retained); err != nil || retained != 3 {
		t.Fatalf("retained=%d err=%v", retained, err)
	}
}

func TestModelQuotaReconciliationAndUnknownUsage(t *testing.T) {
	s := testStore(t, 10)
	quotaFixture(t, s, 0, 30)
	a := runningQuotaExecution(t, s, "a", "quota-a")
	b := runningQuotaExecution(t, s, "b", "quota-b")
	first := contract.ModelPermitRequest{Sequence: 1, InputTokens: 10}
	p, err := s.ReserveModelPermit(t.Context(), a.ID, 1, first, time.Second)
	if err != nil || !p.Granted {
		t.Fatalf("first=%+v err=%v", p, err)
	}
	p, err = s.ReserveModelPermit(t.Context(), b.ID, 1, first, time.Second)
	if err != nil || p.Granted {
		t.Fatalf("unknown usage must keep 20-token estimate: %+v %v", p, err)
	}
	if err = s.ReconcileModelPermit(t.Context(), a.ID, 1, 1, 5); err != nil {
		t.Fatal(err)
	}
	if err = s.ReconcileModelPermit(t.Context(), a.ID, 1, 1, 5); err != nil {
		t.Fatal("idempotent report", err)
	}
	if err = s.ReconcileModelPermit(t.Context(), a.ID, 1, 1, 0); !errors.Is(err, modelquota.ErrConflict) {
		t.Fatalf("changed usage=%v", err)
	}
	p, err = s.ReserveModelPermit(t.Context(), b.ID, 1, first, time.Second)
	if err != nil || !p.Granted {
		t.Fatalf("reconciled allowance: %+v %v", p, err)
	}
	if err = s.ReconcileModelPermit(t.Context(), b.ID, 1, 1, 40); err != nil {
		t.Fatal(err)
	}
	p, err = s.ReserveModelPermit(t.Context(), a.ID, 1, contract.ModelPermitRequest{Sequence: 2, InputTokens: 0}, time.Second)
	if err != nil || p.Granted {
		t.Fatalf("usage beyond estimate must block: %+v %v", p, err)
	}
}

func TestModelQuotaDoesNotRenewDuplicateOrExpiredPermits(t *testing.T) {
	s := testStore(t, 10)
	quotaFixture(t, s, 2, 0)
	a := runningQuotaExecution(t, s, "a", "quota-a")
	input := contract.ModelPermitRequest{Sequence: 1, InputTokens: 2}
	first, err := s.ReserveModelPermit(t.Context(), a.ID, 1, input, time.Second)
	if err != nil || !first.Granted {
		t.Fatal(first, err)
	}
	repeated, err := s.ReserveModelPermit(t.Context(), a.ID, 1, input, time.Second)
	if err != nil || !repeated.Granted || repeated.ValidForMs > first.ValidForMs {
		t.Fatal(repeated, err)
	}
	if _, err = s.ReserveModelPermit(t.Context(), a.ID, 1, contract.ModelPermitRequest{Sequence: 1, InputTokens: 3}, time.Second); !errors.Is(err, modelquota.ErrConflict) {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(t.Context(), `UPDATE model_permits SET valid_until_ms=0 WHERE execution_id=$1`, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReserveModelPermit(t.Context(), a.ID, 1, input, time.Second); !errors.Is(err, modelquota.ErrConflict) {
		t.Fatal("expired permit renewed", err)
	}
	if _, err = s.ReserveModelPermit(t.Context(), a.ID, 2, input, time.Second); !errors.Is(err, modelquota.ErrInactive) {
		t.Fatal("stale owner", err)
	}
	if err = s.RequestCancel(t.Context(), a.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReserveModelPermit(t.Context(), a.ID, 1, contract.ModelPermitRequest{Sequence: 2}, time.Second); !errors.Is(err, modelquota.ErrInactive) {
		t.Fatal("cancelled owner", err)
	}
}

func TestModelQuotaConfigEditsPreserveUsageAndRejectLiveMembershipChanges(t *testing.T) {
	s := testStore(t, 10)
	quota := quotaFixture(t, s, 2, 0)
	a := runningQuotaExecution(t, s, "a", "quota-a")
	if _, err := s.ReserveModelPermit(t.Context(), a.ID, 1, contract.ModelPermitRequest{Sequence: 1}, time.Second); err != nil {
		t.Fatal(err)
	}
	quota.RequestsPerMinute = 1
	if err := s.ConfigureModelQuota(t.Context(), quota); err != nil {
		t.Fatal(err)
	}
	p, err := s.ReserveModelPermit(t.Context(), a.ID, 1, contract.ModelPermitRequest{Sequence: 2}, time.Second)
	if err != nil || p.Granted {
		t.Fatal("configuration reset live usage", p, err)
	}
	quota.Connections = []string{"quota-b"}
	if err = s.ConfigureModelQuota(t.Context(), quota); err == nil {
		t.Fatal("live account move accepted")
	}
	quota.ID = "other"
	if err = s.ConfigureModelQuota(t.Context(), quota); err == nil {
		t.Fatal("duplicate account membership accepted")
	}
	// The failed update rolls back both its group and membership deletions.
	plan, err := s.ModelQuotaForConnection(t.Context(), "quota-b")
	if err != nil || plan.Group != "shared" {
		t.Fatal(plan, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = s.ReserveModelPermit(ctx, a.ID, 1, contract.ModelPermitRequest{Sequence: 2}, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestModelQuotaRejectsStaleAdmissionAndKeepsActivePermitIdentity(t *testing.T) {
	s := testStore(t, 10)
	quota := quotaFixture(t, s, 1, 0)
	stale := executionValue("stale")
	stale.Spec.ModelConnectionID = "quota-a"
	stale.ModelQuota, _ = s.ModelQuotaForConnection(t.Context(), "quota-a")
	quota.MaxOutputTokens = 20
	if err := s.ConfigureModelQuota(t.Context(), quota); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(t.Context(), stale, queuedEvent(stale)); !errors.Is(err, modelquota.ErrConflict) {
		t.Fatalf("stale output cap admitted: %v", err)
	}
	a := runningQuotaExecution(t, s, "active", "quota-a")
	input := contract.ModelPermitRequest{Sequence: 1, InputTokens: 2}
	if p, err := s.ReserveModelPermit(t.Context(), a.ID, 1, input, time.Second); err != nil || !p.Granted {
		t.Fatal(p, err)
	}
	if _, err := s.pool.Exec(t.Context(), `UPDATE model_permits SET expires_at_ms=0,valid_until_ms=0 WHERE execution_id=$1`, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.PruneModelPermits(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReserveModelPermit(t.Context(), a.ID, 1, input, time.Second); !errors.Is(err, modelquota.ErrConflict) {
		t.Fatalf("expired identity was renewed: %v", err)
	}
	if p, err := s.ReserveModelPermit(t.Context(), a.ID, 1, contract.ModelPermitRequest{Sequence: 2}, time.Second); err != nil || !p.Granted {
		t.Fatalf("expired debit still consumes quota: %+v %v", p, err)
	}
	now := time.Now().UTC()
	if err := s.Fail(t.Context(), a.ID, "fixture complete", now, contract.Event{ExecutionID: a.ID, Attempt: 1, Type: "execution.failed", At: now}); err != nil {
		t.Fatal(err)
	}
	if err := s.PruneModelPermits(t.Context()); err != nil {
		t.Fatal(err)
	}
	var retained int
	if err := s.pool.QueryRow(t.Context(), `SELECT count(*) FROM model_permits WHERE execution_id=$1`, a.ID).Scan(&retained); err != nil || retained != 1 {
		t.Fatalf("expired terminal records=%d %v", retained, err)
	}
}
