package app

import (
	"context"
	"encoding/json"
	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/rank/backend/internal/domain"
	"strings"
)

// TrialResponseInput starts an exact-version preview or continues its frozen conversation.
// DeploymentReference is only accepted when starting a deployed preview.
type TrialResponseInput struct {
	AgentVersionID      string                `json:"agentVersionId"`
	DeploymentReference string                `json:"deploymentReference"`
	ConversationID      domain.ConversationID `json:"conversationId"`
	PreviousResponseID  string                `json:"previousResponseId"`
	Input               string                `json:"input"`
}

type TrialDetail struct {
	Conversation domain.Conversation `json:"conversation"`
	Agent        domain.AgentVersion `json:"agent"`
	Messages     []TrialMessage      `json:"messages"`
}

type TrialMessage struct {
	domain.ServingResponse
	Input string `json:"input"`
}

// CreateTrialResponse uses the same immutable Agent and execution lifecycle as serving.
// No Dataset, evaluation Run, public credential, or implicit deployment is created.
func (s *Service) CreateTrialResponse(ctx context.Context, input TrialResponseInput, key string) (domain.ServingResponse, bool, error) {
	var target domain.DeploymentResolution
	var err error
	input.AgentVersionID = strings.TrimSpace(input.AgentVersionID)
	input.DeploymentReference = strings.TrimSpace(input.DeploymentReference)
	if input.ConversationID != "" {
		conversation, err := s.trialConversation(ctx, input.ConversationID)
		if err != nil {
			return domain.ServingResponse{}, false, err
		}
		if input.AgentVersionID != "" || input.DeploymentReference != "" {
			return domain.ServingResponse{}, false, problem(400, "trial_target_frozen", "更换 Agent 或版本请新建试用会话")
		}
		if input.PreviousResponseID == "" {
			return domain.ServingResponse{}, false, problem(400, "trial_head_required", "续聊需要上一条回复")
		}
		previous, err := s.repo.GetResponse(ctx, domain.ResponseID(input.PreviousResponseID))
		if err != nil {
			return domain.ServingResponse{}, false, err
		}
		if previous.ConversationID != conversation.ID {
			return domain.ServingResponse{}, false, domain.ErrNotFound
		}
		target.AgentVersion, err = s.repo.GetAgentVersion(ctx, conversation.AgentVersionID)
		if err != nil {
			return domain.ServingResponse{}, false, err
		}
		target.Deployment.Policy = s.trialPolicy
		if conversation.DeploymentID != "" {
			target.Deployment, err = s.repo.GetDeployment(ctx, conversation.DeploymentID)
			if err != nil {
				return domain.ServingResponse{}, false, err
			}
		}
		target.RequestedRef = previous.RequestedReference
		target.Kind = domain.ResolutionConversation
	} else {
		if input.PreviousResponseID != "" || (input.AgentVersionID == "") == (input.DeploymentReference == "") {
			return domain.ServingResponse{}, false, problem(400, "trial_target_required", "请选择一个 Agent 版本或已发布的部署")
		}
		if input.DeploymentReference != "" {
			target, err = s.ResolveDeploymentReference(ctx, s.tenantID, input.DeploymentReference)
			if err != nil {
				return domain.ServingResponse{}, false, err
			}
			if target.Kind != domain.ResolutionAlias || target.ReleaseRevision == nil {
				return domain.ServingResponse{}, false, problem(400, "trial_release_required", "请选择已发布的部署别名")
			}
		} else {
			target.AgentVersion, err = s.repo.GetAgentVersion(ctx, input.AgentVersionID)
			if err != nil {
				return domain.ServingResponse{}, false, err
			}
			target.RequestedRef, target.Kind = input.AgentVersionID, domain.ResolutionExact
			target.Deployment.Policy = s.trialPolicy
		}
	}
	payload, _ := json.Marshal(input.Input)
	return s.createResponse(ctx, domain.APICredential{TenantID: s.tenantID}, CreateServingResponseInput{
		Model: target.RequestedRef, Input: payload, PreviousResponseID: input.PreviousResponseID,
		IdempotencyKey: key, Background: true,
	}, &target)
}

func (s *Service) trialConversation(ctx context.Context, id domain.ConversationID) (domain.Conversation, error) {
	value, err := s.repo.GetConversation(ctx, id)
	if err != nil {
		return value, err
	}
	if !value.Trial || value.TenantID != s.tenantID {
		return domain.Conversation{}, domain.ErrNotFound
	}
	return value, nil
}

func (s *Service) ListTrialConversations(ctx context.Context, filter domain.TrialConversationFilter) ([]domain.TrialConversation, error) {
	return s.repo.ListTrialConversations(ctx, s.tenantID, filter)
}

func (s *Service) GetTrialConversation(ctx context.Context, id domain.ConversationID) (TrialDetail, error) {
	value, err := s.trialConversation(ctx, id)
	if err != nil {
		return TrialDetail{}, err
	}
	agent, err := s.repo.GetAgentVersion(ctx, value.AgentVersionID)
	if err != nil {
		return TrialDetail{}, err
	}
	responses, err := s.repo.ListConversationResponses(ctx, id)
	if err != nil {
		return TrialDetail{}, err
	}
	items := make([]TrialMessage, 0, len(responses))
	for _, response := range responses {
		items = append(items, TrialMessage{ServingResponse: response, Input: response.Input})
	}
	return TrialDetail{Conversation: value, Agent: agent, Messages: items}, nil
}

func (s *Service) GetTrialResponse(ctx context.Context, id domain.ResponseID) (domain.ServingResponse, error) {
	return s.GetServingResponse(ctx, domain.APICredential{TenantID: s.tenantID}, id)
}

func (s *Service) CancelTrialResponse(ctx context.Context, id domain.ResponseID) (domain.ServingResponse, error) {
	return s.CancelServingResponse(ctx, domain.APICredential{TenantID: s.tenantID}, id)
}

// Trial progress replays from the last persisted execution sequence after restart.
// Only assistant stdout and lifecycle types cross into the conversation.
func (s *Service) trialProgress(ctx context.Context, response domain.ServingResponse) (int64, func(contract.Event) error, error) {
	if response.CredentialID != "" {
		return 0, nil, nil
	}
	after := response.ExecutionSequence
	return after, func(event contract.Event) error {
		err := s.repo.WithinTx(ctx, func(ctx context.Context, repo domain.Repository) error {
			current, err := repo.GetResponse(ctx, response.ID)
			if err != nil {
				return err
			}
			if domain.IsResponseTerminal(current.Status) || event.Sequence <= current.ExecutionSequence {
				return nil
			}
			before := current.ExecutionSequence
			current.ExecutionSequence = event.Sequence
			if event.Type == "harness.stdout" {
				current.OutputText += event.Message
			}
			if event.Type == "harness.output" {
				current.OutputText = event.Message
			}
			progress := responseEvent(current.ID, "trial.progress", map[string]any{
				"executionSequence": event.Sequence, "status": event.Status, "text": current.OutputText,
			}, event.At)
			return repo.UpdateResponseProgress(ctx, current, before, []domain.ResponseEvent{progress})
		})
		if err != nil {
			return err
		}
		s.publishResponse(response.ID)
		return nil
	}, nil
}
