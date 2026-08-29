package app

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/xyzbit/chat2ranker/rank/backend/internal/domain"
)

const (
	ControlSelectDataset      = "select_dataset"
	ControlSelectAgent        = "select_agent"
	ControlCreateDataset      = "create_dataset"
	ControlAddDatasetCases    = "add_dataset_cases"
	ControlCreateAgent        = "create_agent"
	ControlCreateAgentVersion = "create_agent_version"
	ControlPrepareRun         = "prepare_run"
	ControlShowResults        = "show_experiment_results"
	ControlStartRun           = "start_run"
)

type ControlCommandInput struct {
	ExperimentID     string
	ControlSessionID string
	IdempotencyKey   string
	Type             string
	Payload          json.RawMessage
}

func (s *Service) actionToken(experimentID, command string) string {
	mac := hmac.New(sha256.New, s.actionSecret)
	_, _ = mac.Write([]byte("v1\n" + experimentID + "\n" + command))
	return "v1." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *Service) AuthorizeAction(experimentID, command, token string) error {
	expected := s.actionToken(experimentID, command)
	if !hmac.Equal([]byte(expected), []byte(strings.TrimSpace(token))) {
		return problem(403, "invalid_action_token", "操作凭证无效，请刷新实验后重试")
	}
	return nil
}

func (s *Service) projectA2UI(experiment domain.Experiment) domain.A2UIProjection {
	actions := map[string]domain.A2UIAction{}
	for _, command := range []string{ControlSelectDataset, ControlSelectAgent, ControlPrepareRun, ControlStartRun} {
		actions[command] = domain.A2UIAction{Command: command, Token: s.actionToken(experiment.ID, command)}
	}
	return domain.A2UIProjection{Revision: fmt.Sprintf("%d", experiment.UpdatedAt.UnixNano()), Actions: actions}
}

