package httpapi

import (
	"net/http"
	"strconv"

	"github.com/xyzbit/agentkiln/rank/backend/internal/app"
	"github.com/xyzbit/agentkiln/rank/backend/internal/domain"
)

func (h *Handler) getReleasePolicy(w http.ResponseWriter, r *http.Request) {
	value, err := h.service.GetReleasePolicy(r.Context(), domain.DeploymentID(r.PathValue("id")))
	writeResult(w, value, err, http.StatusOK)
}

func (h *Handler) saveReleasePolicy(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ExpectedRevision    int64  `json:"expectedRevision"`
		DatasetVersionID    string `json:"datasetVersionId"`
		MinPassRate         int    `json:"minPassRate"`
		MinValidTrialRate   int    `json:"minValidTrialRate"`
		MaxEvidenceAgeHours int    `json:"maxEvidenceAgeHours"`
	}
	if err := decode(r, &input); err != nil {
		writeError(w, err)
		return
	}
	value, err := h.service.SaveReleasePolicy(r.Context(), domain.DeploymentID(r.PathValue("id")), domain.ReleasePolicy{
		DatasetVersionID: input.DatasetVersionID, MinPassRate: input.MinPassRate,
		MinValidTrialRate: input.MinValidTrialRate, MaxEvidenceAgeHours: input.MaxEvidenceAgeHours,
	}, input.ExpectedRevision)
	writeResult(w, value, err, http.StatusOK)
}

func (h *Handler) preflightRelease(w http.ResponseWriter, r *http.Request) {
	var input struct {
		AgentVersionID string `json:"agentVersionId"`
		EvidenceRunID  string `json:"evidenceRunId"`
	}
	if err := decode(r, &input); err != nil {
		writeError(w, err)
		return
	}
	value, err := h.service.PreflightRelease(r.Context(), app.PromoteDeploymentInput{
		DeploymentID: domain.DeploymentID(r.PathValue("id")), Alias: r.PathValue("alias"),
		AgentVersionID: input.AgentVersionID, EvidenceRunID: input.EvidenceRunID,
	})
	writeResult(w, value, err, http.StatusOK)
}

func (h *Handler) listReleaseDecisions(w http.ResponseWriter, r *http.Request) {
	var before int64
	if raw := r.URL.Query().Get("before"); raw != "" {
		var err error
		before, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || before < 0 {
			writeError(w, &app.Error{Status: 400, Code: "invalid_cursor", Message: "无效游标"})
			return
		}
	}
	value, err := h.service.ListReleaseDecisions(r.Context(), domain.DeploymentID(r.PathValue("id")), r.PathValue("alias"), before)
	writeResult(w, value, err, http.StatusOK)
}
