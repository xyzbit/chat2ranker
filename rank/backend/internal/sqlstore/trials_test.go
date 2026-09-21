package sqlstore_test

import (
	"reflect"
	"testing"

	"github.com/xyzbit/agentkiln/rank/backend/internal/domain"
	"github.com/xyzbit/agentkiln/rank/backend/internal/sqlstore"
)

func TestTrialHistoryFiltersAcrossDatabases(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			var store *sqlstore.Store
			if backend == "postgres" {
				store, _ = postgresPair(t, true)
			} else {
				store, _ = openStore(t)
			}
			_, agent, _ := fixture(t, store)
			ctx, now := t.Context(), agent.CreatedAt
			endpoint := domain.Deployment{ID: "endpoint", TenantID: "local", AgentFamilyID: agent.FamilyID, Name: "endpoint", CreatedAt: now}
			if err := store.CreateDeployment(ctx, endpoint, nil); err != nil {
				t.Fatal(err)
			}
			for _, seed := range []struct {
				id, tenant string
				endpoint   domain.DeploymentID
				trial      bool
			}{
				{"direct", "local", "", true},
				{"endpoint-trial", "local", endpoint.ID, true},
				{"ordinary", "local", endpoint.ID, false},
				{"other-tenant", "other", "", true},
			} {
				conversation := domain.Conversation{ID: domain.ConversationID(seed.id), TenantID: seed.tenant, DeploymentID: seed.endpoint, AgentVersionID: agent.ID, Trial: seed.trial, CreatedAt: now, UpdatedAt: now}
				response := domain.ServingResponse{ID: domain.ResponseID("response-" + seed.id), TenantID: seed.tenant, DeploymentID: seed.endpoint, ConversationID: conversation.ID, AgentVersionID: agent.ID, Input: seed.id, Status: domain.ResponseCompleted, IdempotencyKey: seed.id, CreatedAt: now}
				event := domain.ResponseEvent{ResponseID: response.ID, Type: "response.completed", Data: []byte(`{}`), CreatedAt: now}
				if err := store.AdmitResponse(ctx, response, &conversation, event, 60, 4); err != nil {
					t.Fatal(err)
				}
			}
			for _, test := range []struct {
				name   string
				filter domain.TrialConversationFilter
				want   []string
			}{
				{"family", domain.TrialConversationFilter{AgentFamilyID: agent.FamilyID}, []string{"endpoint-trial", "direct"}},
				{"direct-version", domain.TrialConversationFilter{AgentVersionID: agent.ID, DirectOnly: true}, []string{"direct"}},
				{"cursor", domain.TrialConversationFilter{Before: "endpoint-trial"}, []string{"direct"}},
				{"different-version", domain.TrialConversationFilter{AgentVersionID: "missing"}, []string{}},
			} {
				t.Run(test.name, func(t *testing.T) {
					rows, err := store.ListTrialConversations(ctx, "local", test.filter)
					if err != nil {
						t.Fatal(err)
					}
					got := []string{}
					for _, row := range rows {
						got = append(got, row.Title)
					}
					if !reflect.DeepEqual(got, test.want) {
						t.Fatalf("got %v; want %v", got, test.want)
					}
				})
			}
		})
	}
}
