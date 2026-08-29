package httpapi

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/xyzbit/chat2ranker/execution/backend/contract"
	"github.com/xyzbit/chat2ranker/rank/backend/internal/app"
	"github.com/xyzbit/chat2ranker/rank/backend/internal/domain"
)

type Options struct {
	ControlToken string
	ControlURL   string
}

type Handler struct {
	service      *app.Service
	controlToken string
	controlURL   string
}

func New(service *app.Service, options ...Options) http.Handler {
	configured := Options{ControlToken: "rank-local-control-token"}
	if len(options) > 0 {
		configured = options[0]
	}
	handler := &Handler{service: service, controlToken: configured.ControlToken, controlURL: strings.TrimRight(configured.ControlURL, "/")}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", handler.health)
	mux.HandleFunc("GET /api/bootstrap", handler.bootstrap)
	mux.HandleFunc("GET /api/model-connections", handler.listModelConnections)
	mux.HandleFunc("POST /api/model-connections", handler.createModelConnection)
	mux.HandleFunc("PATCH /api/model-connections/{id}", handler.updateModelConnection)
	mux.HandleFunc("POST /api/model-connections/{id}/verify", handler.verifyModelConnection)
	mux.HandleFunc("DELETE /api/model-connections/{id}", handler.deleteModelConnection)
	mux.HandleFunc("PUT /api/system-models/{role}", handler.saveSystemModel)
	mux.HandleFunc("POST /api/experiments", handler.createExperiment)
	mux.HandleFunc("GET /api/experiments/{id}", handler.getExperiment)
	mux.HandleFunc("PATCH /api/experiments/{id}", handler.updateExperiment)
	mux.HandleFunc("POST /api/experiments/{id}/messages", handler.addMessage)
	mux.HandleFunc("POST /api/experiments/{id}/commands", handler.executeA2UICommand)
	mux.HandleFunc("POST /api/experiments/{id}/runs", handler.startRun)
	mux.HandleFunc("POST /api/datasets", handler.createDataset)
	mux.HandleFunc("POST /api/dataset-families/{id}/versions", handler.createDatasetVersion)
	mux.HandleFunc("POST /api/agents", handler.createAgent)
	mux.HandleFunc("POST /api/agent-families/{id}/versions", handler.createAgentVersion)
	mux.HandleFunc("GET /api/runs/{id}", handler.getRun)
	mux.HandleFunc("GET /api/runs/{id}/events", handler.runEvents)
	mux.HandleFunc("GET /api/runs/{id}/artifacts", handler.getArtifact)
	mux.HandleFunc("POST /api/runs/{id}/cancel", handler.cancelRun)
	mux.HandleFunc("POST /api/internal/control/sessions/bind", handler.bindControlSession)
	mux.HandleFunc("POST /api/internal/control/commands", handler.executeControlCommand)
	mux.HandleFunc("POST /api/internal/control/transcript", handler.appendControlTranscript)
	return recoverMiddleware(cors(mux))
}

func (h *Handler) saveSystemModel(response http.ResponseWriter, request *http.Request) {
	var input contract.SystemModelBindingInput
	if err := decode(request, &input); err != nil {
		writeError(response, err)
		return
	}
	value, err := h.service.SaveSystemModel(request.Context(), request.PathValue("role"), input)
	if err != nil {
		writeError(response, err)
		return
	}
	if value.Role == contract.SystemModelControl && h.controlURL != "" {
		reload, reloadErr := http.NewRequestWithContext(request.Context(), http.MethodPost, h.controlURL+"/control/v1/reload", bytes.NewReader([]byte("{}")))
		if reloadErr == nil {
			reload.Header.Set("X-Rank-Control-Token", h.controlToken)
			if result, callErr := http.DefaultClient.Do(reload); callErr == nil {
				result.Body.Close()
			}
		}
	}
	writeJSON(response, 200, value)
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Access-Control-Allow-Origin", "*")
		response.Header().Set("Access-Control-Allow-Headers", "Content-Type, Idempotency-Key, X-Rank-Action-Token, X-Rank-Control-Token")
		response.Header().Set("Access-Control-Allow-Methods", "GET,POST,PATCH,DELETE,OPTIONS")
		if request.Method == http.MethodOptions {
			response.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(response, request)
	})
}

