package app_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/xyzbit/agentkiln/rank/backend/internal/app"
)

func TestConfigurationActionsRejectSecretsAndInvalidReferences(t *testing.T) {
	service, store, _ := testService(t, false)
	defer store.Close()
	ctx := context.Background()
	experiment, err := service.CreateExperiment(ctx, "Configuration")
	if err != nil {
		t.Fatal(err)
	}
	for index, payload := range []string{
		`{"section":"api_keys","apiKey":"must-not-be-stored"}`,
		`{"section":"webhooks","signingSecret":"must-not-be-stored"}`,
		`{"section":"arbitrary"}`,
		`{"section":"release_policy"}`,
		`{"section":"webhooks","deploymentId":"missing"}`,
		`{"section":"preferences","connectionId":"irrelevant"}`,
	} {
		_, err := service.ApplyControlCommand(ctx, app.ControlCommandInput{ExperimentID: experiment.ID, ControlSessionID: experiment.ControlSessionID, IdempotencyKey: fmt.Sprint(index), Type: app.ControlOpenConfiguration, Payload: json.RawMessage(payload)})
		if err == nil {
			t.Fatalf("accepted invalid configuration: %s", payload)
		}
	}
	view, err := service.GetExperiment(ctx, experiment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.ControlEvents) != 0 {
		t.Fatal("rejected input persisted in conversation")
	}
	input := app.ControlCommandInput{ExperimentID: experiment.ID, ControlSessionID: experiment.ControlSessionID, IdempotencyKey: "open-settings", Type: app.ControlOpenConfiguration, Payload: json.RawMessage(`{"section":"preferences"}`)}
	first, err := service.ApplyControlCommand(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.ApplyControlCommand(ctx, input)
	if err != nil || first.ID != second.ID {
		t.Fatalf("action replay: %v", err)
	}
	view, err = service.GetExperiment(ctx, experiment.ID)
	if err != nil || len(view.ControlEvents) != 1 || view.ControlEvents[0].Type != "a2ui/open_configuration" {
		t.Fatalf("missing durable action: %v", err)
	}
}

func TestConversationDatasetReplacementPreservesFrozenBase(t *testing.T) {
	service, store, _ := testService(t, false)
	defer store.Close()
	ctx := context.Background()
	experiment, err := service.CreateExperiment(ctx, "Edit dataset")
	if err != nil {
		t.Fatal(err)
	}
	base, err := store.GetDatasetVersion(ctx, "dataset-web-research-v3")
	if err != nil {
		t.Fatal(err)
	}
	command := func(key, payload string) error {
		_, err := service.ApplyControlCommand(ctx, app.ControlCommandInput{ExperimentID: experiment.ID, ControlSessionID: experiment.ControlSessionID, IdempotencyKey: key, Type: app.ControlCreateDatasetVersion, Payload: json.RawMessage(payload)})
		return err
	}
	if err := command("empty", `{"baseDatasetVersionId":"dataset-web-research-v3","cases":[]}`); err == nil {
		t.Fatal("empty dataset accepted")
	}
	if err := command("replace", `{"baseDatasetVersionId":"dataset-web-research-v3","description":"revised","cases":[{"id":"replacement","input":"updated"}]}`); err != nil {
		t.Fatal(err)
	}
	view, err := service.GetExperiment(ctx, experiment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Dataset.CaseCount != 1 || view.Dataset.Cases[0].ID != "replacement" || view.Dataset.Description != "revised" {
		t.Fatalf("replacement not applied: %#v", view.Dataset)
	}
	unchanged, err := store.GetDatasetVersion(ctx, base.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(unchanged.Cases) != len(base.Cases) || unchanged.Description != base.Description {
		t.Fatal("base version changed")
	}
}
