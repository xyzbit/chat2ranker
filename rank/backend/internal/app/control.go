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

	"github.com/xyzbit/agentkiln/rank/backend/internal/domain"
)

const (
	ControlSelectDataset         = "select_dataset"
	ControlSelectAgent           = "select_agent"
	ControlCreateDataset         = "create_dataset"
	ControlAddDatasetCases       = "add_dataset_cases"
	ControlCreateDatasetVersion  = "create_dataset_version"
	ControlCreateAgent           = "create_agent"
	ControlCreateAgentVersion    = "create_agent_version"
	ControlPrepareRun            = "prepare_run"
	ControlPrepareRelease        = "prepare_release"
	ControlOpenTrial             = "open_trial"
	ControlOpenConfiguration     = "open_configuration"
	ControlCreateDeployment      = "create_deployment"
	ControlSetReleasePolicy      = "set_release_policy"
	ControlSetEvaluationDefaults = "set_evaluation_defaults"
	ControlReleaseStatus         = "release_status"
	ControlShowResults           = "show_experiment_results"
	ControlStartRun              = "start_run"
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
	err := s.repo.WithinTx(ctx, func(ctx context.Context, repo domain.Repository) error {
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
	case ControlSelectDataset, ControlSelectAgent, ControlCreateDataset, ControlAddDatasetCases, ControlCreateDatasetVersion, ControlCreateAgent, ControlCreateAgentVersion, ControlPrepareRun, ControlPrepareRelease, ControlOpenTrial, ControlOpenConfiguration, ControlShowResults, ControlStartRun, ControlCreateDeployment, ControlSetReleasePolicy, ControlSetEvaluationDefaults, ControlReleaseStatus:
	default:
		return domain.ControlCommand{}, problem(400, "unknown_control_command", "不支持的实验命令")
	}
	payload, err := normalizedPayload(input.Payload)
	if err != nil {
		return domain.ControlCommand{}, err
	}
	var preparedAgent CreateAgentInput
	var agentFamilyID string
	if input.Type == ControlCreateAgent || input.Type == ControlCreateAgentVersion {
		existing, loadErr := s.repo.GetControlCommandByIdempotencyKey(ctx, input.ExperimentID, input.IdempotencyKey)
		if loadErr == nil {
			if existing.Type != input.Type || existing.ControlSessionID != input.ControlSessionID || !bytes.Equal(existing.Payload, payload) {
				return domain.ControlCommand{}, problem(409, "idempotency_conflict", "同一幂等键已用于不同命令")
			}
			return existing, nil
		}
		if !errors.Is(loadErr, domain.ErrNotFound) {
			return domain.ControlCommand{}, loadErr
		}
		experiment, loadErr := s.repo.GetExperiment(ctx, input.ExperimentID)
		if loadErr != nil {
			return domain.ControlCommand{}, loadErr
		}
		if experiment.ControlSessionID != input.ControlSessionID {
			return domain.ControlCommand{}, problem(409, "control_session_mismatch", "Control Session 与实验绑定不一致")
		}
		preparedAgent, agentFamilyID, err = s.prepareControlAgent(ctx, input.Type, payload)
		if err != nil {
			return domain.ControlCommand{}, err
		}
	}
	var created domain.ControlCommand
	err = s.repo.WithinTx(ctx, func(ctx context.Context, repo domain.Repository) error {
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
			if err := applyDevelopmentDefault(ctx, repo, &experiment, agent); err != nil {
				return err
			}
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
		case ControlAddDatasetCases, ControlCreateDatasetVersion:
			var value struct {
				BaseDatasetVersionID string          `json:"baseDatasetVersionId"`
				Cases                []domain.Case   `json:"cases"`
				Description          *string         `json:"description"`
				Schema               json.RawMessage `json:"schema"`
				Rubric               json.RawMessage `json:"rubric"`
			}
			if err := json.Unmarshal(payload, &value); err != nil || strings.TrimSpace(value.BaseDatasetVersionID) == "" {
				return problem(400, "invalid_dataset_change", "基础测试集版本和新增用例不能为空")
			}
			base, getErr := repo.GetDatasetVersion(ctx, value.BaseDatasetVersionID)
			if getErr != nil {
				return mapNotFound(getErr, "dataset_not_found", "基础测试集版本不存在")
			}
			cases := value.Cases
			if input.Type == ControlAddDatasetCases {
				cases = append(append([]domain.Case{}, base.Cases...), value.Cases...)
			} else if cases == nil {
				cases = base.Cases
			}
			cases, normalizeErr := normalizeCases(cases)
			if normalizeErr != nil {
				return normalizeErr
			}
			versions, listErr := repo.ListDatasetVersions(ctx, base.FamilyID)
			if listErr != nil {
				return listErr
			}
			next := versions[0].Version + 1
			dataset := domain.DatasetVersion{ID: fmt.Sprintf("%s-v%d", base.FamilyID, next), FamilyID: base.FamilyID, Name: base.Name, Version: next, Source: "对话追加", Description: base.Description, Schema: base.Schema, Rubric: base.Rubric, Cases: cases, CreatedAt: now, CaseCount: len(cases)}
			if input.Type == ControlCreateDatasetVersion {
				dataset.Source = "对话更新"
				if value.Description != nil {
					dataset.Description = strings.TrimSpace(*value.Description)
				}
				if len(value.Schema) > 0 {
					dataset.Schema = value.Schema
				}
				if len(value.Rubric) > 0 {
					dataset.Rubric = value.Rubric
				}
			}
			dataset.Evaluator = evaluatorForDataset(dataset)
			if err := repo.CreateDatasetVersion(ctx, dataset); err != nil {
				return err
			}
			experiment.DatasetVersionID, experiment.UpdatedAt = dataset.ID, now
			if err := repo.UpdateExperimentControl(ctx, experiment); err != nil {
				return err
			}
			result["datasetVersionId"], result["label"], result["caseCount"] = dataset.ID, fmt.Sprintf("%s v%d", dataset.Name, dataset.Version), len(cases)
			if input.Type == ControlAddDatasetCases {
				result["added"] = len(value.Cases)
			}
		case ControlCreateAgent:
			family, agent, buildErr := s.newAgent(preparedAgent)
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
			agent, buildErr := s.createAgentVersion(ctx, repo, agentFamilyID, preparedAgent)
			if buildErr != nil {
				return buildErr
			}
			experiment.AgentVersionID, experiment.UpdatedAt = agent.ID, now
			if err := applyDevelopmentDefault(ctx, repo, &experiment, agent); err != nil {
				return err
			}
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
		case ControlReleaseStatus:
			tasks, err := repo.ListReleaseTasks(ctx, "", experiment.ID)
			if err != nil {
				return err
			}
			result["tasks"] = tasks
		case ControlCreateDeployment:
			var value struct {
				Name                   string `json:"name"`
				AgentFamilyID          string `json:"agentFamilyId"`
				RequestsPerMinute      *int   `json:"requestsPerMinute"`
				MaxConcurrentResponses *int   `json:"maxConcurrentResponses"`
				ExecutionTimeoutMs     *int64 `json:"executionTimeoutMs"`
			}
			if err := json.Unmarshal(payload, &value); err != nil {
				return problem(400, "invalid_deployment", "部署参数无效")
			}
			if value.AgentFamilyID == "" {
				agent, err := repo.GetAgentVersion(ctx, experiment.AgentVersionID)
				if err != nil {
					return err
				}
				value.AgentFamilyID = agent.FamilyID
			}
			policy := domain.DeploymentPolicy{RequestsPerMinute: 60, MaxConcurrentResponses: 4, ExecutionTimeoutMs: 120000}
			for _, field := range []struct {
				value  *int
				target *int
			}{
				{value.RequestsPerMinute, &policy.RequestsPerMinute},
				{value.MaxConcurrentResponses, &policy.MaxConcurrentResponses},
			} {
				if field.value != nil {
					*field.target = *field.value
				}
			}
			if value.ExecutionTimeoutMs != nil {
				policy.ExecutionTimeoutMs = *value.ExecutionTimeoutMs
			}
			deployment, err := s.createDeployment(ctx, repo, CreateDeploymentInput{TenantID: s.tenantID, AgentFamilyID: value.AgentFamilyID, Name: value.Name, Policy: policy})
			if err != nil {
				return err
			}
			result["deployment"] = deployment
		case ControlSetReleasePolicy:
			var value struct {
				DeploymentID        domain.DeploymentID `json:"deploymentId"`
				MinPassRate         *int                `json:"minPassRate"`
				MinValidTrialRate   *int                `json:"minValidTrialRate"`
				MaxEvidenceAgeHours *int                `json:"maxEvidenceAgeHours"`
			}
			if err := json.Unmarshal(payload, &value); err != nil {
				return problem(400, "invalid_release_policy", "发布门槛参数无效")
			}
			policy, err := repo.GetReleasePolicy(ctx, value.DeploymentID)
			if err != nil {
				return err
			}
			expected := policy.Revision
			if expected == 0 {
				policy.MinPassRate, policy.MinValidTrialRate, policy.MaxEvidenceAgeHours = 100, 100, 24
			}
			for _, field := range []struct {
				value  *int
				target *int
			}{
				{value.MinPassRate, &policy.MinPassRate}, {value.MinValidTrialRate, &policy.MinValidTrialRate}, {value.MaxEvidenceAgeHours, &policy.MaxEvidenceAgeHours},
			} {
				if field.value != nil {
					*field.target = *field.value
				}
			}
			policy, err = s.saveReleasePolicy(ctx, repo, value.DeploymentID, policy, expected)
			if err != nil {
				return err
			}
			result["deploymentId"], result["policy"] = value.DeploymentID, policy
		case ControlSetEvaluationDefaults:
			var value struct {
				AgentFamilyID        string  `json:"agentFamilyId"`
				DevelopmentDatasetID *string `json:"developmentDatasetId"`
				RegressionDatasetID  *string `json:"regressionDatasetId"`
			}
			if err := json.Unmarshal(payload, &value); err != nil {
				return problem(400, "invalid_defaults", "默认测试集参数无效")
			}
			if value.AgentFamilyID == "" {
				agent, err := repo.GetAgentVersion(ctx, experiment.AgentVersionID)
				if err != nil {
					return err
				}
				value.AgentFamilyID = agent.FamilyID
			}
			versions, err := repo.ListAgentVersions(ctx, value.AgentFamilyID)
			if err != nil {
				return err
			}
			if len(versions) == 0 {
				return domain.ErrNotFound
			}
			defaults, err := repo.GetAgentEvaluationDefaults(ctx, value.AgentFamilyID)
			if err != nil {
				return err
			}
			expected := defaults.Revision
			for _, field := range []struct {
				value  *string
				target *string
			}{{value.DevelopmentDatasetID, &defaults.DevelopmentDatasetID}, {value.RegressionDatasetID, &defaults.RegressionDatasetID}} {
				if field.value != nil {
					if _, err := latestDatasetVersion(ctx, repo, *field.value); err != nil {
						return err
					}
					*field.target = *field.value
				}
			}
			defaults.Revision++
			if err := repo.SaveAgentEvaluationDefaults(ctx, value.AgentFamilyID, defaults, expected); err != nil {
				return err
			}
			result["defaults"] = defaults
		case ControlOpenConfiguration:
			var value struct {
				Section      string              `json:"section"`
				DeploymentID domain.DeploymentID `json:"deploymentId,omitempty"`
				ConnectionID string              `json:"connectionId,omitempty"`
			}
			decoder := json.NewDecoder(bytes.NewReader(payload))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&value); err != nil {
				return problem(400, "invalid_configuration", "配置入口只接受类型与资源引用，请在安全表单中填写密钥")
			}
			switch value.Section {
			case "model_connections", "local_connections", "system_models", "preferences", "api_keys":
			case "webhooks", "release_policy", "endpoint_integration":
				if value.DeploymentID == "" {
					return problem(400, "endpoint_required", "请选择端点")
				}
			default:
				return problem(400, "invalid_configuration_section", "不支持的配置入口")
			}
			if value.ConnectionID != "" && value.Section != "model_connections" {
				return problem(400, "invalid_configuration_reference", "仅模型连接配置接受连接引用")
			}
			if value.DeploymentID != "" {
				deployment, err := repo.GetDeployment(ctx, value.DeploymentID)
				if err != nil {
					return mapNotFound(err, "endpoint_not_found", "端点不存在")
				}
				result["deploymentId"], result["deploymentName"] = deployment.ID, deployment.Name
			}
			result["section"], result["connectionId"] = value.Section, value.ConnectionID
		case ControlOpenTrial:
			var value struct {
				AgentVersionID string `json:"agentVersionId"`
				Draft          string `json:"draft"`
			}
			if err := json.Unmarshal(payload, &value); err != nil {
				return problem(400, "invalid_trial", "试用参数无效")
			}
			if value.AgentVersionID == "" {
				value.AgentVersionID = experiment.AgentVersionID
			}
			agent, err := repo.GetAgentVersion(ctx, value.AgentVersionID)
			if err != nil {
				return mapNotFound(err, "agent_not_found", "请选择 Agent 版本")
			}
			result["agentVersionId"], result["label"], result["draft"] = agent.ID, fmt.Sprintf("%s · v%d", agent.Name, agent.Version), value.Draft
		case ControlPrepareRelease:
			var value struct {
				DeploymentID                domain.DeploymentID `json:"deploymentId"`
				Alias                       string              `json:"alias"`
				AgentVersionID              string              `json:"agentVersionId"`
				DevelopmentDatasetVersionID *string             `json:"developmentDatasetVersionId"`
				RegressionDatasetVersionID  *string             `json:"regressionDatasetVersionId"`
			}
			if err := json.Unmarshal(payload, &value); err != nil {
				return problem(400, "invalid_release_options", "发布参数无效")
			}
			if value.AgentVersionID == "" {
				value.AgentVersionID = experiment.AgentVersionID
			}
			if value.AgentVersionID == "" {
				return problem(409, "experiment_agent_required", "请先选择 Agent 版本")
			}
			candidate, getErr := repo.GetAgentVersion(ctx, value.AgentVersionID)
			if getErr != nil {
				return mapNotFound(getErr, "agent_not_found", "Agent 版本不存在")
			}
			if value.DeploymentID == "" {
				deployments, listErr := repo.ListDeploymentsByAgentFamily(ctx, candidate.FamilyID)
				if listErr != nil {
					return listErr
				}
				if len(deployments) == 0 {
					return problem(409, "deployment_required", "当前 Agent 还没有部署，请提供部署名称并创建部署")
				}
				if len(deployments) > 1 {
					return problem(409, "deployment_choice_required", "当前 Agent 有多个部署，请选择发布目标")
				}
				value.DeploymentID = deployments[0].ID
			}
			deployment, getErr := repo.GetDeployment(ctx, value.DeploymentID)
			if getErr != nil {
				return mapNotFound(getErr, "deployment_not_found", "Deployment 不存在")
			}
			if deployment.AgentFamilyID != candidate.FamilyID {
				return problem(400, "release_agent_family_mismatch", "目标 Agent 版本不属于 Deployment 的 Agent 资产")
			}
			value.Alias = strings.TrimSpace(value.Alias)
			if value.Alias == "" {
				value.Alias = DeploymentAliasProduction
			}
			if value.Alias == DeploymentAliasLatest {
				return problem(400, "latest_alias_managed", "latest 为历史只读别名，请通过部署发布当前版本")
			}
			alias, getErr := repo.GetDeploymentAlias(ctx, deployment.ID, value.Alias)
			if getErr != nil {
				return mapNotFound(getErr, "deployment_alias_not_found", "Deployment 路由不存在")
			}
			result["deploymentId"] = deployment.ID
			result["deploymentName"] = deployment.Name
			result["alias"] = value.Alias
			result["candidateAgentVersionId"] = candidate.ID
			draft, err := s.releaseDraft(ctx, repo, ReleaseDraftInput{DeploymentID: deployment.ID, Alias: value.Alias, AgentVersionID: candidate.ID, DevelopmentDatasetVersionID: value.DevelopmentDatasetVersionID, RegressionDatasetVersionID: value.RegressionDatasetVersionID})
			if err != nil {
				return err
			}
			result["developmentDatasetVersionId"] = draft.DevelopmentDatasetVersionID
			result["regressionDatasetVersionId"] = draft.RegressionDatasetVersionID
			if alias.CurrentRelease != nil {
				result["currentAgentVersionId"] = alias.CurrentRelease.AgentVersionID
				result["currentReleaseRevisionId"] = alias.CurrentRelease.ID
			}
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
		if input.Type == ControlCreateDataset || input.Type == ControlAddDatasetCases || input.Type == ControlCreateDatasetVersion || input.Type == ControlCreateAgent || input.Type == ControlCreateAgentVersion || input.Type == ControlCreateDeployment || input.Type == ControlSetReleasePolicy || input.Type == ControlSetEvaluationDefaults || input.Type == ControlReleaseStatus {
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
	if err := s.repo.WithinTx(ctx, func(ctx context.Context, repo domain.Repository) error {
		return repo.AppendControlMessages(ctx, experimentID, clean...)
	}); err != nil {
		return domain.ExperimentView{}, err
	}
	return s.GetExperiment(ctx, experimentID)
}

// prepareControlAgent resolves mutable model defaults before the command's
// business transaction. The base Agent version is immutable.
func (s *Service) prepareControlAgent(ctx context.Context, command string, payload json.RawMessage) (CreateAgentInput, string, error) {
	var input CreateAgentInput
	var familyID string
	if command == ControlCreateAgentVersion {
		var reference struct {
			BaseAgentVersionID string `json:"baseAgentVersionId"`
		}
		if err := json.Unmarshal(payload, &reference); err != nil || strings.TrimSpace(reference.BaseAgentVersionID) == "" {
			return input, "", problem(400, "invalid_agent_change", "基础 Agent 版本不能为空")
		}
		base, err := s.repo.GetAgentVersion(ctx, reference.BaseAgentVersionID)
		if err != nil {
			return input, "", mapNotFound(err, "agent_not_found", "基础 Agent 版本不存在")
		}
		familyID = base.FamilyID
		input = CreateAgentInput{RunnerType: base.RunnerType, Model: base.Model, ModelConnectionID: base.ModelConnectionID, Preset: base.Preset, SystemPrompt: base.SystemPrompt, Description: base.Description, Tools: base.Tools, Skills: base.Skills}
	}
	if err := json.Unmarshal(payload, &input); err != nil {
		return input, familyID, problem(400, "invalid_agent", "Agent 参数无效")
	}
	input, err := s.resolveAgentConnection(ctx, input)
	return input, familyID, err
}