func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				writeError(response, &app.Error{Status: 500, Code: "internal_error", Message: "服务器错误"})
			}
		}()
		next.ServeHTTP(response, request)
	})
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

func writeError(response http.ResponseWriter, err error) {
	var problem *app.Error
	if errors.As(err, &problem) {
		writeJSON(response, problem.Status, map[string]any{"error": map[string]any{"code": problem.Code, "message": problem.Message}})
		return
	}
	if errors.Is(err, domain.ErrNotFound) {
		writeJSON(response, 404, map[string]any{"error": map[string]any{"code": "not_found", "message": "资源不存在"}})
		return
	}
	if errors.Is(err, domain.ErrConflict) {
		writeJSON(response, 409, map[string]any{"error": map[string]any{"code": "conflict", "message": "资源状态冲突"}})
		return
	}
	writeJSON(response, 500, map[string]any{"error": map[string]any{"code": "internal_error", "message": err.Error()}})
}

func decode(request *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(request.Body, 2<<20))
	if err := decoder.Decode(target); err != nil && err != io.EOF {
		return &app.Error{Status: 400, Code: "invalid_json", Message: "请求 JSON 格式无效"}
	}
	return nil
}

func (h *Handler) health(response http.ResponseWriter, request *http.Request) {
	writeJSON(response, 200, map[string]any{"ok": true, "runtime": "rankd", "storage": "sqlite"})
}
func (h *Handler) bootstrap(response http.ResponseWriter, request *http.Request) {
	value, err := h.service.Bootstrap(request.Context())
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, 200, value)
}

func (h *Handler) createExperiment(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Title string `json:"title"`
	}
	if err := decode(request, &input); err != nil {
		writeError(response, err)
		return
	}
	value, err := h.service.CreateExperiment(request.Context(), input.Title)
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, 201, value)
}
func (h *Handler) getExperiment(response http.ResponseWriter, request *http.Request) {
	value, err := h.service.GetExperiment(request.Context(), request.PathValue("id"))
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, 200, value)
}
func (h *Handler) updateExperiment(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Title            *string `json:"title"`
		DatasetVersionID *string `json:"datasetVersionId"`
		AgentVersionID   *string `json:"agentVersionId"`
	}
	if err := decode(request, &input); err != nil {
		writeError(response, err)
		return
	}
	value, err := h.service.UpdateExperiment(request.Context(), request.PathValue("id"), app.ExperimentPatch{Title: input.Title, DatasetVersionID: input.DatasetVersionID, AgentVersionID: input.AgentVersionID})
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, 200, value)
}
func (h *Handler) addMessage(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Content string `json:"content"`
	}
	if err := decode(request, &input); err != nil {
		writeError(response, err)
		return
	}
	value, err := h.service.AddMessage(request.Context(), request.PathValue("id"), input.Content)
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, 201, value)
}

type controlCommandRequest struct {
	ExperimentID     string          `json:"experimentId"`
	ControlSessionID string          `json:"controlSessionId"`
	IdempotencyKey   string          `json:"idempotencyKey"`
	Type             string          `json:"type"`
	Payload          json.RawMessage `json:"payload"`
	ActionToken      string          `json:"actionToken"`
}

func idempotencyKey(request *http.Request, bodyKey string) string {
	key := strings.TrimSpace(request.Header.Get("Idempotency-Key"))
	if key == "" {
		key = strings.TrimSpace(bodyKey)
	}
	return key
}

