package app

import (
	"context"
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
	Response domain.ServingResponse   `json:"response"`
	Input    string                   `json:"input"`
	ToolCall *domain.ResponseToolCall `json:"toolCall,omitempty"`
	Events   []domain.ResponseEvent   `json:"events"`
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
	return OperatorResponseDetail{Response: v, Input: v.Input, ToolCall: v.ToolCall, Events: events}, nil
}

// ReplayWebhookDelivery retains the existing event identity and requires a failed delivery.
func (s *Service) ReplayWebhookDelivery(ctx context.Context, endpointID domain.WebhookEndpointID, deliveryID domain.WebhookDeliveryID) error {
	return s.repo.ReplayWebhookDelivery(ctx, endpointID, deliveryID, s.now().UTC())
}
