package app_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/execution/backend/internal/app"
	"github.com/xyzbit/agentkiln/execution/backend/internal/secret"
	"github.com/xyzbit/agentkiln/execution/backend/internal/sqlite"
)

func localDiscoveryService(t *testing.T) (*app.Service, string, string) {
	t.Helper()
	root := t.TempDir()
	codex, dsh := filepath.Join(root, "codex"), filepath.Join(root, "dsh")
	for _, dir := range []string{codex, dsh} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("CODEX_HOME", codex)
	t.Setenv("DSH_HOME", dsh)
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_BASE_URL", "")
	store, err := sqlite.Open(context.Background(), filepath.Join(root, "execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return app.NewService(store, nil, app.Options{Credentials: secret.NewFileStore(filepath.Join(root, "credentials"))}), codex, dsh
}

func writeLocalFixture(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}

const codexLocalFixture = `model = "test-model"
model_provider = "gateway"
[model_providers.gateway]
base_url = "https://provider.example/v1/"
env_key = "RANK_TEST_LOCAL_KEY"
wire_api = "responses"
`

func TestLocalDiscoveryImportIsExplicitAndIdempotent(t *testing.T) {
	s, codex, _ := localDiscoveryService(t)
	t.Setenv("RANK_TEST_LOCAL_KEY", "private-local-key")
	writeLocalFixture(t, filepath.Join(codex, "config.toml"), codexLocalFixture)
	ctx := context.Background()
	discovery, err := s.DiscoverLocalModels(ctx)
	if err != nil || len(discovery.Candidates) != 1 || len(discovery.Issues) != 0 {
		t.Fatalf("unexpected discovery: %#v %v", discovery, err)
	}
	payload, _ := json.Marshal(discovery)
	if strings.Contains(string(payload), "private-local-key") || strings.Contains(string(payload), "RANK_TEST_LOCAL_KEY") {
		t.Fatal("discovery exposed credentials")
	}
	connections, _ := s.ListModelConnections(ctx)
	if len(connections) != 0 {
		t.Fatal("discovery imported a connection")
	}
	id := discovery.Candidates[0].ID
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.ImportLocalModel(ctx, id, ""); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	connections, _ = s.ListModelConnections(ctx)
	if len(connections) != 1 || !connections[0].HasCredential || connections[0].Status != "unverified" {
		t.Fatalf("import must save one unverified connection: %#v", connections)
	}
	bindings, _ := s.ListSystemModelBindings(ctx)
	if len(bindings) != 0 {
		t.Fatal("import changed system bindings")
	}
	discovery, _ = s.DiscoverLocalModels(ctx)
	if discovery.Candidates[0].ConnectionID != connections[0].ID {
		t.Fatal("rediscovery did not mark existing connection")
	}
	writeLocalFixture(t, filepath.Join(codex, "config.toml"), strings.ReplaceAll(codexLocalFixture, "test-model", "changed-model"))
	if _, err := s.ImportLocalModel(ctx, id, ""); err == nil {
		t.Fatal("stale candidate imported a different model")
	}
}

func TestLocalDiscoverySeparatesSourceFailureAndDSHCredentials(t *testing.T) {
	s, codex, dsh := localDiscoveryService(t)
	writeLocalFixture(t, filepath.Join(codex, "config.toml"), "model = broken-secret-value")
	writeLocalFixture(t, filepath.Join(dsh, "settings.yaml"), `llm-pi-ai:
  providers:
    local:
      baseURL: https://provider.example/v1
      api: openai-completions
      apiKeyEnv: RANK_TEST_DSH_KEY
      models:
        - id: model-a
        - id: model-b
agent-default-model:
  provider: local
  model: model-b
`)
	writeLocalFixture(t, filepath.Join(dsh, ".env"), "RANK_TEST_DSH_KEY=dotenv-secret\n")
	writeLocalFixture(t, filepath.Join(dsh, ".credentials.yaml"), "RANK_TEST_DSH_KEY: managed-secret\n")
	t.Setenv("RANK_TEST_DSH_KEY", "environment-secret")
	ctx := context.Background()
	discovery, err := s.DiscoverLocalModels(ctx)
	if err != nil || len(discovery.Candidates) != 1 || discovery.Candidates[0].Model != "model-b" || len(discovery.Issues) != 1 {
		t.Fatalf("unexpected partial discovery: %#v %v", discovery, err)
	}
	payload, _ := json.Marshal(discovery)
	if strings.Contains(string(payload), "secret") {
		t.Fatal("source failure leaked a value")
	}
	manual, err := s.SaveModelConnection(ctx, "", contract.ModelConnectionInput{Name: "Manual", Protocol: contract.ProtocolOpenAIChat, BaseURL: "https://provider.example/v1", DefaultModel: discovery.Candidates[0].Model, APIKey: "environment-secret"})
	if err != nil {
		t.Fatal(err)
	}
	imported, err := s.ImportLocalModel(ctx, discovery.Candidates[0].ID, "")
	if err != nil || imported.ID != manual.ID {
		t.Fatal("import did not reuse manually configured connection or environment precedence")
	}
}

func TestLocalDiscoveryRejectsUnsupportedCredentialsAndSecretURLs(t *testing.T) {
	for _, fixture := range []struct{ name, config, auth string }{
		{"subscription", "model = 'model-a'", `{"auth_mode":"chatgpt","OPENAI_API_KEY":"stale-key","tokens":{"access_token":"private-token"}}`},
		{"profile", "model = 'model-a'\nprofile = 'work'", ""},
		{"query", strings.ReplaceAll(codexLocalFixture, "https://provider.example/v1/", "https://provider.example/v1?key=secret"), ""},
		{"header", codexLocalFixture + "http_headers = { secret = 'value' }\n", ""},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			s, codex, _ := localDiscoveryService(t)
			t.Setenv("RANK_TEST_LOCAL_KEY", "key")
			writeLocalFixture(t, filepath.Join(codex, "config.toml"), fixture.config)
			if fixture.auth != "" {
				writeLocalFixture(t, filepath.Join(codex, "auth.json"), fixture.auth)
			}
			discovery, err := s.DiscoverLocalModels(context.Background())
			if err != nil || len(discovery.Candidates) != 0 || len(discovery.Issues) != 1 {
				t.Fatalf("unsupported source was not isolated: %#v %v", discovery, err)
			}
		})
	}
}

