package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/xyzbit/agentkiln/rank/backend/internal/domain"
)

func (s *Service) operatorTenant(value string) string {
	if strings.TrimSpace(value) == "" {
		return s.tenantID
	}
	return strings.TrimSpace(value)
}

// ListReleaseHistory pages immutable revisions within one deployment alias.
func (s *Service) ListReleaseHistory(ctx context.Context, id domain.DeploymentID, alias string, before int64) (domain.ReleaseHistoryPage, error) {
	if before < 0 {
		return domain.ReleaseHistoryPage{}, problem(400, "invalid_cursor", "before must be a positive revision number")
	}
	v, err := s.repo.GetDeploymentAlias(ctx, id, alias)
	if err != nil {
		return domain.ReleaseHistoryPage{}, err
	}
	return s.repo.ListReleaseHistory(ctx, v.ID, before, 30)
}

// ListAgentEvaluations returns frozen Run summaries for one exact version.
func (s *Service) ListAgentEvaluations(ctx context.Context, id string) ([]domain.AgentEvaluationSummary, error) {
	if _, err := s.repo.GetAgentVersion(ctx, id); err != nil {
		return nil, err
	}
	return s.repo.ListAgentEvaluationSummaries(ctx, id)
}

// ListAPICredentials lists the selected tenant's redacted credentials.
func (s *Service) ListAPICredentials(ctx context.Context, tenant string) ([]domain.APICredential, error) {
	return s.repo.ListAPICredentials(ctx, s.operatorTenant(tenant))
}

// RevokeAPICredential prevents future authorization without cancelling accepted work.
func (s *Service) RevokeAPICredential(ctx context.Context, id domain.APICredentialID, tenant string) error {
	return s.repo.RevokeAPICredential(ctx, id, s.operatorTenant(tenant), s.now().UTC())
}

// ListOperatorResponses validates filters before querying bounded history.
func (s *Service) ListOperatorResponses(ctx context.Context, f domain.ResponseListFilter) (domain.ResponsePage, error) {
	f.TenantID = s.operatorTenant(f.TenantID)
	f.Query = strings.TrimSpace(f.Query)
	if len(f.Query) > 200 {
		return domain.ResponsePage{}, problem(400, "invalid_query", "Search must not exceed 200 characters")
	}
	if f.Limit == 0 {
		f.Limit = 30
	}
	if f.Limit < 1 || f.Limit > 100 {
		return domain.ResponsePage{}, problem(400, "invalid_limit", "limit must be between 1 and 100")
	}
	switch f.Status {
	case "", domain.ResponseQueued, domain.ResponseInProgress, domain.ResponseCompleted, domain.ResponseFailed, domain.ResponseCancelled, domain.ResponseRequiresAction:
	default:
		return domain.ResponsePage{}, problem(400, "invalid_status", "unknown Response status")
	}
	return s.repo.ListResponsesPage(ctx, f)
}

// OperatorResponseDetail exposes the persisted text and event history on demand.
// Binary attachments, provider credentials, and worker filesystem paths are excluded.
type OperatorResponseDetail struct {
	Response     domain.ServingResponse        `json:"response"`
	Input        string                        `json:"input"`
	ToolCall     *domain.ResponseToolCall      `json:"toolCall,omitempty"`
	Events       []domain.ResponseEvent        `json:"events"`
	Instructions string                        `json:"instructions"`
	CallerTools  []domain.ResponseFunctionTool `json:"callerTools"`
	InputItems   []domain.ResponseInputItem    `json:"inputItems"`
}

func (s *Service) GetOperatorResponse(ctx context.Context, id domain.ResponseID, tenant string) (OperatorResponseDetail, error) {
	v, err := s.repo.GetResponse(ctx, id)
	if err != nil {
		return OperatorResponseDetail{}, err
	}
	if v.TenantID != s.operatorTenant(tenant) {
		return OperatorResponseDetail{}, domain.ErrNotFound
	}
	events, err := s.repo.ListResponseEvents(ctx, id, 0)
	if err != nil {
		return OperatorResponseDetail{}, err
	}
	items := make([]domain.ResponseInputItem, len(v.InputItems))
	for i, item := range v.InputItems {
		item.Data = nil
		item.SourceDigest = ""
		items[i] = item
	}
	return OperatorResponseDetail{Response: v, Input: v.Input, ToolCall: v.ToolCall, Events: events, Instructions: v.Instructions, CallerTools: v.CallerTools, InputItems: items}, nil
}

