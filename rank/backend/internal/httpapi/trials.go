package httpapi

import (
	"github.com/xyzbit/agentkiln/rank/backend/internal/app"
	"github.com/xyzbit/agentkiln/rank/backend/internal/domain"
	"net/http"
)

func (h *Handler) listTrials(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	value, err := h.service.ListTrialConversations(r.Context(), domain.TrialConversationFilter{
		Before: query.Get("before"), AgentFamilyID: query.Get("agentFamilyId"), AgentVersionID: query.Get("agentVersionId"), DirectOnly: query.Get("directOnly") == "true",
	})
	writeResult(w, value, err, http.StatusOK)
}

func (h *Handler) getTrial(w http.ResponseWriter, r *http.Request) {
	value, err := h.service.GetTrialConversation(r.Context(), domain.ConversationID(r.PathValue("id")))
	writeResult(w, value, err, http.StatusOK)
}

func (h *Handler) createTrialResponse(w http.ResponseWriter, r *http.Request) {
	var input app.TrialResponseInput
	if err := decodeStrict(r, &input); err != nil {
		writeError(w, err)
		return
	}
	value, replayed, err := h.service.CreateTrialResponse(r.Context(), input, r.Header.Get("Idempotency-Key"))
	if replayed {
		w.Header().Set("X-Idempotent-Replayed", "true")
	}
	writeResult(w, value, err, http.StatusCreated)
}

func (h *Handler) trialResponseEvents(w http.ResponseWriter, r *http.Request) {
	value, err := h.service.GetTrialResponse(r.Context(), domain.ResponseID(r.PathValue("id")))
	if err != nil {
		writeError(w, err)
		return
	}
	after, err := responseEventCursor(r)
	if err != nil {
		writeError(w, err)
		return
	}
	h.streamServingResponse(w, r, value.ID, after)
}

func (h *Handler) cancelTrialResponse(w http.ResponseWriter, r *http.Request) {
	value, err := h.service.CancelTrialResponse(r.Context(), domain.ResponseID(r.PathValue("id")))
	writeResult(w, value, err, http.StatusOK)
}