func TestCodexAPIKeyFileAndEndpointOverride(t *testing.T) {
	s, codex, _ := localDiscoveryService(t)
	writeLocalFixture(t, filepath.Join(codex, "config.toml"), "model = 'model-a'\nopenai_base_url = 'https://gateway.example/v1'")
	writeLocalFixture(t, filepath.Join(codex, "auth.json"), `{"auth_mode":"apikey","OPENAI_API_KEY":"file-secret"}`)
	discovery, err := s.DiscoverLocalModels(context.Background())
	if err != nil || len(discovery.Candidates) != 1 || discovery.Candidates[0].BaseURL != "https://gateway.example/v1" {
		t.Fatalf("endpoint override lost: %#v %v", discovery, err)
	}
}

func TestDSHDiscoveryResolvesSavedDefaults(t *testing.T) {
	s, _, dsh := localDiscoveryService(t)
	t.Setenv("DEEPSEEK_API_KEY", "deepseek-fixture-key")
	t.Setenv("DEEPSEEK_BASE_URL", "https://deepseek.example/v1")
	t.Setenv("RANK_TEST_OPENAI_KEY", "openai-fixture-key")
	writeLocalFixture(t, filepath.Join(dsh, "settings.yaml"), `llm-pi-ai:
  providers:
    openai:
      baseURL: https://gateway.example/v1
      apiKeyEnv: RANK_TEST_OPENAI_KEY
      models:
        - id: model-a
llm-deepseek:
  models:
    - id: old-model
agent-default-model:
  provider: deepseek-official
  model: selected-model
`)
	discovery, err := s.DiscoverLocalModels(context.Background())
	if err != nil || len(discovery.Candidates) != 2 {
		t.Fatalf("unexpected DSH defaults: %#v %v", discovery, err)
	}
	for _, candidate := range discovery.Candidates {
		if candidate.Name == "DSH · openai" && candidate.Protocol != contract.ProtocolOpenAIResponses {
			t.Fatal("wrong inherited OpenAI protocol")
		}
		if candidate.Name == "DSH · deepseek-official" && (candidate.Model != "selected-model" || candidate.BaseURL != "https://deepseek.example/v1") {
			t.Fatal("saved DSH model or endpoint ignored")
		}
	}
}

func TestDSHVersionedCredentialRefsAndCustomModel(t *testing.T) {
	s, _, dsh := localDiscoveryService(t)
	t.Setenv("RANK_TEST_VERSIONED_KEY", "")
	if err := os.Unsetenv("RANK_TEST_VERSIONED_KEY"); err != nil {
		t.Fatal(err)
	}
	writeLocalFixture(t, filepath.Join(dsh, "settings.yaml"), `llm-pi-ai:
  providers:
    openai:
      baseURL: https://gateway.example/v1
      apiKeyEnv: RANK_TEST_VERSIONED_KEY
      models:
        - id: configured-model
`)
	writeLocalFixture(t, filepath.Join(dsh, ".credentials.yaml"), `version: 1
refs:
  RANK_TEST_VERSIONED_KEY: versioned-secret
records:
  owner/id:
    kind: grant
    payload:
      secret: never-import-a-grant
`)
	ctx := context.Background()
	discovery, err := s.DiscoverLocalModels(ctx)
	if err != nil || len(discovery.Candidates) != 1 {
		t.Fatalf("versioned refs not discovered: %#v %v", discovery, err)
	}
	defaultConnection, err := s.ImportLocalModel(ctx, discovery.Candidates[0].ID, "")
	if err != nil {
		t.Fatal(err)
	}
	custom, err := s.ImportLocalModel(ctx, discovery.Candidates[0].ID, " custom-model ")
	if err != nil || custom.DefaultModel != "custom-model" || custom.ID == defaultConnection.ID {
		t.Fatalf("custom model did not get an independent connection: %#v %v", custom, err)
	}
	again, err := s.ImportLocalModel(ctx, discovery.Candidates[0].ID, "custom-model")
	if err != nil || again.ID != custom.ID {
		t.Fatal("custom model import was duplicated")
	}
	original, _ := s.GetModelConnection(ctx, defaultConnection.ID)
	if original.DefaultModel != "configured-model" {
		t.Fatal("custom model changed an existing connection")
	}
	payload, _ := json.Marshal(discovery)
	if strings.Contains(string(payload), "secret") || strings.Contains(string(payload), "grant") {
		t.Fatal("discovery leaked credentials")
	}
	writeLocalFixture(t, filepath.Join(dsh, ".credentials.yaml"), "version: 2\nrefs: {}\n")
	discovery, err = s.DiscoverLocalModels(ctx)
	if err != nil || len(discovery.Candidates) != 0 || len(discovery.Issues) != 1 {
		t.Fatal("unknown credential version was accepted")
	}
}
