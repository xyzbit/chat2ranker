package httpapi

import (
	"net/http"
	"strconv"

	"github.com/xyzbit/agentkiln/rank/backend/internal/app"
	"github.com/xyzbit/agentkiln/rank/backend/internal/domain"
)

func (h *Handler) listReleaseHistory(w http.ResponseWriter, r *http.Request) {
	var before int64
	if r.URL.Query().Has("before") {
		var err error
		before, err = strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
		if err != nil || before < 1 {
			writeError(w, &app.Error{Status: 400, Code: "invalid_cursor", Message: "before must be a positive revision number"})
			return
		}
	}
	v, err := h.service.ListReleaseHistory(r.Context(), domain.DeploymentID(r.PathValue("id")), r.PathValue("alias"), before)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, v)
}

func (h *Handler) listAgentEvaluations(w http.ResponseWriter, r *http.Request) {
	v, err := h.service.ListAgentEvaluations(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, v)
}

func (h *Handler) listServingCredentials(w http.ResponseWriter, r *http.Request) {
	v, err := h.service.ListAPICredentials(r.Context(), r.URL.Query().Get("tenantId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, v)
}
func (h *Handler) revokeServingCredential(w http.ResponseWriter, r *http.Request) {
	if err := h.service.RevokeAPICredential(r.Context(), domain.APICredentialID(r.PathValue("id")), r.URL.Query().Get("tenantId")); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"revoked": true})
}
func (h *Handler) listOperatorResponses(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 0
	if q.Has("limit") {
		var err error
		limit, err = strconv.Atoi(q.Get("limit"))
		if err != nil || limit < 1 {
			writeError(w, &app.Error{Status: 400, Code: "invalid_limit", Message: "limit must be a positive integer"})
			return
		}
	}
	v, err := h.service.ListOperatorResponses(r.Context(), domain.ResponseListFilter{TenantID: q.Get("tenantId"), DeploymentID: domain.DeploymentID(q.Get("deploymentId")), Status: q.Get("status"), Before: domain.ResponseID(q.Get("before")), Limit: limit})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, v)
}
func (h *Handler) getOperatorResponse(w http.ResponseWriter, r *http.Request) {
	v, err := h.service.GetOperatorResponse(r.Context(), domain.ResponseID(r.PathValue("id")), r.URL.Query().Get("tenantId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, v)
}
func (h *Handler) replayWebhookDelivery(w http.ResponseWriter, r *http.Request) {
	if err := h.service.ReplayWebhookDelivery(r.Context(), domain.WebhookEndpointID(r.PathValue("id")), domain.WebhookDeliveryID(r.PathValue("delivery"))); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 202, map[string]bool{"queued": true})
}