func (h *Handler) executeA2UICommand(response http.ResponseWriter, request *http.Request) {
	var input controlCommandRequest
	if err := decode(request, &input); err != nil {
		writeError(response, err)
		return
	}
	experimentID := request.PathValue("id")
	token := strings.TrimSpace(request.Header.Get("X-Rank-Action-Token"))
	if token == "" {
		token = input.ActionToken
	}
	if err := h.service.AuthorizeAction(experimentID, input.Type, token); err != nil {
		writeError(response, err)
		return
	}
	experiment, err := h.service.GetExperiment(request.Context(), experimentID)
	if err != nil {
		writeError(response, err)
		return
	}
	key := idempotencyKey(request, input.IdempotencyKey)
	command, err := h.service.ApplyControlCommand(request.Context(), app.ControlCommandInput{
		ExperimentID: experimentID, ControlSessionID: experiment.ControlSessionID,
		IdempotencyKey: key, Type: input.Type, Payload: input.Payload,
	})
	if err != nil {
		writeError(response, err)
		return
	}
	if input.Type == app.ControlStartRun {
		var settings struct {
			TrialCount      int      `json:"trialCount"`
			AgentVersionIDs []string `json:"agentVersionIds"`
		}
		if len(input.Payload) > 0 {
			if err := json.Unmarshal(input.Payload, &settings); err != nil {
				writeError(response, &app.Error{Status: 400, Code: "invalid_run_options", Message: "运行参数无效"})
				return
			}
		}
		var run any
		var startErr error
		if len(settings.AgentVersionIDs) > 1 {
			run, startErr = h.service.StartComparison(request.Context(), experimentID, key, app.ComparisonOptions{AgentVersionIDs: settings.AgentVersionIDs, TrialCount: settings.TrialCount})
		} else {
			run, startErr = h.service.StartRun(request.Context(), experimentID, key, app.RunOptions{TrialCount: settings.TrialCount})
		}
		if startErr != nil {
			writeError(response, startErr)
			return
		}
		view, getErr := h.service.GetExperiment(request.Context(), experimentID)
		if getErr != nil {
			writeError(response, getErr)
			return
		}
		writeJSON(response, 202, map[string]any{"command": command, "experiment": view, "run": run})
		return
	}
	view, err := h.service.GetExperiment(request.Context(), experimentID)
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, 200, map[string]any{"command": command, "experiment": view})
}

func (h *Handler) authorizeControl(request *http.Request) error {
	want := []byte(h.controlToken)
	got := []byte(request.Header.Get("X-Rank-Control-Token"))
	if len(want) == 0 || len(got) != len(want) || subtle.ConstantTimeCompare(got, want) != 1 {
		return &app.Error{Status: 401, Code: "control_unauthorized", Message: "Control Host 凭证无效"}
	}
	return nil
}

func (h *Handler) bindControlSession(response http.ResponseWriter, request *http.Request) {
	if err := h.authorizeControl(request); err != nil {
		writeError(response, err)
		return
	}
	var input struct {
		ExperimentID     string `json:"experimentId"`
		ControlSessionID string `json:"controlSessionId"`
	}
	if err := decode(request, &input); err != nil {
		writeError(response, err)
		return
	}
	view, err := h.service.BindControlSession(request.Context(), input.ExperimentID, input.ControlSessionID)
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, 200, view)
}

func (h *Handler) executeControlCommand(response http.ResponseWriter, request *http.Request) {
	if err := h.authorizeControl(request); err != nil {
		writeError(response, err)
		return
	}
	var input controlCommandRequest
	if err := decode(request, &input); err != nil {
		writeError(response, err)
		return
	}
	command, err := h.service.ApplyControlCommand(request.Context(), app.ControlCommandInput{
		ExperimentID: input.ExperimentID, ControlSessionID: input.ControlSessionID,
		IdempotencyKey: idempotencyKey(request, input.IdempotencyKey), Type: input.Type, Payload: input.Payload,
	})
	if err != nil {
		writeError(response, err)
		return
	}
	view, err := h.service.GetExperiment(request.Context(), input.ExperimentID)
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, 200, map[string]any{"command": command, "experiment": view})
}

