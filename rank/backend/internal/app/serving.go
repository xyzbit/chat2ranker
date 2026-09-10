package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/xyzbit/chat2ranker/execution/backend/contract"
	"github.com/xyzbit/chat2ranker/rank/backend/internal/domain"
)

var responseFormatNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type CreateAPICredentialInput struct {
	TenantID      string
	Name          string
	Scopes        []string
	DeploymentIDs []domain.DeploymentID
}

type CreatedAPICredential struct {
	domain.APICredential
	APIKey string `json:"apiKey"`
}

type CreateServingResponseInput struct {
	Model              string
	Input              json.RawMessage
	Instructions       string
	Metadata           map[string]string
	Background         bool
	IdempotencyKey     string
	Tools              json.RawMessage
	PreviousResponseID string
	MaxOutputTokens    *int
	Store              *bool
	Text               *ResponseTextInput
}

type ResponseTextInput struct {
	Format ResponseTextFormatInput `json:"format"`
}

type ResponseTextFormatInput struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema"`
	Strict      *bool           `json:"strict"`
}

type ResponseTextObject struct {
	Format ResponseTextFormatObject `json:"format"`
}

type ResponseTextFormatObject struct {
	Type        string          `json:"type"`
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Schema      json.RawMessage `json:"schema,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
}

type ResponseOutputContent struct {
	Type        string `json:"type"`
	Text        string `json:"text"`
	Annotations []any  `json:"annotations"`
}

type ResponseOutputItem struct {
	ID               string                  `json:"id"`
	Type             string                  `json:"type"`
	Status           string                  `json:"status"`
	Role             string                  `json:"role,omitempty"`
	Content          []ResponseOutputContent `json:"content,omitempty"`
	CallID           string                  `json:"call_id,omitempty"`
	Name             string                  `json:"name,omitempty"`
	Arguments        string                  `json:"arguments,omitempty"`
	ApprovalRequired bool                    `json:"approval_required,omitempty"`
	ApprovalStatus   string                  `json:"approval_status,omitempty"`
}

type ResponseUsageObject struct {
	InputTokens        int64            `json:"input_tokens"`
	InputTokenDetails  map[string]int64 `json:"input_tokens_details"`
	OutputTokens       int64            `json:"output_tokens"`
	OutputTokenDetails map[string]int64 `json:"output_tokens_details"`
	TotalTokens        int64            `json:"total_tokens"`
}

type ResponseDeploymentObject struct {
	ID                 domain.DeploymentID      `json:"id"`
	Name               string                   `json:"name"`
	RequestedReference string                   `json:"requested_reference"`
	ResolutionKind     string                   `json:"resolution_kind"`
	ReleaseRevisionID  domain.ReleaseRevisionID `json:"release_revision_id,omitempty"`
	AgentVersionID     string                   `json:"agent_version_id"`
}

type ResponseConversationObject struct {
	ID             domain.ConversationID `json:"id"`
	AgentVersionID string                `json:"agent_version_id"`
}

type ResponseObject struct {
	ID                 domain.ResponseID             `json:"id"`
	Object             string                        `json:"object"`
	CreatedAt          int64                         `json:"created_at"`
	CompletedAt        *int64                        `json:"completed_at,omitempty"`
	Status             string                        `json:"status"`
	Error              *domain.ResponseFailure       `json:"error"`
	IncompleteDetails  any                           `json:"incomplete_details"`
	Instructions       any                           `json:"instructions"`
	Model              string                        `json:"model"`
	Output             []ResponseOutputItem          `json:"output"`
	ParallelToolCalls  bool                          `json:"parallel_tool_calls"`
	Tools              []domain.ResponseFunctionTool `json:"tools"`
	Metadata           map[string]string             `json:"metadata"`
	Background         bool                          `json:"background"`
	Store              bool                          `json:"store"`
	PreviousResponseID any                           `json:"previous_response_id"`
	Usage              *ResponseUsageObject          `json:"usage"`
	Deployment         ResponseDeploymentObject      `json:"deployment"`
	Conversation       ResponseConversationObject    `json:"conversation"`
	Text               ResponseTextObject            `json:"text"`
	Cost               map[string]any                `json:"cost"`
}

func (s *Service) CreateAPICredential(ctx context.Context, input CreateAPICredentialInput) (CreatedAPICredential, error) {
	input.TenantID = strings.TrimSpace(input.TenantID)
	input.Name = strings.TrimSpace(input.Name)
	if input.TenantID == "" || input.Name == "" {
		return CreatedAPICredential{}, problem(400, "invalid_api_credential", "API credential requires tenantId and name")
	}
	if len(input.DeploymentIDs) == 0 {
		return CreatedAPICredential{}, problem(400, "deployment_access_required", "API credential requires at least one Deployment")
	}
	allowedScopes := map[string]bool{
		domain.ScopeResponsesCreate:  true,
		domain.ScopeResponsesRead:    true,
		domain.ScopeResponsesCancel:  true,
		domain.ScopeResponsesApprove: true,
	}
	seenScopes := map[string]bool{}
	for _, scope := range input.Scopes {
		if !allowedScopes[scope] {
			return CreatedAPICredential{}, problem(400, "invalid_scope", "unsupported API credential scope: "+scope)
		}
		seenScopes[scope] = true
	}
	if len(seenScopes) == 0 {
		return CreatedAPICredential{}, problem(400, "scope_required", "API credential requires at least one scope")
	}
	input.Scopes = input.Scopes[:0]
	for scope := range seenScopes {
		input.Scopes = append(input.Scopes, scope)
	}
	sort.Strings(input.Scopes)
	seenDeployments := map[domain.DeploymentID]bool{}
	for _, id := range input.DeploymentIDs {
		if seenDeployments[id] {
			continue
		}
		deployment, err := s.repo.GetDeployment(ctx, id)
		if err != nil {
			return CreatedAPICredential{}, err
		}
		if deployment.TenantID != input.TenantID {
			return CreatedAPICredential{}, problem(400, "deployment_tenant_mismatch", "API credential cannot access another tenant's Deployment")
		}
		seenDeployments[id] = true
	}
	input.DeploymentIDs = input.DeploymentIDs[:0]
	for id := range seenDeployments {
		input.DeploymentIDs = append(input.DeploymentIDs, id)
	}
	sort.Slice(input.DeploymentIDs, func(left, right int) bool { return input.DeploymentIDs[left] < input.DeploymentIDs[right] })
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		return CreatedAPICredential{}, err
	}
	secret := "rnk_live_" + base64.RawURLEncoding.EncodeToString(secretBytes)
	credential := domain.APICredential{
		ID: domain.APICredentialID(newServingID("cred")), TenantID: input.TenantID, Name: input.Name,
		KeyPrefix: secret[:16], SecretHash: servingSecretHash(secret), Scopes: input.Scopes,
		DeploymentIDs: input.DeploymentIDs, CreatedAt: s.now().UTC(),
	}
	if err := s.repo.CreateAPICredential(ctx, credential); err != nil {
		return CreatedAPICredential{}, err
	}
	return CreatedAPICredential{APICredential: credential, APIKey: secret}, nil
}

func servingSecretHash(secret string) string {
	digest := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(digest[:])
}

func (s *Service) AuthenticateServingCredential(ctx context.Context, secret, scope string) (domain.APICredential, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return domain.APICredential{}, problem(401, "invalid_api_key", "missing bearer API key")
	}
	secretHash := servingSecretHash(secret)
	credential, err := s.repo.GetAPICredentialBySecretHash(ctx, secretHash)
	if err != nil || subtle.ConstantTimeCompare([]byte(credential.SecretHash), []byte(secretHash)) != 1 || credential.RevokedAt != nil {
		return domain.APICredential{}, problem(401, "invalid_api_key", "invalid bearer API key")
	}
	if !credentialHasScope(credential, scope) {
		return domain.APICredential{}, problem(403, "insufficient_scope", "API credential does not grant "+scope)
	}
	return credential, nil
}

func credentialHasScope(credential domain.APICredential, scope string) bool {
	for _, current := range credential.Scopes {
		if current == scope {
			return true
		}
	}
	return false
}

func credentialAllowsDeployment(credential domain.APICredential, deploymentID domain.DeploymentID) bool {
	for _, current := range credential.DeploymentIDs {
		if current == deploymentID {
			return true
		}
	}
	return false
}

func (s *Service) CreateServingResponse(ctx context.Context, credential domain.APICredential, input CreateServingResponseInput) (domain.ServingResponse, bool, error) {
	if s.execution == nil {
		return domain.ServingResponse{}, false, problem(503, "execution_service_unavailable", "Execution Service is not configured")
	}
	input.Model = strings.TrimSpace(input.Model)
	input.Instructions = strings.TrimSpace(input.Instructions)
	input.PreviousResponseID = strings.TrimSpace(input.PreviousResponseID)
	if input.Model == "" {
		return domain.ServingResponse{}, false, problem(400, "model_required", "model must identify a Deployment reference")
	}
	callerTools, err := normalizeCallerTools(input.Tools)
	if err != nil {
		return domain.ServingResponse{}, false, err
	}
	if input.MaxOutputTokens != nil {
		return domain.ServingResponse{}, false, problem(400, "unsupported_max_output_tokens", "max_output_tokens is not supported by this serving version")
	}
	if input.Store != nil && !*input.Store {
		return domain.ServingResponse{}, false, problem(400, "store_required", "store=false is not supported because accepted Responses are durable")
	}
	normalizedInput, err := normalizeServingInput(input.Input)
	if err != nil {
		return domain.ServingResponse{}, false, err
	}
	if len(input.Metadata) > 16 {
		return domain.ServingResponse{}, false, problem(400, "invalid_metadata", "metadata accepts at most 16 entries")
	}
	for key, value := range input.Metadata {
		if len(key) > 64 || len(value) > 512 {
			return domain.ServingResponse{}, false, problem(400, "invalid_metadata", "metadata keys and values exceed the supported size")
		}
	}
	if input.Metadata == nil {
		input.Metadata = map[string]string{}
	}
	textFormat, err := normalizeResponseText(input.Text)
	if err != nil {
		return domain.ServingResponse{}, false, err
	}
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.IdempotencyKey == "" {
		input.IdempotencyKey = newServingID("req")
	}
	requestHash, err := hashServingRequest(input.Model, normalizedInput.Prompt, servingInputDigestItems(normalizedInput.Items), callerTools, input.Instructions, input.Metadata, input.Background, input.PreviousResponseID, textFormat)
	if err != nil {
		return domain.ServingResponse{}, false, err
	}
	existing, err := s.repo.GetResponseByIdempotencyKey(ctx, credential.ID, input.IdempotencyKey)
	if err == nil {
		if existing.RequestHash != requestHash {
			return domain.ServingResponse{}, false, problem(409, "idempotency_conflict", "Idempotency-Key was already used with another request")
		}
		return existing, true, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return domain.ServingResponse{}, false, err
	}
	resolution, err := s.ResolveDeploymentReference(ctx, credential.TenantID, input.Model)
	if err != nil {
		return domain.ServingResponse{}, false, err
	}
	if !credentialAllowsDeployment(credential, resolution.Deployment.ID) {
		return domain.ServingResponse{}, false, problem(403, "deployment_forbidden", "API credential cannot access this Deployment")
	}
	policy := resolution.Deployment.Policy
	if len(callerTools) > 0 && !policy.AllowCallerTools {
		return domain.ServingResponse{}, false, problem(400, "caller_tools_disabled", "this Deployment does not allow caller function tools")
	}
	if input.Instructions != "" && !policy.AllowInstructions {
		return domain.ServingResponse{}, false, problem(400, "instructions_disabled", "this Deployment does not allow caller instructions")
	}
	now := s.now().UTC()
	var conversation *domain.Conversation
	conversationID := domain.ConversationID("")
	previousResponseID := domain.ResponseID(input.PreviousResponseID)
	if previousResponseID == "" {
		if len(functionCallOutputs(normalizedInput.Items)) > 0 {
			return domain.ServingResponse{}, false, problem(400, "unexpected_function_call_output", "function_call_output requires previous_response_id")
		}
		conversation = &domain.Conversation{
			ID: domain.ConversationID(newServingID("conv")), TenantID: credential.TenantID,
			DeploymentID: resolution.Deployment.ID, AgentVersionID: resolution.AgentVersion.ID,
			CreatedAt: now, UpdatedAt: now,
		}
		if resolution.ReleaseRevision != nil {
			conversation.ReleaseRevisionID = resolution.ReleaseRevision.ID
		}
		conversationID = conversation.ID
	} else {
		previous, err := s.GetServingResponse(ctx, credential, previousResponseID)
		if err != nil {
			return domain.ServingResponse{}, false, err
		}
		if previous.Status != domain.ResponseCompleted {
			return domain.ServingResponse{}, false, problem(409, "previous_response_not_completed", "previous_response_id must identify a completed Response")
		}
		if previous.ConversationID == "" {
			return domain.ServingResponse{}, false, problem(409, "previous_response_not_continuable", "previous_response_id predates durable Conversation support")
		}
		current, err := s.repo.GetConversation(ctx, previous.ConversationID)
		if err != nil {
			return domain.ServingResponse{}, false, err
		}
		if current.TenantID != credential.TenantID || current.DeploymentID != resolution.Deployment.ID {
			return domain.ServingResponse{}, false, problem(400, "conversation_deployment_mismatch", "previous_response_id belongs to another Deployment")
		}
		if current.LastResponseID != previous.ID {
			existing, loadErr := s.repo.GetResponseByIdempotencyKey(ctx, credential.ID, input.IdempotencyKey)
			if loadErr == nil {
				if existing.RequestHash != requestHash {
					return domain.ServingResponse{}, false, problem(409, "idempotency_conflict", "Idempotency-Key was already used with another request")
				}
				return existing, true, nil
			}
			return domain.ServingResponse{}, false, problem(409, "conversation_head_changed", "previous_response_id is not the latest Response in its Conversation")
		}
		if resolution.Kind == domain.ResolutionExact && resolution.AgentVersion.ID != current.AgentVersionID {
			return domain.ServingResponse{}, false, problem(400, "conversation_version_mismatch", "an exact model reference cannot change the Conversation Agent version")
		}
		if len(callerTools) == 0 {
			callerTools = previous.CallerTools
		} else if !sameCallerTools(callerTools, previous.CallerTools) {
			return domain.ServingResponse{}, false, problem(400, "conversation_tools_mismatch", "continuation cannot change caller function tools")
		}
		if err := validateFunctionContinuation(previous, normalizedInput); err != nil {
			return domain.ServingResponse{}, false, err
		}
		conversationID = current.ID
		resolution.Kind = domain.ResolutionConversation
		resolution.AgentVersion, err = s.repo.GetAgentVersion(ctx, current.AgentVersionID)
		if err != nil {
			return domain.ServingResponse{}, false, err
		}
		resolution.ReleaseRevision = nil
		if current.ReleaseRevisionID != "" {
			revision := domain.ReleaseRevision{ID: current.ReleaseRevisionID, AgentVersionID: current.AgentVersionID}
			resolution.ReleaseRevision = &revision
		}
	}
	normalizedInput, err = s.freezeServingInput(ctx, normalizedInput)
	if err != nil {
		return domain.ServingResponse{}, false, err
	}
	if previousResponseID != "" {
		prior, err := s.repo.ListConversationResponses(ctx, conversationID)
		if err != nil {
			return domain.ServingResponse{}, false, err
		}
		attachments := []contract.InputAttachment{}
		for _, previous := range prior {
			attachments = append(attachments, servingInputAttachments(previous.InputItems)...)
		}
		attachments = append(attachments, servingInputAttachments(normalizedInput.Items)...)
		if err := contract.ValidateInputAttachments(attachments); err != nil {
			return domain.ServingResponse{}, false, problem(400, "conversation_input_limit", err.Error())
		}
	}
	response := domain.ServingResponse{
		ID: domain.ResponseID(newServingID("resp")), TenantID: credential.TenantID, CredentialID: credential.ID,
		DeploymentID: resolution.Deployment.ID, DeploymentName: resolution.Deployment.Name,
		RequestedReference: input.Model, ResolutionKind: resolution.Kind, AgentVersionID: resolution.AgentVersion.ID,
		ConversationID: conversationID, PreviousResponseID: previousResponseID,
		IdempotencyKey: input.IdempotencyKey, RequestHash: requestHash, Input: normalizedInput.Prompt, InputItems: normalizedInput.Items,
		CallerTools:  callerTools,
		Instructions: input.Instructions, Metadata: input.Metadata, Background: input.Background,
		TextFormat: textFormat,
		Status:     domain.ResponseQueued, OutputItemID: newServingID("msg"), CreatedAt: now,
	}
	if resolution.ReleaseRevision != nil {
		response.ReleaseRevisionID = resolution.ReleaseRevision.ID
	}
	created := responseEvent(response.ID, "response.created", map[string]any{"response": ResponseObjectFor(response)}, now)
	err = s.repo.AdmitResponse(ctx, response, conversation, created, policy.RequestsPerMinute, policy.MaxConcurrentResponses)
	if err != nil {
		existing, loadErr := s.repo.GetResponseByIdempotencyKey(ctx, credential.ID, input.IdempotencyKey)
		if loadErr == nil {
			if existing.RequestHash != requestHash {
				return domain.ServingResponse{}, false, problem(409, "idempotency_conflict", "Idempotency-Key was already used with another request")
			}
			return existing, true, nil
		}
	}
	if errors.Is(err, domain.ErrRateLimited) {
		return domain.ServingResponse{}, false, problem(429, "rate_limit_exceeded", "Deployment request rate limit exceeded")
	}
	if errors.Is(err, domain.ErrConcurrencyLimit) {
		return domain.ServingResponse{}, false, problem(429, "concurrency_limit_exceeded", "Deployment concurrent Response limit exceeded")
	}
	if errors.Is(err, domain.ErrConversationBusy) {
		return domain.ServingResponse{}, false, problem(409, "conversation_busy", "Conversation already has an active Response")
	}
	if errors.Is(err, domain.ErrConversationHead) {
		return domain.ServingResponse{}, false, problem(409, "conversation_head_changed", "previous_response_id is not the latest Response in its Conversation")
	}
	if err != nil {
		return domain.ServingResponse{}, false, err
	}
	s.publishResponse(response.ID)
	s.dispatchResponse(response.ID)
	return response, false, nil
}

func hashServingRequest(model, input string, inputItems []domain.ResponseInputItem, callerTools []domain.ResponseFunctionTool, instructions string, metadata map[string]string, background bool, previousResponseID string, textFormat domain.ResponseTextFormat) (string, error) {
	payload, err := json.Marshal(struct {
		Model              string                        `json:"model"`
		Input              string                        `json:"input"`
		InputItems         []domain.ResponseInputItem    `json:"input_items"`
		CallerTools        []domain.ResponseFunctionTool `json:"tools"`
		Instructions       string                        `json:"instructions"`
		Metadata           map[string]string             `json:"metadata"`
		Background         bool                          `json:"background"`
		PreviousResponseID string                        `json:"previous_response_id"`
		TextFormat         domain.ResponseTextFormat     `json:"text"`
	}{model, input, inputItems, callerTools, instructions, metadata, background, previousResponseID, textFormat})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func normalizeResponseText(input *ResponseTextInput) (domain.ResponseTextFormat, error) {
	plain := domain.ResponseTextFormat{Type: "text"}
	if input == nil {
		return plain, nil
	}
	format := input.Format
	format.Type = strings.TrimSpace(format.Type)
	format.Name = strings.TrimSpace(format.Name)
	format.Description = strings.TrimSpace(format.Description)
	if format.Type == "text" {
		if format.Name != "" || format.Description != "" || len(format.Schema) != 0 || format.Strict != nil {
			return domain.ResponseTextFormat{}, problem(400, "invalid_text_format", "text format does not accept JSON Schema fields")
		}
		return plain, nil
	}
	if format.Type != "json_schema" {
		return domain.ResponseTextFormat{}, problem(400, "unsupported_text_format", "text.format.type must be text or json_schema")
	}
	if !responseFormatNamePattern.MatchString(format.Name) {
		return domain.ResponseTextFormat{}, problem(400, "invalid_text_format", "JSON Schema format name must contain 1 to 64 letters, numbers, underscores, or hyphens")
	}
	if len(format.Description) > 512 {
		return domain.ResponseTextFormat{}, problem(400, "invalid_text_format", "JSON Schema format description exceeds 512 characters")
	}
	if format.Strict != nil && !*format.Strict {
		return domain.ResponseTextFormat{}, problem(400, "unsupported_text_format", "json_schema format requires strict=true")
	}
	var schema any
	if len(format.Schema) == 0 || json.Unmarshal(format.Schema, &schema) != nil {
		return domain.ResponseTextFormat{}, problem(400, "invalid_text_format", "json_schema format requires a valid schema object")
	}
	canonical, err := json.Marshal(schema)
	if err != nil {
		return domain.ResponseTextFormat{}, problem(400, "invalid_text_format", "json_schema format requires a valid schema object")
	}
	result := domain.ResponseTextFormat{Type: format.Type, Name: format.Name, Description: format.Description, Schema: canonical}
	if err := contract.ValidateStructuredOutput(&contract.StructuredOutput{Name: result.Name, Description: result.Description, Schema: result.Schema}); err != nil {
		return domain.ResponseTextFormat{}, problem(400, "invalid_text_format", err.Error())
	}
	return result, nil
}

func decodeServingJSON(raw json.RawMessage, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("multiple JSON values")
	}
	return nil
}

func (s *Service) servingExecutionInput(ctx context.Context, response domain.ServingResponse) (string, []contract.InputAttachment, error) {
	if response.PreviousResponseID == "" {
		return response.Input, servingInputAttachments(response.InputItems), nil
	}
	responses, err := s.repo.ListConversationResponses(ctx, response.ConversationID)
	if err != nil {
		return "", nil, err
	}
	type conversationMessage struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	messages := make([]conversationMessage, 0, len(responses)*2)
	attachments := []contract.InputAttachment{}
	found := false
	for _, item := range responses {
		messages = append(messages, conversationMessage{Role: "user", Content: servingConversationInput(item)})
		attachments = append(attachments, servingInputAttachments(item.InputItems)...)
		if item.ID == response.ID {
			found = true
			break
		}
		if item.Status != domain.ResponseCompleted {
			return "", nil, problem(409, "conversation_history_incomplete", "Conversation history contains a non-completed Response")
		}
		messages = append(messages, conversationMessage{Role: "assistant", Content: servingConversationOutput(item)})
	}
	if !found {
		return "", nil, domain.ErrNotFound
	}
	payload, err := json.Marshal(messages)
	if err != nil {
		return "", nil, err
	}
	return "Continue the conversation in this JSON message history. Respond to the final user message.\n" + string(payload), attachments, nil
}

func servingConversationInput(response domain.ServingResponse) string {
	parts := []string{}
	for _, item := range response.InputItems {
		if item.Type == "input_text" {
			parts = append(parts, item.Text)
			continue
		}
		if item.Type == "function_call_output" {
			parts = append(parts, fmt.Sprintf("[function_call_output: %s]\n%s", item.CallID, item.Output))
			continue
		}
		parts = append(parts, fmt.Sprintf("[%s: %s, %s]", item.Type, item.Name, item.ContentDigest))
	}
	if len(parts) == 0 {
		return response.Input
	}
	return strings.Join(parts, "\n")
}

func servingConversationOutput(response domain.ServingResponse) string {
	if response.ToolCall != nil {
		return fmt.Sprintf("[function_call: %s %s %s]", response.ToolCall.ID, response.ToolCall.Name, response.ToolCall.Arguments)
	}
	return response.OutputText
}

func (s *Service) dispatchResponse(id domain.ResponseID) {
	s.mu.Lock()
	if _, exists := s.responseWorkers[id]; exists {
		s.mu.Unlock()
		return
	}
	s.responseWorkers[id] = struct{}{}
	s.mu.Unlock()
	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.responseWorkers, id)
			s.mu.Unlock()
		}()
		_ = s.runServingResponse(context.Background(), id)
	}()
}

func (s *Service) runServingResponse(ctx context.Context, id domain.ResponseID) error {
	response, err := s.repo.GetResponse(ctx, id)
	if err != nil || domain.IsResponseTerminal(response.Status) {
		return err
	}
	version, err := s.repo.GetAgentVersion(ctx, response.AgentVersionID)
	if err != nil {
		return s.failServingResponse(ctx, response, "agent_version_unavailable", err.Error())
	}
	deployment, err := s.repo.GetDeployment(ctx, response.DeploymentID)
	if err != nil {
		return s.failServingResponse(ctx, response, "deployment_unavailable", err.Error())
	}
	systemPrompt := version.SystemPrompt
	if response.Instructions != "" {
		systemPrompt = strings.TrimSpace(systemPrompt + "\n\n" + response.Instructions)
	}
	prompt, attachments, err := s.servingExecutionInput(ctx, response)
	if err != nil {
		return s.failServingResponse(ctx, response, "conversation_history_unavailable", err.Error())
	}
	var structuredOutput *contract.StructuredOutput
	if response.TextFormat.Type == "json_schema" {
		structuredOutput = &contract.StructuredOutput{
			Name: response.TextFormat.Name, Description: response.TextFormat.Description, Schema: response.TextFormat.Schema,
		}
	}
	execution, err := s.execution.Submit(ctx, contract.SubmitRequest{
		IdempotencyKey: "response:" + string(response.ID),
		Spec: contract.Spec{
			ProtocolVersion: contract.ProtocolVersion, Kind: contract.KindAgent, Harness: version.RunnerType,
			ModelConnectionID: version.ModelConnectionID, Model: runtimeModel(version.Model), Preset: version.Preset,
			SystemPrompt: systemPrompt, Tools: version.Tools, Skills: version.Skills, Prompt: callerToolPrompt(prompt, response.CallerTools),
			TimeoutMs: deployment.Policy.ExecutionTimeoutMs,
			Metadata: map[string]string{
				"responseId": string(response.ID), "deploymentId": string(response.DeploymentID),
				"agentVersionId": response.AgentVersionID, "releaseRevisionId": string(response.ReleaseRevisionID),
				"conversationId": string(response.ConversationID), "previousResponseId": string(response.PreviousResponseID),
			},
			StructuredOutput: structuredOutput, InputAttachments: attachments,
		},
	})
	if err != nil {
		return s.failServingResponse(ctx, response, "execution_submit_failed", err.Error())
	}
	if response.Status == domain.ResponseQueued {
		now := s.now().UTC()
		response.Status, response.ExecutionID, response.StartedAt = domain.ResponseInProgress, execution.ID, &now
		started := responseEvent(response.ID, "response.in_progress", map[string]any{"response": ResponseObjectFor(response)}, now)
		if err := s.repo.UpdateResponse(ctx, response, domain.ResponseQueued, []domain.ResponseEvent{started}); err != nil {
			if errors.Is(err, domain.ErrConflict) {
				_, _ = s.execution.Cancel(context.Background(), execution.ID)
				return nil
			}
			return err
		}
		s.publishResponse(response.ID)
	}
	_, watchErr := s.execution.Watch(ctx, execution.ID, 0, nil)
	execution, getErr := s.execution.Get(ctx, execution.ID)
	if getErr != nil {
		if watchErr != nil {
			getErr = fmt.Errorf("%v; %w", watchErr, getErr)
		}
		return s.failServingResponse(ctx, response, "execution_unavailable", getErr.Error())
	}
	current, err := s.repo.GetResponse(ctx, response.ID)
	if err != nil || domain.IsResponseTerminal(current.Status) {
		return err
	}
	if execution.Status == contract.StatusCancelled {
		return s.finishServingResponse(ctx, current, domain.ResponseCancelled, execution, "response_cancelled", "Response was cancelled")
	}
	if execution.Status != contract.StatusCompleted || execution.Result == nil {
		message := execution.Error
		if message == "" && watchErr != nil {
			message = watchErr.Error()
		}
		if message == "" {
			message = "Execution ended without a result"
		}
		return s.finishServingResponse(ctx, current, domain.ResponseFailed, execution, "execution_failed", message)
	}
	return s.finishServingResponse(ctx, current, domain.ResponseCompleted, execution, "", "")
}

func (s *Service) failServingResponse(ctx context.Context, response domain.ServingResponse, code, message string) error {
	current, err := s.repo.GetResponse(ctx, response.ID)
	if err != nil || domain.IsResponseTerminal(current.Status) {
		return err
	}
	return s.finishServingResponse(ctx, current, domain.ResponseFailed, contract.Execution{}, code, message)
}

func (s *Service) finishServingResponse(ctx context.Context, response domain.ServingResponse, status string, execution contract.Execution, code, message string) error {
	now := s.now().UTC()
	expectedStatus := response.Status
	response.Status, response.CompletedAt = status, &now
	if execution.ID != "" {
		response.ExecutionID = execution.ID
	}
	if execution.Result != nil {
		response.OutputText = execution.Result.Output
		toolCall, parseErr := parseCallerToolCall(response.OutputText, response.CallerTools)
		if parseErr != nil {
			response.Failure = &domain.ResponseFailure{Code: "invalid_function_call", Message: parseErr.Error()}
			response.Status = domain.ResponseFailed
		} else if toolCall != nil {
			response.ToolCall, response.OutputText = toolCall, ""
			if toolCall.ApprovalRequired {
				response.Status, response.CompletedAt, toolCall.ApprovalStatus = domain.ResponseRequiresAction, nil, "pending"
			}
		}
		response.Usage = domain.ResponseUsage{
			InputTokens: execution.Result.Usage.InputTokens, OutputTokens: execution.Result.Usage.OutputTokens,
			ReasoningTokens: execution.Result.Usage.ReasoningTokens,
			TotalTokens:     execution.Result.Usage.InputTokens + execution.Result.Usage.OutputTokens,
		}
		response.Cost, response.CostKnown = execution.Result.Cost, execution.Result.CostKnown
	}
	if code != "" || message != "" {
		response.Failure = &domain.ResponseFailure{Code: code, Message: message}
	}
	events := terminalResponseEvents(response, now)
	if err := s.updateResponseWithWebhooks(ctx, response, expectedStatus, events); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return nil
		}
		return err
	}
	s.publishResponse(response.ID)
	return nil
}

func terminalResponseEvents(response domain.ServingResponse, at time.Time) []domain.ResponseEvent {
	if response.Status == domain.ResponseFailed {
		return []domain.ResponseEvent{responseEvent(response.ID, "response.failed", map[string]any{"response": ResponseObjectFor(response)}, at)}
	}
	if response.Status == domain.ResponseCancelled {
		return []domain.ResponseEvent{responseEvent(response.ID, "response.cancelled", map[string]any{"response": ResponseObjectFor(response)}, at)}
	}
	if response.ToolCall != nil {
		item := responseToolCallItem(response)
		terminalType := "response.completed"
		if response.Status == domain.ResponseRequiresAction {
			terminalType = "response.requires_action"
		}
		return []domain.ResponseEvent{
			responseEvent(response.ID, "response.output_item.added", map[string]any{"output_index": 0, "item": item}, at),
			responseEvent(response.ID, "response.function_call_arguments.delta", map[string]any{"item_id": item.ID, "output_index": 0, "delta": item.Arguments}, at),
			responseEvent(response.ID, "response.function_call_arguments.done", map[string]any{"item_id": item.ID, "output_index": 0, "arguments": item.Arguments}, at),
			responseEvent(response.ID, "response.output_item.done", map[string]any{"output_index": 0, "item": item}, at),
			responseEvent(response.ID, terminalType, map[string]any{"response": ResponseObjectFor(response)}, at),
		}
	}
	item := ResponseOutputItem{ID: response.OutputItemID, Type: "message", Status: "completed", Role: "assistant", Content: []ResponseOutputContent{{Type: "output_text", Text: response.OutputText, Annotations: []any{}}}}
	content := item.Content[0]
	return []domain.ResponseEvent{
		responseEvent(response.ID, "response.output_item.added", map[string]any{"output_index": 0, "item": ResponseOutputItem{ID: item.ID, Type: item.Type, Status: "in_progress", Role: item.Role, Content: []ResponseOutputContent{}}}, at),
		responseEvent(response.ID, "response.content_part.added", map[string]any{"item_id": item.ID, "output_index": 0, "content_index": 0, "part": ResponseOutputContent{Type: "output_text", Text: "", Annotations: []any{}}}, at),
		responseEvent(response.ID, "response.output_text.delta", map[string]any{"item_id": item.ID, "output_index": 0, "content_index": 0, "delta": response.OutputText}, at),
		responseEvent(response.ID, "response.output_text.done", map[string]any{"item_id": item.ID, "output_index": 0, "content_index": 0, "text": response.OutputText}, at),
		responseEvent(response.ID, "response.content_part.done", map[string]any{"item_id": item.ID, "output_index": 0, "content_index": 0, "part": content}, at),
		responseEvent(response.ID, "response.output_item.done", map[string]any{"output_index": 0, "item": item}, at),
		responseEvent(response.ID, "response.completed", map[string]any{"response": ResponseObjectFor(response)}, at),
	}
}

func responseToolCallItem(response domain.ServingResponse) ResponseOutputItem {
	status := "completed"
	if response.ToolCall.ApprovalStatus == "pending" {
		status = "in_progress"
	}
	return ResponseOutputItem{ID: response.OutputItemID, Type: "function_call", Status: status, CallID: response.ToolCall.ID, Name: response.ToolCall.Name, Arguments: string(response.ToolCall.Arguments), ApprovalRequired: response.ToolCall.ApprovalRequired, ApprovalStatus: response.ToolCall.ApprovalStatus}
}

func responseEvent(responseID domain.ResponseID, eventType string, data map[string]any, at time.Time) domain.ResponseEvent {
	payload, err := json.Marshal(data)
	if err != nil {
		panic(err)
	}
	return domain.ResponseEvent{ResponseID: responseID, Type: eventType, Data: payload, CreatedAt: at}
}

func ResponseObjectFor(response domain.ServingResponse) ResponseObject {
	format := ResponseTextFormatObject{Type: response.TextFormat.Type}
	if format.Type == "" {
		format.Type = "text"
	}
	if format.Type == "json_schema" {
		strict := true
		format.Name, format.Description, format.Schema, format.Strict = response.TextFormat.Name, response.TextFormat.Description, response.TextFormat.Schema, &strict
	}
	result := ResponseObject{
		ID: response.ID, Object: "response", CreatedAt: response.CreatedAt.Unix(), Status: response.Status,
		Error: response.Failure, IncompleteDetails: nil, Model: response.RequestedReference,
		Output: []ResponseOutputItem{}, ParallelToolCalls: false, Tools: response.CallerTools, Metadata: response.Metadata,
		Background: response.Background, Store: true, PreviousResponseID: nil,
		Deployment: ResponseDeploymentObject{
			ID: response.DeploymentID, Name: response.DeploymentName, RequestedReference: response.RequestedReference,
			ResolutionKind: response.ResolutionKind, ReleaseRevisionID: response.ReleaseRevisionID,
			AgentVersionID: response.AgentVersionID,
		},
		Conversation: ResponseConversationObject{ID: response.ConversationID, AgentVersionID: response.AgentVersionID},
		Text:         ResponseTextObject{Format: format},
		Cost:         map[string]any{"amount": response.Cost, "currency": "USD", "known": response.CostKnown},
	}
	if response.PreviousResponseID != "" {
		result.PreviousResponseID = response.PreviousResponseID
	}
	if response.Instructions != "" {
		result.Instructions = response.Instructions
	}
	if response.CompletedAt != nil {
		completed := response.CompletedAt.Unix()
		result.CompletedAt = &completed
	}
	if response.Status == domain.ResponseCompleted || response.Status == domain.ResponseRequiresAction || (response.Status == domain.ResponseCancelled && response.ToolCall != nil) {
		if response.ToolCall != nil {
			result.Output = []ResponseOutputItem{responseToolCallItem(response)}
		} else {
			result.Output = []ResponseOutputItem{{
				ID: response.OutputItemID, Type: "message", Status: "completed", Role: "assistant",
				Content: []ResponseOutputContent{{Type: "output_text", Text: response.OutputText, Annotations: []any{}}},
			}}
		}
		result.Usage = &ResponseUsageObject{
			InputTokens: response.Usage.InputTokens, InputTokenDetails: map[string]int64{"cached_tokens": 0},
			OutputTokens: response.Usage.OutputTokens, OutputTokenDetails: map[string]int64{"reasoning_tokens": response.Usage.ReasoningTokens},
			TotalTokens: response.Usage.TotalTokens,
		}
	}
	return result
}

// DecideServingApproval records the caller's decision for one pending
// approval-gated function call without executing that function in Rank.
func (s *Service) DecideServingApproval(ctx context.Context, credential domain.APICredential, id domain.ResponseID, approved bool, reason string) (domain.ServingResponse, error) {
	response, err := s.GetServingResponse(ctx, credential, id)
	if err != nil {
		return response, err
	}
	if response.ToolCall != nil && ((approved && response.Status == domain.ResponseCompleted && response.ToolCall.ApprovalStatus == "approved") || (!approved && response.Status == domain.ResponseCancelled && response.ToolCall.ApprovalStatus == "rejected")) {
		return response, nil
	}
	if response.Status != domain.ResponseRequiresAction || response.ToolCall == nil || response.ToolCall.ApprovalStatus != "pending" {
		return response, problem(409, "approval_not_pending", "Response has no pending function approval")
	}
	now := s.now().UTC()
	response.CompletedAt = &now
	eventType := "response.completed"
	if approved {
		response.Status, response.ToolCall.ApprovalStatus = domain.ResponseCompleted, "approved"
	} else {
		response.Status, response.ToolCall.ApprovalStatus = domain.ResponseCancelled, "rejected"
		response.Failure = &domain.ResponseFailure{Code: "approval_rejected", Message: strings.TrimSpace(reason)}
		if response.Failure.Message == "" {
			response.Failure.Message = "Function call approval was rejected"
		}
		eventType = "response.cancelled"
	}
	events := []domain.ResponseEvent{
		responseEvent(response.ID, "response.approval.completed", map[string]any{"call_id": response.ToolCall.ID, "approved": approved, "reason": strings.TrimSpace(reason)}, now),
		responseEvent(response.ID, eventType, map[string]any{"response": ResponseObjectFor(response)}, now),
	}
	if err := s.updateResponseWithWebhooks(ctx, response, domain.ResponseRequiresAction, events); err != nil {
		return response, err
	}
	s.publishResponse(response.ID)
	return response, nil
}

func (s *Service) GetServingResponse(ctx context.Context, credential domain.APICredential, id domain.ResponseID) (domain.ServingResponse, error) {
	response, err := s.repo.GetResponse(ctx, id)
	if err != nil {
		return response, err
	}
	if response.TenantID != credential.TenantID || !credentialAllowsDeployment(credential, response.DeploymentID) {
		return domain.ServingResponse{}, domain.ErrNotFound
	}
	return response, nil
}

func (s *Service) CancelServingResponse(ctx context.Context, credential domain.APICredential, id domain.ResponseID) (domain.ServingResponse, error) {
	response, err := s.GetServingResponse(ctx, credential, id)
	if err != nil {
		return response, err
	}
	if domain.IsResponseTerminal(response.Status) {
		return response, nil
	}
	if response.ExecutionID != "" {
		if _, err := s.execution.Cancel(ctx, response.ExecutionID); err != nil {
			return response, err
		}
	}
	now := s.now().UTC()
	expected := response.Status
	response.Status, response.CompletedAt = domain.ResponseCancelled, &now
	response.Failure = &domain.ResponseFailure{Code: "response_cancelled", Message: "Response was cancelled"}
	event := responseEvent(response.ID, "response.cancelled", map[string]any{"response": ResponseObjectFor(response)}, now)
	if err := s.updateResponseWithWebhooks(ctx, response, expected, []domain.ResponseEvent{event}); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return s.repo.GetResponse(ctx, response.ID)
		}
		return response, err
	}
	s.publishResponse(response.ID)
	return response, nil
}

func (s *Service) WaitServingResponse(ctx context.Context, id domain.ResponseID) (domain.ServingResponse, error) {
	wakeup := make(chan struct{}, 1)
	s.subscribeResponse(id, wakeup)
	defer s.unsubscribeResponse(id, wakeup)
	for {
		response, err := s.repo.GetResponse(ctx, id)
		if err != nil || domain.IsResponseTerminal(response.Status) {
			return response, err
		}
		select {
		case <-ctx.Done():
			return domain.ServingResponse{}, ctx.Err()
		case <-wakeup:
		case <-time.After(15 * time.Second):
		}
	}
}

func (s *Service) WaitResponseEvents(ctx context.Context, id domain.ResponseID, after int64, heartbeat time.Duration) ([]domain.ResponseEvent, bool, error) {
	if heartbeat <= 0 {
		heartbeat = 15 * time.Second
	}
	wakeup := make(chan struct{}, 1)
	s.subscribeResponse(id, wakeup)
	defer s.unsubscribeResponse(id, wakeup)
	for {
		events, err := s.repo.ListResponseEvents(ctx, id, after)
		if err != nil || len(events) > 0 {
			return events, false, err
		}
		response, err := s.repo.GetResponse(ctx, id)
		if err != nil {
			return nil, false, err
		}
		if domain.IsResponseTerminal(response.Status) {
			events, err = s.repo.ListResponseEvents(ctx, id, after)
			return events, len(events) == 0, err
		}
		timer := time.NewTimer(heartbeat)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, false, ctx.Err()
		case <-wakeup:
			timer.Stop()
		case <-timer.C:
			return nil, false, nil
		}
	}
}

func (s *Service) ResumeActiveResponses(ctx context.Context) error {
	if s.execution == nil {
		return nil
	}
	responses, err := s.repo.ListActiveResponses(ctx)
	if err != nil {
		return err
	}
	for _, response := range responses {
		s.dispatchResponse(response.ID)
	}
	return nil
}

func (s *Service) subscribeResponse(id domain.ResponseID, wakeup chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.responseSubscriptions[id] == nil {
		s.responseSubscriptions[id] = map[chan struct{}]struct{}{}
	}
	s.responseSubscriptions[id][wakeup] = struct{}{}
}

func (s *Service) unsubscribeResponse(id domain.ResponseID, wakeup chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.responseSubscriptions[id], wakeup)
	if len(s.responseSubscriptions[id]) == 0 {
		delete(s.responseSubscriptions, id)
	}
}

func (s *Service) publishResponse(id domain.ResponseID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for wakeup := range s.responseSubscriptions[id] {
		select {
		case wakeup <- struct{}{}:
		default:
		}
	}
}