func normalizedPayload(payload json.RawMessage) (json.RawMessage, error) {
	if len(bytes.TrimSpace(payload)) == 0 {
		return json.RawMessage(`{}`), nil
	}
	var value any
	if err := json.Unmarshal(payload, &value); err != nil {
		return nil, problem(400, "invalid_command_payload", "命令参数不是有效 JSON")
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

func (s *Service) BindControlSession(ctx context.Context, experimentID, sessionID string) (domain.ExperimentView, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return domain.ExperimentView{}, problem(400, "empty_control_session", "Control Session ID 不能为空")
	}
	err := s.repo.WithinTx(ctx, func(repo domain.Repository) error {
		return repo.BindControlSession(ctx, experimentID, sessionID, s.now().UTC())
	})
	if err != nil {
		return domain.ExperimentView{}, mapNotFound(err, "experiment_not_found", "实验不存在")
	}
	return s.GetExperiment(ctx, experimentID)
}

func (s *Service) ApplyControlCommand(ctx context.Context, input ControlCommandInput) (domain.ControlCommand, error) {
	input.ExperimentID = strings.TrimSpace(input.ExperimentID)
	input.ControlSessionID = strings.TrimSpace(input.ControlSessionID)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	input.Type = strings.TrimSpace(input.Type)
	if input.ExperimentID == "" || input.ControlSessionID == "" || input.IdempotencyKey == "" {
		return domain.ControlCommand{}, problem(400, "invalid_control_command", "实验、Session 和幂等键不能为空")
	}
	switch input.Type {
	case ControlSelectDataset, ControlSelectAgent, ControlCreateDataset, ControlAddDatasetCases, ControlCreateAgent, ControlCreateAgentVersion, ControlPrepareRun, ControlShowResults, ControlStartRun:
	default:
		return domain.ControlCommand{}, problem(400, "unknown_control_command", "不支持的实验命令")
	}
	payload, err := normalizedPayload(input.Payload)
	if err != nil {
		return domain.ControlCommand{}, err
	}
	var created domain.ControlCommand
	err = s.repo.WithinTx(ctx, func(repo domain.Repository) error {
		existing, existingErr := repo.GetControlCommandByIdempotencyKey(ctx, input.ExperimentID, input.IdempotencyKey)
		if existingErr == nil {
			if existing.Type != input.Type || existing.ControlSessionID != input.ControlSessionID || !bytes.Equal(existing.Payload, payload) {
				return problem(409, "idempotency_conflict", "同一幂等键已用于不同命令")
			}
			created = existing
			return nil
		}
		if !errors.Is(existingErr, domain.ErrNotFound) {
			return existingErr
		}
		experiment, getErr := repo.GetExperiment(ctx, input.ExperimentID)
		if getErr != nil {
			return mapNotFound(getErr, "experiment_not_found", "实验不存在")
		}
		if experiment.ControlSessionID != input.ControlSessionID {
			return problem(409, "control_session_mismatch", "Control Session 与实验绑定不一致")
		}
		now := s.now().UTC()
		result := map[string]any{"accepted": true, "command": input.Type}
		switch input.Type {
		case ControlSelectDataset:
			var value struct {
				DatasetVersionID string `json:"datasetVersionId"`
			}
			if err := json.Unmarshal(payload, &value); err != nil || strings.TrimSpace(value.DatasetVersionID) == "" {
				return problem(400, "dataset_required", "请选择测试集版本")
			}
			dataset, getErr := repo.GetDatasetVersion(ctx, value.DatasetVersionID)
			if getErr != nil {
				return mapNotFound(getErr, "dataset_not_found", "测试集版本不存在")
			}
			experiment.DatasetVersionID = dataset.ID
			experiment.UpdatedAt = now
			if err := repo.UpdateExperimentControl(ctx, experiment); err != nil {
				return err
			}
			result["datasetVersionId"] = dataset.ID
			result["label"] = fmt.Sprintf("%s v%d", dataset.Name, dataset.Version)
			result["caseCount"] = len(dataset.Cases)
		case ControlSelectAgent:
			var value struct {
				AgentVersionID string `json:"agentVersionId"`
			}
			if err := json.Unmarshal(payload, &value); err != nil || strings.TrimSpace(value.AgentVersionID) == "" {
				return problem(400, "agent_required", "请选择 Agent 版本")
			}
			agent, getErr := repo.GetAgentVersion(ctx, value.AgentVersionID)
			if getErr != nil {
				return mapNotFound(getErr, "agent_not_found", "Agent 版本不存在")
			}
			experiment.AgentVersionID = agent.ID
			experiment.UpdatedAt = now
			if err := repo.UpdateExperimentControl(ctx, experiment); err != nil {
				return err
			}
			result["agentVersionId"] = agent.ID
			result["label"] = fmt.Sprintf("%s v%d", agent.Handle, agent.Version)
		case ControlCreateDataset:
			var value struct {
				Name        string          `json:"name"`
				Source      string          `json:"source"`
				Description string          `json:"description"`
				Schema      json.RawMessage `json:"schema"`
				Rubric      json.RawMessage `json:"rubric"`
				Cases       []domain.Case   `json:"cases"`
			}
			if err := json.Unmarshal(payload, &value); err != nil {
				return problem(400, "invalid_dataset", "测试集参数无效")
			}
			family, dataset, buildErr := s.newDataset(CreateDatasetInput{Name: value.Name, Source: value.Source, Description: value.Description, Schema: value.Schema, Rubric: value.Rubric, Cases: value.Cases})
			if buildErr != nil {
				return buildErr
			}
			if err := repo.CreateDatasetFamily(ctx, family, dataset); err != nil {
				return err
			}
			experiment.DatasetVersionID = dataset.ID
			experiment.UpdatedAt = now
			if err := repo.UpdateExperimentControl(ctx, experiment); err != nil {
				return err
			}
			result["datasetVersionId"] = dataset.ID
			result["label"] = fmt.Sprintf("%s v%d", dataset.Name, dataset.Version)
			result["caseCount"] = len(dataset.Cases)
		case ControlAddDatasetCases:
			var value struct {
				BaseDatasetVersionID string        `json:"baseDatasetVersionId"`
				Cases                []domain.Case `json:"cases"`
			}
			if err := json.Unmarshal(payload, &value); err != nil || strings.TrimSpace(value.BaseDatasetVersionID) == "" {
				return problem(400, "invalid_dataset_change", "基础测试集版本和新增用例不能为空")
			}
			base, getErr := repo.GetDatasetVersion(ctx, value.BaseDatasetVersionID)
			if getErr != nil {
				return mapNotFound(getErr, "dataset_not_found", "基础测试集版本不存在")
			}
			cases, normalizeErr := normalizeCases(append(append([]domain.Case{}, base.Cases...), value.Cases...))
			if normalizeErr != nil {
				return normalizeErr
			}
			versions, listErr := repo.ListDatasetVersions(ctx, base.FamilyID)
			if listErr != nil {
				return listErr
			}
			next := versions[0].Version + 1
			dataset := domain.DatasetVersion{ID: fmt.Sprintf("%s-v%d", base.FamilyID, next), FamilyID: base.FamilyID, Name: base.Name, Version: next, Source: "对话追加", Description: base.Description, Schema: base.Schema, Rubric: base.Rubric, Cases: cases, CreatedAt: now, CaseCount: len(cases)}
			dataset.Evaluator = evaluatorForDataset(dataset)
			if err := repo.CreateDatasetVersion(ctx, dataset); err != nil {
				return err
			}
			experiment.DatasetVersionID, experiment.UpdatedAt = dataset.ID, now
			if err := repo.UpdateExperimentControl(ctx, experiment); err != nil {
				return err
			}
			result["datasetVersionId"], result["label"], result["caseCount"], result["added"] = dataset.ID, fmt.Sprintf("%s v%d", dataset.Name, dataset.Version), len(cases), len(value.Cases)
		case ControlCreateAgent:
			var value struct {
				Name              string   `json:"name"`
				Handle            string   `json:"handle"`
				RunnerType        string   `json:"runnerType"`
				Model             string   `json:"model"`
				ModelConnectionID string   `json:"modelConnectionId"`
				Preset            string   `json:"preset"`
				SystemPrompt      string   `json:"systemPrompt"`
				Description       string   `json:"description"`
				Tools             []string `json:"tools"`
				Skills            []string `json:"skills"`
			}
			if err := json.Unmarshal(payload, &value); err != nil {
				return problem(400, "invalid_agent", "Agent 参数无效")
			}
			family, agent, buildErr := s.newAgent(CreateAgentInput{Name: value.Name, Handle: value.Handle, RunnerType: value.RunnerType, Model: value.Model, ModelConnectionID: value.ModelConnectionID, Preset: value.Preset, SystemPrompt: value.SystemPrompt, Description: value.Description, Tools: value.Tools, Skills: value.Skills})
			if buildErr != nil {
				return buildErr
			}
			if err := repo.CreateAgentFamily(ctx, family, agent); err != nil {
				return err
			}
			experiment.AgentVersionID = agent.ID
			experiment.UpdatedAt = now
			if err := repo.UpdateExperimentControl(ctx, experiment); err != nil {
				return err
			}
			result["agentVersionId"] = agent.ID
			result["label"] = fmt.Sprintf("%s v%d", agent.Handle, agent.Version)
		case ControlCreateAgentVersion:
			var value struct {
				BaseAgentVersionID string    `json:"baseAgentVersionId"`
				RunnerType         *string   `json:"runnerType"`
				Model              *string   `json:"model"`
				ModelConnectionID  *string   `json:"modelConnectionId"`
				Preset             *string   `json:"preset"`
				SystemPrompt       *string   `json:"systemPrompt"`
				Description        *string   `json:"description"`
				Tools              *[]string `json:"tools"`
				Skills             *[]string `json:"skills"`
			}
			if err := json.Unmarshal(payload, &value); err != nil || strings.TrimSpace(value.BaseAgentVersionID) == "" {
				return problem(400, "invalid_agent_change", "基础 Agent 版本不能为空")
			}
			base, getErr := repo.GetAgentVersion(ctx, value.BaseAgentVersionID)
			if getErr != nil {
				return mapNotFound(getErr, "agent_not_found", "基础 Agent 版本不存在")
			}
			input := CreateAgentInput{RunnerType: base.RunnerType, Model: base.Model, ModelConnectionID: base.ModelConnectionID, Preset: base.Preset, SystemPrompt: base.SystemPrompt, Description: base.Description, Tools: base.Tools, Skills: base.Skills}
			if value.RunnerType != nil {
				input.RunnerType = *value.RunnerType
			}
			if value.Model != nil {
				input.Model = *value.Model
			}
			if value.ModelConnectionID != nil {
				input.ModelConnectionID = *value.ModelConnectionID
			}
			if value.Preset != nil {
				input.Preset = *value.Preset
			}
			if value.SystemPrompt != nil {
				input.SystemPrompt = *value.SystemPrompt
			}
			if value.Description != nil {
				input.Description = *value.Description
			}
			if value.Tools != nil {
				input.Tools = *value.Tools
			}
			if value.Skills != nil {
				input.Skills = *value.Skills
			}
			agent, buildErr := s.createAgentVersion(ctx, repo, base.FamilyID, input)
			if buildErr != nil {
				return buildErr
			}
			experiment.AgentVersionID, experiment.UpdatedAt = agent.ID, now
			if err := repo.UpdateExperimentControl(ctx, experiment); err != nil {
				return err
			}
			result["agentVersionId"], result["label"] = agent.ID, fmt.Sprintf("%s v%d", agent.Handle, agent.Version)
		case ControlPrepareRun, ControlStartRun:
			if experiment.DatasetVersionID == "" || experiment.AgentVersionID == "" {
				return problem(409, "experiment_not_ready", "请先选择测试集和 Agent")
			}
			var value struct {
				TrialCount      int      `json:"trialCount"`
				AgentVersionIDs []string `json:"agentVersionIds"`
			}
			if err := json.Unmarshal(payload, &value); err != nil {
				return problem(400, "invalid_run_options", "运行参数无效")
			}
			trialCount, normalizeErr := normalizeTrialCount(value.TrialCount)
			if normalizeErr != nil {
				return normalizeErr
			}
			result["datasetVersionId"] = experiment.DatasetVersionID
			result["agentVersionId"] = experiment.AgentVersionID
			if len(value.AgentVersionIDs) > 1 {
				result["agentVersionIds"] = uniqueStrings(value.AgentVersionIDs)
			}
			dataset, datasetErr := repo.GetDatasetVersion(ctx, experiment.DatasetVersionID)
			if datasetErr != nil {
				return datasetErr
			}
			agent, agentErr := repo.GetAgentVersion(ctx, experiment.AgentVersionID)
			if agentErr != nil {
				return agentErr
			}
			result["dataset"] = map[string]any{"id": dataset.ID, "name": dataset.Name, "version": dataset.Version, "caseCount": len(dataset.Cases)}
			result["agent"] = map[string]any{"id": agent.ID, "handle": agent.Handle, "version": agent.Version, "runnerType": agent.RunnerType}
			result["trialCount"] = trialCount
			result["state"] = "confirmation_required"
		case ControlShowResults:
			runs, listErr := repo.ListRunsByExperiment(ctx, experiment.ID)
			if listErr != nil {
				return listErr
			}
			items := make([]map[string]any, 0, len(runs))
			for _, run := range runs {
				items = append(items, map[string]any{"id": run.ID, "status": run.Status, "agent": run.AgentSnapshot.Handle, "agentVersion": run.AgentSnapshot.Version, "passRate": run.PassRate, "cost": run.Cost, "costKnown": run.CostKnown, "durationMs": run.DurationMs, "createdAt": run.CreatedAt})
			}
			result["runCount"] = len(items)
			result["runs"] = items
		}
		resultJSON, marshalErr := json.Marshal(result)
		if marshalErr != nil {
			return marshalErr
		}
		created = domain.ControlCommand{ID: newID("cmd"), ExperimentID: input.ExperimentID, ControlSessionID: input.ControlSessionID, IdempotencyKey: input.IdempotencyKey, Type: input.Type, Payload: payload, Result: resultJSON, CreatedAt: now}
		eventType := "a2ui/" + input.Type
		if input.Type == ControlCreateDataset || input.Type == ControlAddDatasetCases || input.Type == ControlCreateAgent || input.Type == ControlCreateAgentVersion {
			eventType = "control/" + input.Type
		}
		event := domain.ControlEvent{ExperimentID: input.ExperimentID, ControlSessionID: input.ControlSessionID, CommandID: created.ID, Type: eventType, Payload: resultJSON, CreatedAt: now}
		return repo.CreateControlCommand(ctx, created, event)
	})
	return created, err
}

func (s *Service) AppendControlMessages(ctx context.Context, experimentID, controlSessionID string, messages []domain.Message) (domain.ExperimentView, error) {
	experiment, err := s.repo.GetExperiment(ctx, experimentID)
	if err != nil {
		return domain.ExperimentView{}, mapNotFound(err, "experiment_not_found", "实验不存在")
	}
	if experiment.ControlSessionID != controlSessionID {
		return domain.ExperimentView{}, problem(409, "control_session_mismatch", "Control Session 与实验绑定不一致")
	}
	clean := make([]domain.Message, 0, len(messages))
	for _, message := range messages {
		message.ID = strings.TrimSpace(message.ID)
		message.Content = strings.TrimSpace(message.Content)
		if message.ID == "" || message.Content == "" || (message.Role != "user" && message.Role != "assistant") {
			return domain.ExperimentView{}, problem(400, "invalid_control_transcript", "DSH transcript 消息无效")
		}
		message.ExperimentID = experimentID
		if message.CreatedAt.IsZero() {
			message.CreatedAt = s.now().UTC()
		}
		clean = append(clean, message)
	}
	if err := s.repo.WithinTx(ctx, func(repo domain.Repository) error {
		return repo.AppendControlMessages(ctx, experimentID, clean...)
	}); err != nil {
		return domain.ExperimentView{}, err
	}
	return s.GetExperiment(ctx, experimentID)
}
