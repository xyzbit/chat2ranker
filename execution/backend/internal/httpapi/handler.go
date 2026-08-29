package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/xyzbit/chat2ranker/execution/backend/contract"
	"github.com/xyzbit/chat2ranker/execution/backend/internal/app"
)

type Options struct{ ControlToken string }
type Handler struct {
	service      *app.Service
	controlToken string
}

func New(service *app.Service, options ...Options) http.Handler {
	configured := Options{ControlToken: "rank-local-control-token"}
	if len(options) > 0 {
		configured = options[0]
	}
	if configured.ControlToken == "" {
		configured.ControlToken = "rank-local-control-token"
	}
	h := &Handler{service: service, controlToken: configured.ControlToken}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", h.health)
	mux.HandleFunc("GET /v1/runtimes/{harness}", h.probe)
	mux.HandleFunc("GET /v1/model-catalog", h.listModelCatalog)
	mux.HandleFunc("GET /v1/model-connections", h.listModelConnections)
	mux.HandleFunc("POST /v1/model-connections", h.createModelConnection)
	mux.HandleFunc("GET /v1/model-connections/{id}", h.getModelConnection)
	mux.HandleFunc("PATCH /v1/model-connections/{id}", h.updateModelConnection)
	mux.HandleFunc("POST /v1/model-connections/{id}/verify", h.verifyModelConnection)
	mux.HandleFunc("DELETE /v1/model-connections/{id}", h.deleteModelConnection)
	mux.HandleFunc("GET /v1/system-model-bindings", h.listSystemModelBindings)
	mux.HandleFunc("GET /v1/system-model-bindings/{role}", h.getSystemModelBinding)
	mux.HandleFunc("PUT /v1/system-model-bindings/{role}", h.saveSystemModelBinding)
	mux.HandleFunc("GET /v1/internal/system-model-bindings/{role}/runtime", h.resolveSystemModel)
	mux.HandleFunc("POST /v1/executions", h.submit)
	mux.HandleFunc("GET /v1/executions/{id}", h.get)
	mux.HandleFunc("GET /v1/executions/{id}/events", h.events)
	mux.HandleFunc("POST /v1/executions/{id}/cancel", h.cancel)
	mux.HandleFunc("GET /v1/executions/{id}/artifacts", h.artifact)
	return recoverMiddleware(mux)
}

func (h *Handler) listSystemModelBindings(response http.ResponseWriter, request *http.Request) {
	value, err := h.service.ListSystemModelBindings(request.Context())
	writeValue(response, value, err, 200)
}
func (h *Handler) getSystemModelBinding(response http.ResponseWriter, request *http.Request) {
	value, err := h.service.GetSystemModelBinding(request.Context(), request.PathValue("role"))
	writeValue(response, value, err, 200)
}
func (h *Handler) saveSystemModelBinding(response http.ResponseWriter, request *http.Request) {
	var input contract.SystemModelBindingInput
	if err := decode(request, &input); err != nil {
		writeError(response, err)
		return
	}
	value, err := h.service.SaveSystemModelBinding(request.Context(), request.PathValue("role"), input)
	writeValue(response, value, err, 200)
}
func (h *Handler) resolveSystemModel(response http.ResponseWriter, request *http.Request) {
	if h.controlToken == "" || request.Header.Get("X-Rank-Control-Token") != h.controlToken {
		writeJSON(response, http.StatusUnauthorized, map[string]any{"error": map[string]any{"code": "unauthorized", "message": "invalid control token"}})
		return
	}
	value, err := h.service.ResolveSystemModel(request.Context(), request.PathValue("role"))
	writeValue(response, value, err, 200)
}

func (h *Handler) listModelCatalog(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, 200, h.service.ListModelCatalog())
}