func (h *Handler) appendControlTranscript(response http.ResponseWriter, request *http.Request) {
	if err := h.authorizeControl(request); err != nil {
		writeError(response, err)
		return
	}
	var input struct {
		ExperimentID     string           `json:"experimentId"`
		ControlSessionID string           `json:"controlSessionId"`
		Messages         []domain.Message `json:"messages"`
	}
	if err := decode(request, &input); err != nil {
		writeError(response, err)
		return
	}
	view, err := h.service.AppendControlMessages(request.Context(), input.ExperimentID, input.ControlSessionID, input.Messages)
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, 200, view)
}

func (h *Handler) startRun(response http.ResponseWriter, request *http.Request) {
	key := strings.TrimSpace(request.Header.Get("Idempotency-Key"))
	var input struct {
		IdempotencyKey  string   `json:"idempotencyKey"`
		TrialCount      int      `json:"trialCount"`
		AgentVersionIDs []string `json:"agentVersionIds"`
	}
	if request.ContentLength != 0 {
		if err := decode(request, &input); err != nil {
			writeError(response, err)
			return
		}
	}
	if key == "" {
		key = input.IdempotencyKey
	}
	var value any
	var err error
	if len(input.AgentVersionIDs) > 1 {
		value, err = h.service.StartComparison(request.Context(), request.PathValue("id"), key, app.ComparisonOptions{AgentVersionIDs: input.AgentVersionIDs, TrialCount: input.TrialCount})
	} else {
		value, err = h.service.StartRun(request.Context(), request.PathValue("id"), key, app.RunOptions{TrialCount: input.TrialCount})
	}
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, 202, value)
}

type datasetRequest struct {
	Name        string          `json:"name"`
	Source      string          `json:"source"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema"`
	Rubric      json.RawMessage `json:"rubric"`
	Cases       []domain.Case   `json:"cases"`
}

func (input datasetRequest) appInput() app.CreateDatasetInput {
	return app.CreateDatasetInput{Name: input.Name, Source: input.Source, Description: input.Description, Schema: input.Schema, Rubric: input.Rubric, Cases: input.Cases}
}
func (h *Handler) createDataset(response http.ResponseWriter, request *http.Request) {
	var input datasetRequest
	if err := decode(request, &input); err != nil {
		writeError(response, err)
		return
	}
	value, err := h.service.CreateDataset(request.Context(), input.appInput())
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, 201, value)
}
func (h *Handler) createDatasetVersion(response http.ResponseWriter, request *http.Request) {
	var input datasetRequest
	if err := decode(request, &input); err != nil {
		writeError(response, err)
		return
	}
	value, err := h.service.CreateDatasetVersion(request.Context(), request.PathValue("id"), input.appInput())
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, 201, value)
}

type agentRequest struct {
	Name              string   `json:"name"`
	Handle            string   `json:"handle"`
	RunnerType        string   `json:"runnerType"`
	Model             string   `json:"model"`
	ModelConnectionID string   `json:"modelConnectionId"`
	Preset            string   `json:"preset"`
	SystemPrompt      string   `json:"systemPrompt"`
	Description       string   `json:"description"`
	Tools             []string `json:"tools"`
	Skills            []string `json:"skills"`
}

func (input agentRequest) appInput() app.CreateAgentInput {
	return app.CreateAgentInput{Name: input.Name, Handle: input.Handle, RunnerType: input.RunnerType, Model: input.Model, ModelConnectionID: input.ModelConnectionID, Preset: input.Preset, SystemPrompt: input.SystemPrompt, Description: input.Description, Tools: input.Tools, Skills: input.Skills}
}

