package sqlstore_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/xyzbit/agentkiln/rank/backend/internal/domain"
	"github.com/xyzbit/agentkiln/rank/backend/internal/sqlstore"
)

func postgresPair(t *testing.T, migrate bool) (*sqlstore.Store, *sqlstore.Store) {
	t.Helper()
	url := os.Getenv("RANK_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("RANK_TEST_DATABASE_URL is required for PostgreSQL integration tests")
	}
	schema := "rank_pg_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	config := sqlstore.PostgresConfig{OperationTimeout: 5 * time.Second, URL: url, Schema: schema, MaxConnections: 8}
	first, err := sqlstore.OpenPostgres(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := first.DB().ExecContext(ctx, "DROP SCHEMA IF EXISTS "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Error(err)
		}
		first.Close()
	})
	second, err := sqlstore.OpenPostgres(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { second.Close() })
	if migrate {
		if err := first.MigratePostgres(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	return first, second
}

func TestPostgresPoolAndTransactionLockDeadlines(t *testing.T) {
	first, _ := postgresPair(t, true)
	var schema string
	if err := first.DB().QueryRowContext(t.Context(), "SELECT current_schema()").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	short, err := sqlstore.OpenPostgres(t.Context(), sqlstore.PostgresConfig{
		URL: os.Getenv("RANK_TEST_DATABASE_URL"), Schema: schema, MaxConnections: 1, OperationTimeout: 250 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer short.Close()
	check := func(operation func(context.Context) error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		defer cancel()
		start := time.Now()
		if err := operation(ctx); !errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil || time.Since(start) > time.Second {
			t.Fatalf("deadline did not bound PostgreSQL: elapsed=%s err=%v", time.Since(start), err)
		}
	}
	conn, err := short.DB().Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	check(func(ctx context.Context) error { _, err := short.ListAgentFamilies(ctx); return err })
	conn.Close()
	// Hold the same lock used by business writes from a different connection.
	lock, err := first.DB().BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback()
	if _, err := lock.ExecContext(t.Context(), "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", schema+"/rank-writes"); err != nil {
		t.Fatal(err)
	}
	called := false
	check(func(ctx context.Context) error {
		return short.WithinTx(ctx, func(context.Context, domain.Repository) error { called = true; return nil })
	})
	if called {
		t.Fatal("blocked transaction invoked its callback")
	}
	lock.Rollback()
	if err := short.WithinTx(t.Context(), func(ctx context.Context, repo domain.Repository) error {
		_, err := repo.ListAgentFamilies(ctx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	lock, err = first.DB().BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback()
	if _, err := lock.ExecContext(t.Context(), "LOCK TABLE agent_families IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	err = short.WithinTx(t.Context(), func(ctx context.Context, repo domain.Repository) error {
		// A blocked query must use the remainder of the transaction budget.
		timer := time.NewTimer(175 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return ctx.Err()
		}
		_, err := repo.ListAgentFamilies(ctx)
		return err
	})
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 375*time.Millisecond {
		t.Fatalf("callback reset its transaction budget: elapsed=%s err=%v", time.Since(start), err)
	}
}

func TestPostgresExplicitMigrationAndVersionRefusal(t *testing.T) {
	first, second := postgresPair(t, false)
	ctx := t.Context()
	if err := first.CheckPostgresSchema(ctx); err == nil {
		t.Fatal("unmigrated schema accepted")
	}
	errorsCh := make(chan error, 2)
	for _, store := range []*sqlstore.Store{first, second} {
		go func() { errorsCh <- store.MigratePostgres(ctx) }()
	}
	for range 2 {
		if err := <-errorsCh; err != nil {
			t.Fatal(err)
		}
	}
	if err := second.CheckPostgresSchema(ctx); err != nil {
		t.Fatal(err)
	}
	fixture(t, first)
	if err := second.MigratePostgres(ctx); err != nil {
		t.Fatal(err)
	}
	if families, err := second.ListAgentFamilies(ctx); err != nil || len(families) != 1 {
		t.Fatalf("repeat migration lost data: %v, %v", families, err)
	}
	if _, err := first.DB().ExecContext(ctx, `INSERT INTO schema_migrations(version,applied_at) VALUES(999,'future')`); err != nil {
		t.Fatal(err)
	}
	if err := second.CheckPostgresSchema(ctx); err == nil {
		t.Fatal("future schema accepted for serving")
	}
	if err := second.MigratePostgres(ctx); err == nil {
		t.Fatal("future schema accepted for migration")
	}
}

func TestPostgresAdmissionAndConversationHeadAcrossPools(t *testing.T) {
	first, second := postgresPair(t, true)
	_, agent, _ := fixture(t, first)
	ctx := t.Context()
	now := time.Now().UTC()
	type admission struct {
		response domain.ServingResponse
		err      error
	}
	results := make(chan admission, 8)
	stores := []*sqlstore.Store{first, second}
	for i := range 8 {
		go func() {
			conversation := domain.Conversation{ID: domain.ConversationID(fmt.Sprintf("conversation-%d", i)), TenantID: "tenant", AgentVersionID: agent.ID, Trial: true, CreatedAt: now, UpdatedAt: now}
			response := domain.ServingResponse{ID: domain.ResponseID(fmt.Sprintf("response-%d", i)), TenantID: "tenant", AgentVersionID: agent.ID, ConversationID: conversation.ID, IdempotencyKey: fmt.Sprintf("key-%d", i), Status: domain.ResponseQueued, CreatedAt: now}
			err := stores[i%2].AdmitResponse(ctx, response, &conversation, domain.ResponseEvent{ResponseID: response.ID, Type: "response.created", Data: []byte(`{}`), CreatedAt: now}, 100, 1)
			results <- admission{response, err}
		}()
	}
	var accepted domain.ServingResponse
	successes := 0
	for range 8 {
		result := <-results
		if result.err == nil {
			successes++
			accepted = result.response
		} else if !errors.Is(result.err, domain.ErrConcurrencyLimit) {
			t.Fatal(result.err)
		}
	}
	if successes != 1 {
		t.Fatalf("admitted %d responses for one slot", successes)
	}
	var conversations int
	if err := second.DB().QueryRowContext(ctx, `SELECT count(*) FROM conversations`).Scan(&conversations); err != nil || conversations != 1 {
		t.Fatalf("rejected admission leaked conversations: %d, %v", conversations, err)
	}
	completed := accepted
	completed.Status = domain.ResponseCompleted
	completed.CompletedAt = &now
	if err := first.UpdateResponse(ctx, completed, domain.ResponseQueued, nil); err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		go func() {
			response := accepted
			response.ID = domain.ResponseID(fmt.Sprintf("continuation-%d", i))
			response.IdempotencyKey = string(response.ID)
			response.PreviousResponseID = accepted.ID
			err := stores[i].AdmitResponse(ctx, response, nil, domain.ResponseEvent{ResponseID: response.ID, Type: "response.created", Data: []byte(`{}`), CreatedAt: now}, 100, 100)
			results <- admission{response, err}
		}()
	}
	successes = 0
	for range 2 {
		result := <-results
		if result.err == nil {
			successes++
		} else if !errors.Is(result.err, domain.ErrConversationHead) {
			t.Fatal(result.err)
		}
	}
	if successes != 1 {
		t.Fatalf("accepted %d writers for the same conversation head", successes)
	}
}

func TestPostgresReleaseCASRollsBackLosingRevision(t *testing.T) {
	first, second := postgresPair(t, true)
	_, agent, _ := fixture(t, first)
	ctx := t.Context()
	now := time.Now().UTC()
	deployment := domain.Deployment{ID: "deployment", TenantID: "tenant", AgentFamilyID: agent.FamilyID, Name: "endpoint", CreatedAt: now}
	alias := domain.DeploymentAlias{ID: "alias", DeploymentID: deployment.ID, Name: "production", CreatedAt: now, UpdatedAt: now}
	if err := first.WithinTx(ctx, func(ctx context.Context, repo domain.Repository) error {
		return repo.CreateDeployment(ctx, deployment, []domain.DeploymentAlias{alias})
	}); err != nil {
		t.Fatal(err)
	}
	errorsCh := make(chan error, 2)
	for i, store := range []*sqlstore.Store{first, second} {
		go func() {
			_, err := store.AppendReleaseRevision(ctx, domain.ReleaseRevision{ID: domain.ReleaseRevisionID(fmt.Sprintf("release-%d", i)), DeploymentAliasID: alias.ID, AgentVersionID: agent.ID, PolicyResult: domain.ReleasePolicyManualOverride, CreatedAt: now}, "")
			errorsCh <- err
		}()
	}
	winners := 0
	for range 2 {
		err := <-errorsCh
		if err == nil {
			winners++
		} else if !errors.Is(err, domain.ErrConflict) {
			t.Fatal(err)
		}
	}
	var count int
	if err := first.DB().QueryRowContext(ctx, `SELECT count(*) FROM release_revisions`).Scan(&count); err != nil || count != 1 || winners != 1 {
		t.Fatalf("CAS revisions=%d winners=%d: %v", count, winners, err)
	}
}

func TestPostgresTransactionRollbackAndOrderedEvents(t *testing.T) {
	first, second := postgresPair(t, true)
	_, agent, _ := fixture(t, first)
	ctx := t.Context()
	now := time.Now().UTC()
	conversation := domain.Conversation{ID: "conversation", TenantID: "tenant", AgentVersionID: agent.ID, Trial: true, CreatedAt: now, UpdatedAt: now}
	response := domain.ServingResponse{ID: "response", TenantID: "tenant", AgentVersionID: agent.ID, ConversationID: conversation.ID, IdempotencyKey: "once", Status: domain.ResponseQueued, CreatedAt: now}
	created := domain.ResponseEvent{ResponseID: response.ID, Type: "response.created", Data: []byte(`{}`), CreatedAt: now}
	if err := first.AdmitResponse(ctx, response, &conversation, created, 100, 1); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("abort business transaction")
	err := first.WithinTx(ctx, func(ctx context.Context, repo domain.Repository) error {
		changed := response
		changed.Status = domain.ResponseCompleted
		if err := repo.UpdateResponse(ctx, changed, response.Status, []domain.ResponseEvent{created}); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	loaded, err := second.GetResponse(ctx, response.ID)
	if err != nil || loaded.Status != domain.ResponseQueued {
		t.Fatalf("rollback failed: %v, %v", loaded, err)
	}
	var wg sync.WaitGroup
	errorsCh := make(chan error, 12)
	stores := []*sqlstore.Store{first, second}
	for i := range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errorsCh <- stores[i%2].UpdateResponse(ctx, response, response.Status, []domain.ResponseEvent{created})
		}()
	}
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	events, err := second.ListResponseEvents(ctx, response.ID, 0)
	if err != nil || len(events) != 13 {
		t.Fatalf("events=%d, %v", len(events), err)
	}
	for i, event := range events {
		if event.Sequence != int64(i+1) {
			t.Fatalf("event order: %v", events)
		}
	}
}