func (h *Handler) listModelConnections(response http.ResponseWriter, request *http.Request) {
	value, err := h.service.ListModelConnections(request.Context())
	writeValue(response, value, err, 200)
}
func (h *Handler) getModelConnection(response http.ResponseWriter, request *http.Request) {
	value, err := h.service.GetModelConnection(request.Context(), request.PathValue("id"))
	writeValue(response, value, err, 200)
}
func (h *Handler) createModelConnection(response http.ResponseWriter, request *http.Request) {
	var input contract.ModelConnectionInput
	if err := decode(request, &input); err != nil {
		writeError(response, err)
		return
	}
	value, err := h.service.SaveModelConnection(request.Context(), "", input)
	writeValue(response, value, err, http.StatusCreated)
}
func (h *Handler) updateModelConnection(response http.ResponseWriter, request *http.Request) {
	var input contract.ModelConnectionInput
	if err := decode(request, &input); err != nil {
		writeError(response, err)
		return
	}
	value, err := h.service.SaveModelConnection(request.Context(), request.PathValue("id"), input)
	writeValue(response, value, err, 200)
}
func (h *Handler) verifyModelConnection(response http.ResponseWriter, request *http.Request) {
	value, err := h.service.VerifyModelConnection(request.Context(), request.PathValue("id"))
	writeValue(response, value, err, 200)
}
func (h *Handler) deleteModelConnection(response http.ResponseWriter, request *http.Request) {
	err := h.service.DeleteModelConnection(request.Context(), request.PathValue("id"))
	if err != nil {
		writeError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}
func writeValue(response http.ResponseWriter, value any, err error, status int) {
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, status, value)
}

func (h *Handler) health(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, 200, map[string]any{"ok": true, "service": "executiond", "storage": "repository"})
}

func (h *Handler) probe(response http.ResponseWriter, request *http.Request) {
	writeJSON(response, 200, h.service.Probe(request.Context(), request.PathValue("harness")))
}

func (h *Handler) submit(response http.ResponseWriter, request *http.Request) {
	var input contract.SubmitRequest
	if err := decode(request, &input); err != nil {
		writeError(response, err)
		return
	}
	value, err := h.service.Submit(request.Context(), input)
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusAccepted, value)
}

func (h *Handler) get(response http.ResponseWriter, request *http.Request) {
	value, err := h.service.Get(request.Context(), request.PathValue("id"))
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, 200, value)
}

func (h *Handler) events(response http.ResponseWriter, request *http.Request) {
	flusher, ok := response.(http.Flusher)
	if !ok {
		writeError(response, &app.Error{Status: 500, Code: "stream_unsupported", Message: "SSE is not supported"})
		return
	}
	afterText := request.Header.Get("Last-Event-ID")
	if query := request.URL.Query().Get("after"); query != "" {
		afterText = query
	}
	after, err := strconv.ParseInt(strings.TrimSpace(afterText), 10, 64)
	if err != nil && strings.TrimSpace(afterText) != "" {
		writeError(response, &app.Error{Status: 400, Code: "invalid_event_cursor", Message: "event cursor must be an integer"})
		return
	}
	response.Header().Set("Content-Type", "text/event-stream")
	response.Header().Set("Cache-Control", "no-cache, no-transform")
	response.Header().Set("Connection", "keep-alive")
	response.Header().Set("X-Accel-Buffering", "no")
	_, _ = io.WriteString(response, "retry: 1000\n\n")
	flusher.Flush()
	for {
		events, terminal, waitErr := h.service.WaitEvents(request.Context(), request.PathValue("id"), after, 15*time.Second)
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
			_, _ = response.Write([]byte("id: " + strconv.FormatInt(event.Sequence, 10) + "\nevent: " + event.Type + "\ndata: " + string(payload) + "\n\n"))
			after = event.Sequence
		}
		flusher.Flush()
	}
}

func (h *Handler) cancel(response http.ResponseWriter, request *http.Request) {
	value, err := h.service.Cancel(request.Context(), request.PathValue("id"))
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, 200, value)
}

func (h *Handler) artifact(response http.ResponseWriter, request *http.Request) {
	value, err := h.service.ReadArtifact(request.Context(), request.PathValue("id"), request.URL.Query().Get("path"))
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, 200, value)
}

func decode(request *http.Request, target any) error {
	err := json.NewDecoder(io.LimitReader(request.Body, 8<<20)).Decode(target)
	if err != nil && err != io.EOF {
		return &app.Error{Status: 400, Code: "invalid_json", Message: "invalid JSON request"}
	}
	return nil
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
	writeJSON(response, 500, map[string]any{"error": map[string]any{"code": "internal_error", "message": err.Error()}})
}

func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				writeJSON(response, 500, map[string]any{"error": map[string]any{"code": "internal_error", "message": "execution service error"}})
			}
		}()
		if strings.HasPrefix(request.URL.Path, "/v1/") {
			next.ServeHTTP(response, request)
			return
		}
		http.NotFound(response, request)
	})
}
