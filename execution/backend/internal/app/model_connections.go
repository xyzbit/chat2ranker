package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/xyzbit/chat2ranker/execution/backend/contract"
	"github.com/xyzbit/chat2ranker/execution/backend/internal/domain"
	"github.com/xyzbit/chat2ranker/execution/backend/internal/pricing"
)

type CredentialStore interface {
	Put(context.Context, string, string) error
	Get(context.Context, string) (string, error)
	Delete(context.Context, string) error
}

type ConnectionVerifier interface {
	Verify(context.Context, contract.ModelConnection, string) ([]string, error)
}

type HTTPConnectionVerifier struct{ Client *http.Client }

func (v HTTPConnectionVerifier) Verify(ctx context.Context, connection contract.ModelConnection, apiKey string) ([]string, error) {
	client := v.Client
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	models, listErr := listModels(ctx, client, connection, apiKey)
	model := strings.TrimSpace(connection.DefaultModel)
	if model == "" && len(models) > 0 {
		model = models[0]
	}
	if model == "" {
		if listErr != nil {
			return nil, listErr
		}
		return nil, errors.New("连接未返回模型，请填写默认模型")
	}
	endpoint, payload := "/chat/completions", map[string]any{"model": model, "messages": []map[string]string{{"role": "user", "content": "Reply with OK."}}, "max_tokens": 1}
	if connection.Protocol == contract.ProtocolOpenAIResponses {
		endpoint, payload = "/responses", map[string]any{"model": model, "input": "Reply with OK.", "max_output_tokens": 1}
	} else if connection.Protocol == contract.ProtocolAnthropic {
		endpoint, payload = "/v1/messages", map[string]any{"model": model, "messages": []map[string]string{{"role": "user", "content": "Reply with OK."}}, "max_tokens": 1}
	}
	if err := providerRequest(ctx, client, connection.Protocol, http.MethodPost, connection.BaseURL+endpoint, apiKey, payload, nil); err != nil {
		if listErr != nil {
			return nil, fmt.Errorf("同步模型列表失败（%v），验证模型也失败：%w", listErr, err)
		}
		return nil, err
	}
	if listErr != nil {
		models = append([]string{}, connection.Models...)
	}
	return uniqueModels(append(models, model)), nil
}

func uniqueModels(models []string) []string {
	unique := map[string]struct{}{}
	result := make([]string, 0, len(models))
	for _, model := range models {
		if model = strings.TrimSpace(model); model == "" {
			continue
		}
		if _, exists := unique[model]; exists {
			continue
		}
		unique[model] = struct{}{}
		result = append(result, model)
	}
	sort.Strings(result)
	return result
}

func listModels(ctx context.Context, client *http.Client, connection contract.ModelConnection, apiKey string) ([]string, error) {
	var response struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	endpoint := connection.BaseURL + "/models"
	if connection.Protocol == contract.ProtocolAnthropic {
		endpoint = connection.BaseURL + "/v1/models"
	}
	if err := providerRequest(ctx, client, connection.Protocol, http.MethodGet, endpoint, apiKey, nil, &response); err != nil {
		return nil, err
	}
	models := make([]string, 0, len(response.Data))
	for _, item := range response.Data {
		if id := strings.TrimSpace(item.ID); id != "" {
			models = append(models, id)
		}
	}
	sort.Strings(models)
	return models, nil
}

func providerRequest(ctx context.Context, client *http.Client, protocol, method, target, apiKey string, body, result any) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	request, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return err
	}
	if protocol == contract.ProtocolAnthropic {
		request.Header.Set("x-api-key", apiKey)
		request.Header.Set("anthropic-version", "2023-06-01")
	} else {
		request.Header.Set("Authorization", "Bearer "+apiKey)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("连接失败：%w", err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("服务返回 HTTP %d：%s", response.StatusCode, strings.TrimSpace(string(payload)))
	}
	if result != nil && len(payload) > 0 {
		if err := json.Unmarshal(payload, result); err != nil {
			return fmt.Errorf("响应不是有效 JSON：%w", err)
		}
	}
	return nil
}

func (s *Service) ListModelConnections(ctx context.Context) ([]contract.ModelConnection, error) {
	return s.repo.ListModelConnections(ctx)
}

func (s *Service) ListModelCatalog() []contract.ModelProvider { return pricing.Providers() }

func (s *Service) GetModelConnection(ctx context.Context, id string) (contract.ModelConnection, error) {
	value, err := s.repo.GetModelConnection(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		return value, problem(404, "model_connection_not_found", "模型连接不存在")
	}
	return value, err
}