func (h *Handler) listModelConnections(response http.ResponseWriter, request *http.Request) {
	value, err := h.service.ListModelConnections(request.Context())
	writeResult(response, value, err, 200)
}
func (h *Handler) createModelConnection(response http.ResponseWriter, request *http.Request) {
	var input contract.ModelConnectionInput
	if err := decode(request, &input); err != nil {
		writeError(response, err)
		return
	}
	value, err := h.service.CreateModelConnection(request.Context(), input)
	writeResult(response, value, err, 201)
}
func (h *Handler) updateModelConnection(response http.ResponseWriter, request *http.Request) {
	var input contract.ModelConnectionInput
	if err := decode(request, &input); err != nil {
		writeError(response, err)
		return
	}
	value, err := h.service.UpdateModelConnection(request.Context(), request.PathValue("id"), input)
	writeResult(response, value, err, 200)
}
func (h *Handler) verifyModelConnection(response http.ResponseWriter, request *http.Request) {
	value, err := h.service.VerifyModelConnection(request.Context(), request.PathValue("id"))
	writeResult(response, value, err, 200)
}
func (h *Handler) deleteModelConnection(response http.ResponseWriter, request *http.Request) {
	if err := h.service.DeleteModelConnection(request.Context(), request.PathValue("id")); err != nil {
		writeError(response, err)
		return
	}
	response.WriteHeader(204)
}
func writeResult(response http.ResponseWriter, value any, err error, status int) {
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, status, value)
}
func (h *Handler) createAgent(response http.ResponseWriter, request *http.Request) {
	var input agentRequest
	if err := decode(request, &input); err != nil {
		writeError(response, err)
		return
	}
	value, err := h.service.CreateAgent(request.Context(), input.appInput())
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, 201, value)
}
func (h *Handler) createAgentVersion(response http.ResponseWriter, request *http.Request) {
	var input agentRequest
	if err := decode(request, &input); err != nil {
		writeError(response, err)
		return
	}
	value, err := h.service.CreateAgentVersion(request.Context(), request.PathValue("id"), input.appInput())
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, 201, value)
}
func (h *Handler) getRun(response http.ResponseWriter, request *http.Request) {
	value, err := h.service.GetRun(request.Context(), request.PathValue("id"))
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, 200, value)
}

func (h *Handler) runEvents(response http.ResponseWriter, request *http.Request) {
	flusher, ok := response.(http.Flusher)
	if !ok {
		writeError(response, &app.Error{Status: 500, Code: "stream_unsupported", Message: "当前 HTTP 服务不支持事件流"})
		return
	}
	afterText := request.Header.Get("Last-Event-ID")
	if query := request.URL.Query().Get("after"); query != "" {
		afterText = query
	}
	after, err := strconv.ParseInt(strings.TrimSpace(afterText), 10, 64)
	if err != nil && strings.TrimSpace(afterText) != "" {
		writeError(response, &app.Error{Status: 400, Code: "invalid_event_cursor", Message: "事件游标必须是整数"})
		return
	}
	response.Header().Set("Content-Type", "text/event-stream")
	response.Header().Set("Cache-Control", "no-cache, no-transform")
	response.Header().Set("Connection", "keep-alive")
	response.Header().Set("X-Accel-Buffering", "no")
	_, _ = io.WriteString(response, "retry: 1000\n\n")
	flusher.Flush()
	for {
		events, terminal, waitErr := h.service.WaitRunEvents(request.Context(), request.PathValue("id"), after, 15*time.Second)
		if waitErr != nil {
			if errors.Is(waitErr, request.Context().Err()) {
				return
			}
			payload, _ := json.Marshal(map[string]any{"error": waitErr.Error()})
			_, _ = response.Write([]byte("event: stream.error\ndata: " + string(payload) + "\n\n"))
			flusher.Flush()
			return
		}
		if len(events) == 0 {
			if terminal {
				return
			}
			_, _ = io.WriteString(response, ": keepalive\n\n")
			flusher.Flush()
			continue
		}
		for _, event := range events {
			payload, _ := json.Marshal(event)
			_, _ = response.Write([]byte("id: " + strconv.FormatInt(event.ID, 10) + "\nevent: " + event.Type + "\ndata: " + string(payload) + "\n\n"))
			after = event.ID
		}
		flusher.Flush()
	}
}

func (h *Handler) getArtifact(response http.ResponseWriter, request *http.Request) {
	value, err := h.service.ReadArtifact(request.Context(), request.PathValue("id"), request.URL.Query().Get("caseId"), request.URL.Query().Get("path"))
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, 200, value)
}
func (h *Handler) cancelRun(response http.ResponseWriter, request *http.Request) {
	value, err := h.service.CancelRun(request.Context(), request.PathValue("id"))
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, 200, value)
}
