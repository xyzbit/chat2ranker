package sqlstore_test

import (
	"context"
	"github.com/xyzbit/agentkiln/rank/backend/internal/domain"
	"testing"
)

func TestTrialMigrationPreservesServingHistoryAndForeignKeys(t *testing.T) {
	store, _ := openStore(t)
	_, agent, _ := fixture(t, store)
	ctx := context.Background()
	now := agent.CreatedAt
	deployment := domain.Deployment{ID: "deploy-migration", TenantID: "local", AgentFamilyID: agent.FamilyID, Name: "migration", CreatedAt: now}
	if err := store.CreateDeployment(ctx, deployment, nil); err != nil {
		t.Fatal(err)
	}
	credential := domain.APICredential{ID: "key-migration", TenantID: "local", Name: "migration", SecretHash: "migration-hash", CreatedAt: now}
	if err := store.CreateAPICredential(ctx, credential); err != nil {
		t.Fatal(err)
	}
	conversation := domain.Conversation{ID: "conv-migration", TenantID: "local", DeploymentID: deployment.ID, AgentVersionID: agent.ID, CreatedAt: now, UpdatedAt: now}
	response := domain.ServingResponse{ID: "resp-migration", TenantID: "local", CredentialID: credential.ID, DeploymentID: deployment.ID, DeploymentName: deployment.Name, ConversationID: conversation.ID, AgentVersionID: agent.ID, Status: domain.ResponseCompleted, Input: "hello", OutputText: "answer", IdempotencyKey: "migration", CreatedAt: now}
	event := domain.ResponseEvent{ResponseID: response.ID, Type: "response.completed", Data: []byte(`{"message":"retained"}`), CreatedAt: now}
	if err := store.AdmitResponse(ctx, response, &conversation, event, 60, 4); err != nil {
		t.Fatal(err)
	}
	// Restore the pre-trial conversation columns to exercise a populated upgrade.
	if _, err := store.DB().ExecContext(ctx, "ALTER TABLE responses DROP COLUMN execution_request_json; ALTER TABLE responses DROP COLUMN cancel_requested; ALTER TABLE responses DROP COLUMN execution_sequence; ALTER TABLE conversations DROP COLUMN trial; DROP INDEX responses_trial_idempotency; DELETE FROM schema_migrations WHERE version=20"); err != nil {
		t.Fatal(err)
	}
	if err := store.MigrateSQLite(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.MigrateSQLite(ctx); err != nil {
		t.Fatal(err)
	}
	retained, err := store.GetResponse(ctx, response.ID)
	if err != nil || retained.Input != "hello" || retained.OutputText != "answer" || retained.CredentialID != credential.ID {
		t.Fatalf("lost response: %#v %v", retained, err)
	}
	current, err := store.GetConversation(ctx, conversation.ID)
	if err != nil || current.Trial || current.LastResponseID != response.ID {
		t.Fatalf("lost head: %#v %v", current, err)
	}
	events, err := store.ListResponseEvents(ctx, response.ID, 0)
	if err != nil || len(events) != 1 || string(events[0].Data) != string(event.Data) {
		t.Fatalf("lost events: %#v %v", events, err)
	}
	var enabled int
	if err := store.DB().QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&enabled); err != nil || enabled != 1 {
		t.Fatalf("foreign keys disabled: %v", err)
	}
	trial := domain.Conversation{ID: "conv-trial", TenantID: "local", AgentVersionID: agent.ID, Trial: true, CreatedAt: now, UpdatedAt: now}
	response.ID, response.CredentialID, response.DeploymentID, response.ConversationID, response.IdempotencyKey = "resp-trial", "", "", trial.ID, "trial"
	event.ResponseID = response.ID
	if err := store.AdmitResponse(ctx, response, &trial, event, 60, 4); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, "UPDATE responses SET agent_version_id='missing' WHERE public_id='resp-trial'"); err == nil {
		t.Fatal("missing Agent reference accepted")
	}
}
