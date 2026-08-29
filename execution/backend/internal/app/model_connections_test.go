package app_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/xyzbit/chat2ranker/execution/backend/contract"
	"github.com/xyzbit/chat2ranker/execution/backend/internal/app"
	"github.com/xyzbit/chat2ranker/execution/backend/internal/secret"
	"github.com/xyzbit/chat2ranker/execution/backend/internal/sqlite"
)

func TestModelConnectionStoresCredentialSeparatelyAndVerifies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer temporary-secret" {
			http.Error(response, "unauthorized", 401)
			return
		}
		switch request.URL.Path {
		case "/v1/models":
			json.NewEncoder(response).Encode(map[string]any{"data": []map[string]string{{"id": "model-b"}, {"id": "model-a"}}})
		case "/v1/chat/completions":
			json.NewEncoder(response).Encode(map[string]any{"choices": []any{}})
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	ctx, root := context.Background(), t.TempDir()
	store, err := sqlite.Open(ctx, filepath.Join(root, "execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := app.NewService(store, nil, app.Options{Credentials: secret.NewFileStore(filepath.Join(root, "credentials"))})
	created, err := service.SaveModelConnection(ctx, "", contract.ModelConnectionInput{Name: "Local", Protocol: contract.ProtocolOpenAIChat, BaseURL: server.URL + "/v1", APIKey: "temporary-secret", DefaultModel: "model-a"})
	if err != nil {
		t.Fatal(err)
	}
	verified, err := service.VerifyModelConnection(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if verified.Status != "verified" || len(verified.Models) != 2 || verified.APIKey != "" {
		t.Fatalf("unexpected connection: %#v", verified)
	}
	loaded, err := store.GetModelConnection(ctx, created.ID)
	if err != nil || loaded.APIKey != "" || !loaded.HasCredential {
		t.Fatalf("secret leaked into repository value: %#v %v", loaded, err)
	}
}

func TestSystemModelsBindVerifiedConnectionAndProtectIt(t *testing.T) {
	ctx, root := context.Background(), t.TempDir()
	store, err := sqlite.Open(ctx, filepath.Join(root, "execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	credentials := secret.NewFileStore(filepath.Join(root, "credentials"))
	service := app.NewService(store, nil, app.Options{Credentials: credentials, Verifier: verifierFunc(func(_ context.Context, _ contract.ModelConnection, _ string) ([]string, error) {
		return []string{"model-a"}, nil
	})})
	connection, err := service.SaveModelConnection(ctx, "", contract.ModelConnectionInput{Name: "Shared", Provider: "custom", Protocol: contract.ProtocolOpenAIChat, BaseURL: "https://example.test/v1", APIKey: "secret", DefaultModel: "model-a"})
	if err != nil {
		t.Fatal(err)
	}
	connection, err = service.VerifyModelConnection(ctx, connection.ID)
	if err != nil {
		t.Fatal(err)
	}
	control, err := service.SaveSystemModelBinding(ctx, contract.SystemModelControl, contract.SystemModelBindingInput{ConnectionID: connection.ID})
	if err != nil || control.Model != "model-a" {
		t.Fatalf("unexpected control binding: %#v %v", control, err)
	}
	if _, err = service.SaveSystemModelBinding(ctx, contract.SystemModelJudge, contract.SystemModelBindingInput{ConnectionID: connection.ID, Model: "model-a"}); err != nil {
		t.Fatal(err)
	}
	runtime, err := service.ResolveSystemModel(ctx, contract.SystemModelControl)
	if err != nil || runtime.Credential != "secret" {
		t.Fatalf("unexpected runtime: %#v %v", runtime, err)
	}
	if err = service.DeleteModelConnection(ctx, connection.ID); err == nil {
		t.Fatal("bound connection must not be deleted")
	}
}

type verifierFunc func(context.Context, contract.ModelConnection, string) ([]string, error)

func (fn verifierFunc) Verify(ctx context.Context, connection contract.ModelConnection, key string) ([]string, error) {
	return fn(ctx, connection, key)
}

func TestModelConnectionCanSaveCatalogMetadataBeforeCredential(t *testing.T) {
	ctx, root := context.Background(), t.TempDir()
	store, err := sqlite.Open(ctx, filepath.Join(root, "execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := app.NewService(store, nil, app.Options{Credentials: secret.NewFileStore(filepath.Join(root, "credentials"))})
	price := contract.ModelPrice{Input: 1, Output: 2, CacheRead: .5}
	created, err := service.SaveModelConnection(ctx, "", contract.ModelConnectionInput{
		Name: "Team Gateway", Provider: "custom", Protocol: contract.ProtocolOpenAIChat,
		BaseURL: "https://example.invalid/v1", DefaultModel: "team-model", Prices: map[string]contract.ModelPrice{"team-model": price},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != "missing_credential" || created.HasCredential || created.Prices["team-model"] != price {
		t.Fatalf("unexpected metadata-only connection: %#v", created)
	}
	if _, err := service.VerifyModelConnection(ctx, created.ID); err == nil {
		t.Fatal("verification without a credential must fail")
	}
	providers := service.ListModelCatalog()
	if len(providers) == 0 || providers[0].ID != "deepseek" || len(providers[0].Models) == 0 {
		t.Fatalf("built-in catalog missing: %#v", providers)
	}
}

func TestAnthropicConnectionUsesNativeHeadersAndEndpoints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("x-api-key") != "claude-secret" || request.Header.Get("anthropic-version") != "2023-06-01" || request.Header.Get("Authorization") != "" {
			http.Error(response, "invalid headers", http.StatusUnauthorized)
			return
		}
		switch request.URL.Path {
		case "/v1/models":
			json.NewEncoder(response).Encode(map[string]any{"data": []map[string]string{{"id": "claude-sonnet-5"}}})
		case "/v1/messages":
			json.NewEncoder(response).Encode(map[string]any{"content": []any{}})
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	models, err := (app.HTTPConnectionVerifier{}).Verify(context.Background(), contract.ModelConnection{Protocol: contract.ProtocolAnthropic, BaseURL: server.URL, DefaultModel: "claude-sonnet-5"}, "claude-secret")
	if err != nil || len(models) != 1 || models[0] != "claude-sonnet-5" {
		t.Fatalf("unexpected Anthropic verification: %#v %v", models, err)
	}
}

func TestConnectionVerifiesManualModelWhenModelsEndpointIsUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/models":
			http.Error(response, "not supported", http.StatusNotFound)
		case "/v1/chat/completions":
			json.NewEncoder(response).Encode(map[string]any{"choices": []any{}})
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	models, err := (app.HTTPConnectionVerifier{}).Verify(context.Background(), contract.ModelConnection{Protocol: contract.ProtocolOpenAIChat, BaseURL: server.URL + "/v1", DefaultModel: "manual-new", Models: []string{"last-known"}}, "secret")
	if err != nil || len(models) != 2 || models[0] != "last-known" || models[1] != "manual-new" {
		t.Fatalf("manual model fallback failed: %#v %v", models, err)
	}
}
