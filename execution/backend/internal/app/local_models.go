package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/joho/godotenv"
	"github.com/pelletier/go-toml/v2"
	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"gopkg.in/yaml.v3"
)

type localModel struct {
	Source string
	Input  contract.ModelConnectionInput
}

// readLocalConfig accepts only bounded regular files; parser errors never reach the UI.
func readLocalConfig(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, fmt.Errorf("unsupported configuration file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(io.LimitReader(file, 1<<20))
}

func localHome(env, directory string) string {
	if value := os.Getenv(env); value != "" {
		return value
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, directory)
}

func discoverCodex() ([]localModel, error) {
	home := localHome("CODEX_HOME", ".codex")
	data, err := readLocalConfig(filepath.Join(home, "config.toml"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var config struct {
		Model         string `toml:"model"`
		Provider      string `toml:"model_provider"`
		Profile       string `toml:"profile"`
		OpenAIBaseURL string `toml:"openai_base_url"`
		Providers     map[string]struct {
			Name       string            `toml:"name"`
			BaseURL    string            `toml:"base_url"`
			EnvKey     string            `toml:"env_key"`
			Token      string            `toml:"experimental_bearer_token"`
			WireAPI    string            `toml:"wire_api"`
			OpenAIAuth bool              `toml:"requires_openai_auth"`
			Headers    map[string]string `toml:"http_headers"`
			EnvHeaders map[string]string `toml:"env_http_headers"`
			Query      map[string]string `toml:"query_params"`
			Auth       map[string]any    `toml:"auth"`
		} `toml:"model_providers"`
	}
	if err := toml.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	// Profile files and CLI overrides can select a different provider; do not guess.
	if config.Profile != "" {
		return nil, fmt.Errorf("profile configuration requires manual setup")
	}
	provider := config.Provider
	if provider == "" {
		provider = "openai"
	}
	entry := config.Providers[provider]
	if len(entry.Headers)+len(entry.EnvHeaders)+len(entry.Query)+len(entry.Auth) > 0 {
		return nil, fmt.Errorf("custom authentication or headers require manual setup")
	}
	if entry.WireAPI != "" && entry.WireAPI != "responses" {
		return nil, fmt.Errorf("unsupported protocol")
	}
	baseURL := entry.BaseURL
	if baseURL == "" && provider == "openai" {
		baseURL = config.OpenAIBaseURL
		if baseURL == "" {
			baseURL = os.Getenv("OPENAI_BASE_URL")
		}
		if baseURL == "" {
			baseURL = "https://api.openai.com/v1"
		}
	}
	key := entry.Token
	if entry.EnvKey != "" {
		key = os.Getenv(entry.EnvKey)
	}
	if provider == "openai" || entry.OpenAIAuth {
		key = os.Getenv("OPENAI_API_KEY")
		if key == "" {
			var auth struct {
				APIKey string `json:"OPENAI_API_KEY"`
				Mode   string `json:"auth_mode"`
			}
			data, err := readLocalConfig(filepath.Join(home, "auth.json"))
			if err == nil {
				err = json.Unmarshal(data, &auth)
			}
			if err != nil && !os.IsNotExist(err) {
				return nil, err
			}
			key = auth.APIKey
			if auth.Mode != "" && auth.Mode != "apikey" {
				key = ""
			}
		}
	}
	if baseURL == "" || config.Model == "" || strings.TrimSpace(key) == "" {
		return nil, fmt.Errorf("missing reusable API configuration")
	}
	return []localModel{{Source: "Codex", Input: contract.ModelConnectionInput{Name: "Codex · " + provider, BaseURL: strings.TrimRight(baseURL, "/"), Protocol: contract.ProtocolOpenAIResponses, DefaultModel: config.Model, APIKey: key}}}, nil
}

type dshLocalProvider struct {
	DisplayName string            `yaml:"displayName"`
	BaseURL     string            `yaml:"baseURL"`
	API         string            `yaml:"api"`
	KeyRef      string            `yaml:"apiKeyEnv"`
	Headers     map[string]string `yaml:"headers"`
	Models      []struct {
		ID string `yaml:"id"`
	} `yaml:"models"`
}

func discoverDSH() ([]localModel, error) {
	home := localHome("DSH_HOME", ".dsh")
	data, err := readLocalConfig(filepath.Join(home, "settings.yaml"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var config struct {
		LLM struct {
			Providers map[string]dshLocalProvider `yaml:"providers"`
		} `yaml:"llm-pi-ai"`
		DeepSeek *dshLocalProvider `yaml:"llm-deepseek"`
		Selected struct {
			Provider string `yaml:"provider"`
			Model    string `yaml:"model"`
		} `yaml:"agent-default-model"`
	}
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	credentials := map[string]string{}
	if data, err := readLocalConfig(filepath.Join(home, ".credentials.yaml")); err == nil {
		var document struct {
			Version *int              `yaml:"version"`
			Refs    map[string]string `yaml:"refs"`
		}
		if err := yaml.Unmarshal(data, &document); err != nil {
			return nil, err
		}
		if document.Version != nil {
			if *document.Version != 1 {
				return nil, fmt.Errorf("unsupported DSH credential version")
			}
			credentials = document.Refs
		} else if err := yaml.Unmarshal(data, &credentials); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	env := map[string]string{}
	if data, err := readLocalConfig(filepath.Join(home, ".env")); err == nil {
		env, err = godotenv.Unmarshal(string(data))
		if err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	result := []localModel{}
	if config.LLM.Providers == nil {
		config.LLM.Providers = map[string]dshLocalProvider{}
	}
	if config.DeepSeek != nil {
		provider := *config.DeepSeek
		provider.API = "openai-completions"
		if provider.KeyRef == "" {
			provider.KeyRef = "DEEPSEEK_API_KEY"
		}
		if provider.BaseURL == "" {
			provider.BaseURL = os.Getenv("DEEPSEEK_BASE_URL")
			if provider.BaseURL == "" {
				provider.BaseURL = env["DEEPSEEK_BASE_URL"]
			}
			if provider.BaseURL == "" {
				provider.BaseURL = "https://api.deepseek.com"
			}
		}
		config.LLM.Providers["deepseek-official"] = provider
	}
	for id, provider := range config.LLM.Providers {
		if provider.API == "" {
			// These are DSH's pi-ai protocol defaults, not Rank's provider templates.
			provider.API = map[string]string{"openai": "openai-responses", "anthropic": "anthropic-messages", "minimax": "anthropic-messages", "minimax-cn": "anthropic-messages", "deepseek": "openai-completions"}[id]
		}
		protocol := map[string]string{"openai-completions": contract.ProtocolOpenAIChat, "openai-responses": contract.ProtocolOpenAIResponses, "anthropic-messages": contract.ProtocolAnthropic}[provider.API]
		key := env[provider.KeyRef]
		if value, ok := credentials[provider.KeyRef]; ok {
			key = value
		}
		if value, ok := os.LookupEnv(provider.KeyRef); ok {
			key = value
		}
		if protocol == "" || provider.BaseURL == "" || key == "" || len(provider.Headers) > 0 {
			continue
		}
		name := provider.DisplayName
		if name == "" {
			name = id
		}
		model := config.Selected.Model
		if config.Selected.Provider != id {
			model = ""
		}
		if model == "" && len(provider.Models) > 0 {
			model = provider.Models[0].ID
		}
		if model == "" {
			continue
		}
		result = append(result, localModel{Source: "DSH", Input: contract.ModelConnectionInput{Name: "DSH · " + name, Protocol: protocol, BaseURL: strings.TrimRight(provider.BaseURL, "/"), DefaultModel: model, APIKey: key}})
	}
	if len(config.LLM.Providers) > 0 && len(result) == 0 {
		return nil, fmt.Errorf("missing reusable API configuration")
	}
	return result, nil
}

func scanLocalModels() ([]localModel, []contract.LocalModelIssue) {
	models := []localModel{}
	issues := []contract.LocalModelIssue{}
	for _, source := range []struct {
		name string
		scan func() ([]localModel, error)
	}{{"Codex", discoverCodex}, {"DSH", discoverDSH}} {
		found, err := source.scan()
		if err != nil {
			issues = append(issues, contract.LocalModelIssue{Source: source.name, Message: "未找到可复用的 API Key、端点和模型配置，可手动添加连接。"})
			continue
		}
		models = append(models, found...)
	}
	valid := []localModel{}
	seen := map[string]bool{}
	for _, model := range models {
		endpoint, err := url.Parse(model.Input.BaseURL)
		if err != nil || (endpoint.Scheme != "https" && endpoint.Scheme != "http") || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
			issues = append(issues, contract.LocalModelIssue{Source: model.Source, Message: "端点不能直接复用，请手动添加连接。"})
			continue
		}
		id := localModelID(model)
		if !seen[id] {
			valid = append(valid, model)
			seen[id] = true
		}
	}
	sort.Slice(valid, func(i, j int) bool { return localModelID(valid[i]) < localModelID(valid[j]) })
	return valid, issues
}

func localModelID(model localModel) string {
	data, _ := json.Marshal([]string{model.Source, model.Input.Name, model.Input.Protocol, model.Input.BaseURL, model.Input.DefaultModel})
	return fmt.Sprintf("local-%x", sha256.Sum256(data))
}

func (s *Service) matchingLocalConnection(ctx context.Context, model localModel, connections []contract.ModelConnection) string {
	if s.credentials == nil {
		return ""
	}
	for _, c := range connections {
		if c.Protocol != model.Input.Protocol || c.BaseURL != model.Input.BaseURL || c.DefaultModel != model.Input.DefaultModel {
			continue
		}
		key, err := s.credentials.Get(ctx, c.CredentialRef)
		if err == nil && key == model.Input.APIKey {
			return c.ID
		}
	}
	return ""
}

// DiscoverLocalModels reads optional local sources without writing connections or calling providers.
func (s *Service) DiscoverLocalModels(ctx context.Context) (contract.LocalModelDiscovery, error) {
	models, issues := scanLocalModels()
	result := contract.LocalModelDiscovery{Candidates: []contract.LocalModelCandidate{}, Issues: issues}
	connections, err := s.repo.ListModelConnections(ctx)
	if err != nil {
		return result, err
	}
	for _, model := range models {
		result.Candidates = append(result.Candidates, contract.LocalModelCandidate{ID: localModelID(model), Source: model.Source, Name: model.Input.Name, Protocol: model.Input.Protocol, BaseURL: model.Input.BaseURL, Model: model.Input.DefaultModel, ConnectionID: s.matchingLocalConnection(ctx, model, connections)})
	}
	return result, nil
}

// ImportLocalModel re-reads a server-selected source and reuses an identical stored connection.
func (s *Service) ImportLocalModel(ctx context.Context, id, selectedModel string) (contract.ModelConnection, error) {
	s.discoveryMu.Lock()
	defer s.discoveryMu.Unlock()
	models, _ := scanLocalModels()
	for _, model := range models {
		if localModelID(model) != id {
			continue
		}
		if selectedModel = strings.TrimSpace(selectedModel); selectedModel != "" {
			model.Input.DefaultModel = selectedModel
		}
		connections, err := s.repo.ListModelConnections(ctx)
		if err != nil {
			return contract.ModelConnection{}, err
		}
		if existing := s.matchingLocalConnection(ctx, model, connections); existing != "" {
			return s.GetModelConnection(ctx, existing)
		}
		return s.SaveModelConnection(ctx, "", model.Input)
	}
	return contract.ModelConnection{}, problem(404, "local_model_unavailable", "本地配置已变化或不可用，请重新发现或手动添加连接")
}
