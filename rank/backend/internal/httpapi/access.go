package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/rank/backend/internal/access"
	"github.com/xyzbit/agentkiln/rank/backend/internal/app"
)

type principalKey struct{}

func requestActor(r *http.Request, fallback string) string {
	if principal, ok := r.Context().Value(principalKey{}).(access.Principal); ok {
		return principal.Subject
	}
	return fallback
}

func (h *Handler) identity(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	principal, ok := r.Context().Value(principalKey{}).(access.Principal)
	if !ok {
		principal = access.Principal{Subject: "local", Role: "local-development"}
	}
	writeJSON(w, 200, principal)
}

func controlRoute(r *http.Request) bool {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if r.Method == "POST" {
		switch r.URL.Path {
		case "/api/internal/control/sessions/bind", "/api/internal/control/commands", "/api/internal/control/transcript", "/api/model-connections":
			return true
		}
	}
	if r.Method == "GET" {
		if r.URL.Path == "/api/bootstrap" {
			return true
		}
		if len(parts) == 3 && parts[0] == "api" && parts[1] == "experiments" && parts[2] != "" {
			return true
		}
		if len(parts) == 4 && parts[0] == "api" && parts[1] == "agent-families" && parts[2] != "" && parts[3] == "evaluation-defaults" {
			return true
		}
	}
	if r.Method == "PATCH" && len(parts) == 3 && parts[0] == "api" && parts[1] == "model-connections" && parts[2] != "" {
		return true
	}
	return r.Method == "PUT" && len(parts) == 3 && parts[0] == "api" && parts[1] == "system-models" && (parts[2] == "control" || parts[2] == "judge")
}

func (h *Handler) authorizeManagement(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/api/health" || strings.HasPrefix(r.URL.Path, "/v1/") {
			next.ServeHTTP(w, r)
			return
		}
		if r.Header.Get("X-Rank-Control-Token") != "" {
			if err := h.authorizeControl(r); err != nil {
				writeError(w, err)
				return
			}
			if !controlRoute(r) {
				writeError(w, &app.Error{Status: 403, Code: "service_scope_denied", Message: "Control service cannot access this operation"})
				return
			}
			principal := access.Principal{Subject: "rank-control", Role: "control-service"}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, principal)))
			return
		}
		if h.accessFile == nil {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method == "POST" && (r.URL.Path == "/api/auth/login" || r.URL.Path == "/api/auth/logout") {
			next.ServeHTTP(w, r)
			return
		}
		cookieAuth := r.Header.Get("Authorization") == "" && sessionID(r) != ""
		if r.Method != "GET" && r.Method != "HEAD" && (cookieAuth || r.Header.Get("Origin") != "") && !h.sameOrigin(r) {
			writeError(w, &app.Error{Status: 403, Code: "origin_denied", Message: "Management mutations require the same origin"})
			return
		}
		username, token, basic := r.BasicAuth()
		if !basic {
			fields := strings.Fields(r.Header.Get("Authorization"))
			if len(fields) == 2 && strings.EqualFold(fields[0], "Bearer") {
				token = fields[1]
			}
		} else if username == "" {
			token = ""
		}
		var principal access.Principal
		var err error
		if cookieAuth {
			principal, err = h.sessions.Authenticate(r.Context(), h.accessFile, sessionID(r))
		} else {
			principal, err = h.accessFile.Authenticate(username, token)
		}
		if err != nil {
			status, code, message := 503, "management_auth_unavailable", "Management authentication is unavailable"
			if errors.Is(err, access.ErrUnauthorized) {
				status, code, message = 401, "management_unauthorized", "Management credential required"
			}
			writeError(w, &app.Error{Status: status, Code: code, Message: message})
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, principal)))
	})
}

func allowControlConnectionInput(w http.ResponseWriter, r *http.Request, input contract.ModelConnectionInput) bool {
	principal, _ := r.Context().Value(principalKey{}).(access.Principal)
	if principal.Role == "control-service" && input.APIKey != "" {
		writeError(w, &app.Error{Status: 403, Code: "service_scope_denied", Message: "Model secrets require an operator form"})
		return false
	}
	return true
}
