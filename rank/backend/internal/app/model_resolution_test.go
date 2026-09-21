package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/rank/backend/internal/app"
)

type selectedConnection struct {
	app.ModelConnectionClient
	value contract.ModelConnection
}

func (client *selectedConnection) GetModelConnection(_ context.Context, id string) (contract.ModelConnection, error) {
	if id != client.value.ID {
		return contract.ModelConnection{}, errors.New("missing connection")
	}
	return client.value, nil
}

func TestAgentModelDefaultFreezesAtVersionCreation(t *testing.T) {
	_, store, _ := testService(t, false)
	defer store.Close()
	connection := &selectedConnection{value: contract.ModelConnection{ID: "connection", Status: "verified", Protocol: contract.ProtocolOpenAIChat, DefaultModel: "first"}}
	service := app.NewService(store, app.Options{Connections: connection})
	first, err := service.CreateAgent(t.Context(), app.CreateAgentInput{Name: "Frozen", RunnerType: "mock", ModelConnectionID: "connection"})
	if err != nil {
		t.Fatal(err)
	}
	connection.value.DefaultModel = "second"
	next, err := service.CreateAgentVersion(t.Context(), first.FamilyID, app.CreateAgentInput{RunnerType: "mock", ModelConnectionID: "connection"})
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := store.GetAgentVersion(t.Context(), first.ID)
	if err != nil || frozen.Model != "first" || next.Model != "second" {
		t.Fatalf("mutable version model: %q %q %v", frozen.Model, next.Model, err)
	}
	connection.value.DefaultModel = ""
	_, err = service.CreateAgent(t.Context(), app.CreateAgentInput{Name: "Unresolved", RunnerType: "mock", ModelConnectionID: "connection"})
	var failure *app.Error
	if !errors.As(err, &failure) || failure.Code != "model_required" {
		t.Fatalf("missing default accepted: %v", err)
	}
	explicit, err := service.CreateAgent(t.Context(), app.CreateAgentInput{Name: "Custom", RunnerType: "mock", ModelConnectionID: "connection", Model: " custom "})
	if err != nil || explicit.Model != "custom" {
		t.Fatalf("explicit model rejected: %q %v", explicit.Model, err)
	}
}

func TestUnresolvedStoredAgentCannotAdmitEvaluationOrTrial(t *testing.T) {
	_, store, _ := testService(t, false)
	defer store.Close()
	service := app.NewService(store, app.Options{Execution: struct{ app.ServingExecutionClient }{}})
	base, err := store.GetAgentVersion(t.Context(), "agent-research-demo-v1")
	if err != nil {
		t.Fatal(err)
	}
	base.ID, base.Version, base.Model, base.ModelConnectionID = "agent-research-demo-v2", 2, "由运行时决定", "connection"
	if err := store.CreateAgentVersion(t.Context(), base); err != nil {
		t.Fatal(err)
	}
	experiment := configuredExperiment(t, service)
	_, err = service.UpdateExperiment(t.Context(), experiment.ID, app.ExperimentPatch{AgentVersionID: &base.ID})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.StartRun(t.Context(), experiment.ID, "unresolved")
	var failure *app.Error
	if !errors.As(err, &failure) || failure.Code != "agent_model_unresolved" {
		t.Fatalf("evaluation admitted: %v", err)
	}
	_, _, err = service.CreateTrialResponse(t.Context(), app.TrialResponseInput{AgentVersionID: base.ID, Input: "test"}, "unresolved")
	if !errors.As(err, &failure) || failure.Code != "agent_model_unresolved" {
		t.Fatalf("trial admitted: %v", err)
	}
}
