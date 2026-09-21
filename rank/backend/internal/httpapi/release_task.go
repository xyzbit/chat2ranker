package httpapi

import (
	"github.com/xyzbit/agentkiln/rank/backend/internal/app"
	"github.com/xyzbit/agentkiln/rank/backend/internal/domain"
	"net/http"
)

func (h *Handler) getEvaluationDefaults(w http.ResponseWriter, r *http.Request) {
	value, err := h.service.GetAgentEvaluationDefaults(r.Context(), r.PathValue("id"))
	writeResult(w, value, err, 200)
}
func (h *Handler) saveEvaluationDefaults(w http.ResponseWriter, r *http.Request) {
	var input struct {
		domain.AgentEvaluationDefaults
		ExpectedRevision int64 `json:"expectedRevision"`
	}
	if err := decode(r, &input); err != nil {
		writeError(w, err)
		return
	}
	value, err := h.service.SaveAgentEvaluationDefaults(r.Context(), r.PathValue("id"), input.AgentEvaluationDefaults, input.ExpectedRevision)
	writeResult(w, value, err, 200)
}
func (h *Handler) releaseDraft(w http.ResponseWriter, r *http.Request) {
	var input app.ReleaseDraftInput
	if err := decode(r, &input); err != nil {
		writeError(w, err)
		return
	}
	input.DeploymentID = domain.DeploymentID(r.PathValue("id"))
	value, err := h.service.PrepareReleaseDraft(r.Context(), input)
	writeResult(w, value, err, 200)
}
func (h *Handler) releaseComparison(w http.ResponseWriter, r *http.Request) {
	var input app.ReleaseDraftInput
	if err := decode(r, &input); err != nil {
		writeError(w, err)
		return
	}
	input.DeploymentID = domain.DeploymentID(r.PathValue("id"))
	value, err := h.service.StartReleaseComparison(r.Context(), input)
	writeResult(w, value, err, 202)
}
func (h *Handler) createReleaseTask(w http.ResponseWriter, r *http.Request) {
	var input app.StartReleaseTaskInput
	if err := decode(r, &input); err != nil {
		writeError(w, err)
		return
	}
	input.DeploymentID = domain.DeploymentID(r.PathValue("id"))
	input.Actor = requestActor(r, input.Actor)
	value, err := h.service.StartReleaseTask(r.Context(), input, r.Header.Get("Idempotency-Key"))
	writeResult(w, value, err, 202)
}
func (h *Handler) listReleaseTasks(w http.ResponseWriter, r *http.Request) {
	value, err := h.service.ListReleaseTasks(r.Context(), domain.DeploymentID(r.URL.Query().Get("deploymentId")), r.URL.Query().Get("experimentId"))
	writeResult(w, value, err, 200)
}
func (h *Handler) getReleaseTask(w http.ResponseWriter, r *http.Request) {
	value, err := h.service.GetReleaseTask(r.Context(), domain.ReleaseTaskID(r.PathValue("id")))
	writeResult(w, value, err, 200)
}
func (h *Handler) cancelReleaseTask(w http.ResponseWriter, r *http.Request) {
	value, err := h.service.CancelReleaseTask(r.Context(), domain.ReleaseTaskID(r.PathValue("id")))
	writeResult(w, value, err, 200)
}
func (h *Handler) overrideReleaseTask(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Reason string `json:"reason"`
	}
	if err := decode(r, &input); err != nil {
		writeError(w, err)
		return
	}
	value, err := h.service.OverrideReleaseTask(r.Context(), domain.ReleaseTaskID(r.PathValue("id")), input.Reason)
	writeResult(w, value, err, 200)
}
