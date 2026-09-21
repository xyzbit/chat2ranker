package app_test

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/execution/backend/internal/app"
	"github.com/xyzbit/agentkiln/execution/backend/internal/sqlite"
)

func TestRepeatedJSONRequestPreservesIdentityWithEmptyCollectionsAndNumbers(t *testing.T) {
	store, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := app.NewService(store, &runtimeOutcome{}, app.Options{})
	request := contract.SubmitRequest{IdempotencyKey: "repeat", Spec: contract.Spec{ProtocolVersion: contract.ProtocolVersion, Kind: contract.KindAgent, Harness: "mock", Prompt: "hello", Metadata: map[string]string{}, Tools: []string{}, Expected: map[string]any{"count": 3}}}
	first, err := service.Submit(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		repeated, err := service.Submit(t.Context(), request)
		if err != nil || repeated.ID != first.ID {
			t.Fatalf("identical request conflicted: %v", err)
		}
	}
	request.Spec.Prompt = "different"
	_, err = service.Submit(t.Context(), request)
	var failure *app.Error
	if !errors.As(err, &failure) || failure.Code != "idempotency_conflict" {
		t.Fatalf("changed request accepted: %v", err)
	}
}

func TestIdempotencyComparesRawJSONValuesWithoutRoundingOrReorderingArrays(t *testing.T) {
	store, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := app.NewService(store, &runtimeOutcome{}, app.Options{})
	request := contract.SubmitRequest{IdempotencyKey: "schema", Spec: contract.Spec{ProtocolVersion: contract.ProtocolVersion, Kind: contract.KindAgent, Harness: "mock", Prompt: "hello", StructuredOutput: &contract.StructuredOutput{Name: "test", Schema: json.RawMessage(`{"const":{"big":9007199254740993,"value":1e3,"items":[1,2]}}`)}}}
	first, err := service.Submit(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		schema   string
		accepted bool
	}{
		{`{ "const" : { "items": [1.0,2], "value": 1000.00, "big": 9007199254740993 } }`, true},
		{`{"const":{"big":9.007199254740993e15,"value":1e+3,"items":[1,2]}}`, true},
		{`{"const":{"big":9007199254740992,"value":1000,"items":[1,2]}}`, false},
		{`{"const":{"big":9007199254740993,"value":1000,"items":[2,1]}}`, false},
		{`{"const":{"big":"9007199254740993","value":1000,"items":[1,2]}}`, false},
	} {
		request.Spec.StructuredOutput.Schema = json.RawMessage(test.schema)
		result, err := service.Submit(t.Context(), request)
		if test.accepted {
			if err != nil || result.ID != first.ID {
				t.Fatalf("equivalent JSON conflicted: %v", err)
			}
		} else {
			var failure *app.Error
			if !errors.As(err, &failure) || failure.Code != "idempotency_conflict" {
				t.Fatalf("changed JSON accepted: %v", err)
			}
		}
	}
}