func (s *Service) SaveModelConnection(ctx context.Context, id string, input contract.ModelConnectionInput) (contract.ModelConnection, error) {
	if s.credentials == nil {
		return contract.ModelConnection{}, problem(503, "credential_store_unavailable", "凭据存储未配置")
	}
	input.Name, input.Provider, input.BaseURL, input.DefaultModel = strings.TrimSpace(input.Name), strings.TrimSpace(input.Provider), strings.TrimRight(strings.TrimSpace(input.BaseURL), "/"), strings.TrimSpace(input.DefaultModel)
	if input.Name == "" || input.BaseURL == "" {
		return contract.ModelConnection{}, problem(400, "invalid_model_connection", "名称和 Base URL 必填")
	}
	if input.Protocol != contract.ProtocolOpenAIChat && input.Protocol != contract.ProtocolOpenAIResponses && input.Protocol != contract.ProtocolAnthropic {
		return contract.ModelConnection{}, problem(400, "invalid_model_protocol", "不支持的模型协议")
	}
	parsed, err := url.Parse(input.BaseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return contract.ModelConnection{}, problem(400, "invalid_base_url", "Base URL 必须是 http(s) URL")
	}
	for model, price := range input.Prices {
		if strings.TrimSpace(model) == "" || price.Input < 0 || price.Output < 0 || price.CacheRead < 0 || price.CacheWrite < 0 {
			return contract.ModelConnection{}, problem(400, "invalid_model_price", "模型价格必须是非负数")
		}
	}
	if input.Provider == "" {
		input.Provider = pricing.ProviderForBaseURL(input.BaseURL)
	}
	now := s.now().UTC()
	value := contract.ModelConnection{ID: id, Name: input.Name, Provider: input.Provider, Protocol: input.Protocol, BaseURL: input.BaseURL, DefaultModel: input.DefaultModel, Models: []string{}, Prices: input.Prices, Status: "missing_credential", CreatedAt: now, UpdatedAt: now}
	if id == "" {
		value.ID = newID("model")
	}
	if id != "" {
		value, err = s.GetModelConnection(ctx, id)
		if err != nil {
			return value, err
		}
		value.Name, value.Provider, value.Protocol, value.BaseURL, value.DefaultModel, value.Prices, value.Status, value.StatusMessage, value.UpdatedAt = input.Name, input.Provider, input.Protocol, input.BaseURL, input.DefaultModel, input.Prices, "unverified", "", now
	}
	if input.APIKey != "" {
		if value.CredentialRef == "" {
			value.CredentialRef = "model-connection/" + value.ID
		}
		if err := s.credentials.Put(ctx, value.CredentialRef, input.APIKey); err != nil {
			return value, err
		}
		value.HasCredential = true
	}
	if !value.HasCredential {
		value.Status = "missing_credential"
	}
	return value, s.repo.SaveModelConnection(ctx, value)
}

func (s *Service) VerifyModelConnection(ctx context.Context, id string) (contract.ModelConnection, error) {
	value, err := s.GetModelConnection(ctx, id)
	if err != nil {
		return value, err
	}
	if !value.HasCredential {
		return value, problem(400, "credential_missing", "请先添加 API Key")
	}
	key, err := s.credentials.Get(ctx, value.CredentialRef)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return value, problem(400, "credential_missing", "模型连接没有可用凭据")
		}
		return value, err
	}
	models, verifyErr := s.verifier.Verify(ctx, value, key)
	now := s.now().UTC()
	value.LastVerifiedAt, value.UpdatedAt = &now, now
	if verifyErr != nil {
		value.Status, value.StatusMessage = "failed", verifyErr.Error()
	} else {
		value.Status, value.StatusMessage, value.Models = "verified", "", models
		if value.DefaultModel == "" && len(models) > 0 {
			value.DefaultModel = models[0]
		}
	}
	if err := s.repo.SaveModelConnection(ctx, value); err != nil {
		return value, err
	}
	if verifyErr != nil {
		return value, problem(422, "model_connection_failed", verifyErr.Error())
	}
	return value, nil
}

func (s *Service) DeleteModelConnection(ctx context.Context, id string) error {
	value, err := s.GetModelConnection(ctx, id)
	if err != nil {
		return err
	}
	bindings, err := s.repo.ListSystemModelBindings(ctx)
	if err != nil {
		return err
	}
	for _, binding := range bindings {
		if binding.ConnectionID == id {
			return problem(409, "model_connection_in_use", "该连接正用于系统 "+binding.Role+" 模型，请先切换绑定")
		}
	}
	if err := s.repo.DeleteModelConnection(ctx, id); err != nil {
		return err
	}
	if value.CredentialRef == "" {
		return nil
	}
	return s.credentials.Delete(ctx, value.CredentialRef)
}

func (s *Service) ResolveModelConnection(ctx context.Context, id string) (contract.ModelConnection, string, error) {
	value, err := s.GetModelConnection(ctx, id)
	if err != nil {
		return value, "", err
	}
	if value.Status != "verified" {
		return value, "", problem(400, "model_connection_unverified", "模型连接尚未验证")
	}
	key, err := s.credentials.Get(ctx, value.CredentialRef)
	return value, key, err
}