// CaptureResponseCaseInput contains only text reviewed by the operator, never an implicit copy of output.
type CaptureResponseCaseInput struct {
	TenantID             string `json:"tenantId"`
	DatasetName          string `json:"datasetName"`
	BaseDatasetVersionID string `json:"baseDatasetVersionId"`
	Title                string `json:"title"`
	Input                string `json:"input"`
	Expected             string `json:"expected"`
	Reviewed             bool   `json:"reviewed"`
}

// CaptureResponseCase creates an immutable dataset version after explicit review.
// Existing datasets append to their current version; stale drafts and duplicate sources are rejected.
func (s *Service) CaptureResponseCase(ctx context.Context, id domain.ResponseID, input CaptureResponseCaseInput) (domain.DatasetVersion, error) {
	if !input.Reviewed || strings.TrimSpace(input.Input) == "" || strings.TrimSpace(input.Expected) == "" || strings.TrimSpace(input.Title) == "" {
		return domain.DatasetVersion{}, problem(400, "case_review_required", "Review the input, title and expected result before saving")
	}
	if len(input.Input) > 100000 || len(input.Expected) > 20000 || len(input.Title) > 200 {
		return domain.DatasetVersion{}, problem(400, "case_too_large", "Case text exceeds the allowed length")
	}
	if (strings.TrimSpace(input.DatasetName) == "") == (input.BaseDatasetVersionID == "") {
		return domain.DatasetVersion{}, problem(400, "invalid_dataset_target", "Choose an existing dataset or name a new dataset")
	}
	var created domain.DatasetVersion
	err := s.repo.WithinTx(ctx, func(ctx context.Context, repo domain.Repository) error {
		response, err := repo.GetResponse(ctx, id)
		if err != nil {
			return err
		}
		if response.TenantID != s.operatorTenant(input.TenantID) {
			return domain.ErrNotFound
		}
		if !domain.IsResponseTerminal(response.Status) {
			return problem(409, "response_not_finished", "Wait for this invocation to finish")
		}
		item := domain.Case{ID: "response-" + string(id), Title: strings.TrimSpace(input.Title), Input: strings.TrimSpace(input.Input), Expected: map[string]any{"summary": strings.TrimSpace(input.Expected)}, Source: &domain.CaseSource{ResponseID: id, DeploymentID: response.DeploymentID, AgentVersionID: response.AgentVersionID, CapturedAt: s.now().UTC()}}
		if input.BaseDatasetVersionID == "" {
			family, version, err := s.newDataset(CreateDatasetInput{Name: input.DatasetName, Source: "调用记录", Cases: []domain.Case{item}})
			if err != nil {
				return err
			}
			created = version
			return repo.CreateDatasetFamily(ctx, family, version)
		}
		base, err := repo.GetDatasetVersion(ctx, input.BaseDatasetVersionID)
		if err != nil {
			return err
		}
		versions, err := repo.ListDatasetVersions(ctx, base.FamilyID)
		if err != nil {
			return err
		}
		if len(versions) == 0 || versions[0].ID != base.ID {
			return problem(409, "dataset_version_changed", "Dataset changed; reload the latest version before adding this case")
		}
		if len(base.Cases) >= 200 {
			return problem(400, "dataset_full", "This dataset has 200 cases; choose another dataset")
		}
		for _, existing := range base.Cases {
			if existing.ID == item.ID || existing.Source != nil && existing.Source.ResponseID == id {
				return problem(409, "response_already_captured", "This invocation already exists in the dataset")
			}
		}
		created = base
		created.Version++
		created.ID = fmt.Sprintf("%s-v%d", base.FamilyID, created.Version)
		created.Cases = append(append([]domain.Case{}, base.Cases...), item)
		created.CaseCount = len(created.Cases)
		created.Source = "调用记录"
		created.CreatedAt = s.now().UTC()
		return repo.CreateDatasetVersion(ctx, created)
	})
	return created, err
}

// ReplayWebhookDelivery retains the existing event identity and requires a failed delivery.
func (s *Service) ReplayWebhookDelivery(ctx context.Context, endpointID domain.WebhookEndpointID, deliveryID domain.WebhookDeliveryID) error {
	return s.repo.ReplayWebhookDelivery(ctx, endpointID, deliveryID, s.now().UTC())
}
