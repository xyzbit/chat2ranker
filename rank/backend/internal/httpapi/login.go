package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/xyzbit/agentkiln/rank/backend/internal/access"
	"github.com/xyzbit/agentkiln/rank/backend/internal/app"
)

const sessionCookie = "rank_session"

func sessionID(r *http.Request) string {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func (h *Handler) sameOrigin(r *http.Request) bool {
	expected := h.managementOrigin
	if expected == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		expected = scheme + "://" + r.Host
	}
	return r.Header.Get("Origin") == expected && r.Header.Get("Sec-Fetch-Site") != "cross-site"
}

func (h *Handler) cookie(w http.ResponseWriter, r *http.Request, id string, expires time.Time) {
	maxAge := int(time.Until(expires).Seconds())
	if id == "" {
		maxAge = -1
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: id, Path: "/", HttpOnly: true, Secure: r.TLS != nil || strings.HasPrefix(h.managementOrigin, "https://"), SameSite: http.SameSiteStrictMode, Expires: expires, MaxAge: maxAge})
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !h.sameOrigin(r) {
		writeError(w, &app.Error{Status: 403, Code: "origin_denied", Message: "Login requires the same origin"})
		return
	}
	if h.accessFile == nil {
		h.identity(w, r)
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, &app.Error{Status: 415, Code: "invalid_login", Message: "Login requires JSON"})
		return
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeError(w, &app.Error{Status: 400, Code: "invalid_login", Message: "Invalid login request"})
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeError(w, &app.Error{Status: 400, Code: "invalid_login", Message: "Invalid login request"})
		return
	}
	var principal access.Principal
	authErr := access.ErrUnauthorized
	if input.Username != "" {
		principal, authErr = h.accessFile.Authenticate(input.Username, input.Password)
	}
	if authErr != nil {
		status, code := 503, "management_auth_unavailable"
		if errors.Is(authErr, access.ErrUnauthorized) {
			status, code = 401, "management_unauthorized"
		}
		writeError(w, &app.Error{Status: status, Code: code, Message: "Unable to sign in"})
		return
	}
	id, expires, err := h.sessions.Create(r.Context(), input.Username, input.Password, sessionID(r))
	if err != nil {
		writeError(w, &app.Error{Status: 503, Code: "management_auth_unavailable", Message: "Unable to create login session"})
		return
	}
	h.cookie(w, r, id, expires)
	writeJSON(w, 200, principal)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !h.sameOrigin(r) {
		writeError(w, &app.Error{Status: 403, Code: "origin_denied", Message: "Logout requires the same origin"})
		return
	}
	if err := h.sessions.Revoke(r.Context(), sessionID(r)); err != nil {
		writeError(w, &app.Error{Status: 503, Code: "management_auth_unavailable", Message: "Unable to revoke login session"})
		return
	}
	h.cookie(w, r, "", time.Unix(1, 0))
	w.WriteHeader(http.StatusNoContent)
}
