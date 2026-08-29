package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/xyzbit/chat2ranker/execution/backend/contract"
	"github.com/xyzbit/chat2ranker/rank/backend/internal/domain"
)

type Error struct {
	Status  int
	Code    string
	Message string
}

func (err *Error) Error() string { return err.Message }

func problem(status int, code, message string) error {
	return &Error{Status: status, Code: code, Message: message}
}

type Options struct {
	Runners       RunnerRegistry
	Artifacts     ArtifactReader
	Workers       bool
	Clock         func() time.Time
	WorkerLatency time.Duration
	ActionSecret  string
	JudgeHarness  string
	JudgeModel    string
	Connections   ModelConnectionClient
}

type ModelConnectionClient interface {
	ListModelCatalog(context.Context) ([]contract.ModelProvider, error)
	ListModelConnections(context.Context) ([]contract.ModelConnection, error)
	GetModelConnection(context.Context, string) (contract.ModelConnection, error)
	CreateModelConnection(context.Context, contract.ModelConnectionInput) (contract.ModelConnection, error)
	UpdateModelConnection(context.Context, string, contract.ModelConnectionInput) (contract.ModelConnection, error)
	VerifyModelConnection(context.Context, string) (contract.ModelConnection, error)
	DeleteModelConnection(context.Context, string) error
	ListSystemModelBindings(context.Context) ([]contract.SystemModelBinding, error)
	GetSystemModelBinding(context.Context, string) (contract.SystemModelBinding, error)
	SaveSystemModelBinding(context.Context, string, contract.SystemModelBindingInput) (contract.SystemModelBinding, error)
}

type Service struct {
	repo          domain.Repository
	runners       RunnerRegistry
	workers       bool
	now           func() time.Time
	workerLatency time.Duration
	actionSecret  []byte
	artifacts     ArtifactReader
	judgeHarness  string
	judgeModel    string
	connections   ModelConnectionClient
	mu            sync.Mutex
	cancels       map[string]context.CancelFunc
}

func NewService(repo domain.Repository, options Options) *Service {
	if options.Runners == nil {
		options.Runners = DefaultRunners()
	}
	if options.Clock == nil {
		options.Clock = time.Now
	}
	if options.WorkerLatency <= 0 {
		options.WorkerLatency = 60 * time.Millisecond
	}
	if strings.TrimSpace(options.ActionSecret) == "" {
		options.ActionSecret = "rank-local-action-secret"
	}
	if strings.TrimSpace(options.JudgeHarness) == "" {
		options.JudgeHarness = "dsh"
	}
	return &Service{repo: repo, runners: options.Runners, artifacts: options.Artifacts, connections: options.Connections, workers: options.Workers, now: options.Clock, workerLatency: options.WorkerLatency, actionSecret: []byte(options.ActionSecret), judgeHarness: options.JudgeHarness, judgeModel: options.JudgeModel, cancels: map[string]context.CancelFunc{}}
}

func newID(prefix string) string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		panic(err)
	}
	return prefix + "-" + hex.EncodeToString(value)
}

func (s *Service) Bootstrap(ctx context.Context) (domain.Bootstrap, error) {
	datasetFamilies, err := s.repo.ListDatasetFamilies(ctx)
	if err != nil {
		return domain.Bootstrap{}, err
	}
	datasets := make([]domain.DatasetAsset, 0, len(datasetFamilies))
	for _, family := range datasetFamilies {
		versions, err := s.repo.ListDatasetVersions(ctx, family.ID)
		if err != nil {
			return domain.Bootstrap{}, err
		}
		if len(versions) == 0 {
			continue
		}
		latest := versions[0]
		for _, version := range versions {
			if version.ID == family.LatestVersionID {
				latest = version
				break
			}
		}
		datasets = append(datasets, domain.DatasetAsset{DatasetVersion: latest, FamilyDescription: family.Description, VersionCount: len(versions), Versions: versions})
	}
	agentFamilies, err := s.repo.ListAgentFamilies(ctx)
	if err != nil {
		return domain.Bootstrap{}, err
	}
	agents := make([]domain.AgentAsset, 0, len(agentFamilies))
	for _, family := range agentFamilies {
		versions, err := s.repo.ListAgentVersions(ctx, family.ID)
		if err != nil {
			return domain.Bootstrap{}, err
		}
		if len(versions) == 0 {
			continue
		}
		views := make([]domain.AgentVersionView, 0, len(versions))
		for _, version := range versions {
			views = append(views, domain.AgentVersionView{AgentVersion: version, Runtime: s.probe(ctx, version)})
		}
		latest := views[0]
		for _, version := range views {
			if version.ID == family.LatestVersionID {
				latest = version
				break
			}
		}
		agents = append(agents, domain.AgentAsset{AgentVersionView: latest, FamilyDescription: family.Description, VersionCount: len(views), Versions: views})
	}
	experiments, err := s.repo.ListExperiments(ctx)
	if err != nil {
		return domain.Bootstrap{}, err
	}
	connections := []contract.ModelConnection{}
	catalog := []contract.ModelProvider{}
	systemModels := []contract.SystemModelBinding{}
	if s.connections != nil {
		connections, _ = s.connections.ListModelConnections(ctx)
		catalog, _ = s.connections.ListModelCatalog(ctx)
		systemModels, _ = s.connections.ListSystemModelBindings(ctx)
	}
	runtimes := map[string]domain.RuntimeAvailability{}
	for name, runner := range s.runners {
		runtimes[name] = runner.Probe(ctx, domain.AgentVersion{RunnerType: name})
	}
	return domain.Bootstrap{Datasets: datasets, Agents: agents, Experiments: experiments, ModelConnections: connections, ModelCatalog: catalog, SystemModels: systemModels, Runtimes: runtimes}, nil
}

func (s *Service) SaveSystemModel(ctx context.Context, role string, input contract.SystemModelBindingInput) (contract.SystemModelBinding, error) {
	if s.connections == nil {
		return contract.SystemModelBinding{}, problem(503, "execution_service_unavailable", "Execution Service 未配置")
	}
	return s.connections.SaveSystemModelBinding(ctx, role, input)
}

func (s *Service) probe(ctx context.Context, agent domain.AgentVersion) domain.RuntimeAvailability {
	runner := s.runners[agent.RunnerType]
	if runner == nil {
		return domain.RuntimeAvailability{Available: false, Reason: "未知 Runner：" + agent.RunnerType}
	}
	return runner.Probe(ctx, agent)
}

type CreateDatasetInput struct {
	Name, Source, Description string
	Schema, Rubric            json.RawMessage
	Cases                     []domain.Case
}

func normalizeCases(cases []domain.Case) ([]domain.Case, error) {
	if len(cases) == 0 {
		return nil, problem(400, "invalid_cases", "测试集至少需要一个用例")
	}
	if len(cases) > 200 {
		cases = cases[:200]
	}
	result := make([]domain.Case, len(cases))
	seenIDs := map[string]bool{}
	for index, item := range cases {
		item.Input = strings.TrimSpace(item.Input)
		if item.Input == "" {
			return nil, problem(400, "invalid_case", fmt.Sprintf("第 %d 条用例缺少输入", index+1))
		}
		if item.ID == "" {
			item.ID = fmt.Sprintf("case-%03d", index+1)
		}
		if seenIDs[item.ID] {
			return nil, problem(400, "duplicate_case_id", fmt.Sprintf("第 %d 条用例的 ID 重复：%s", index+1, item.ID))
		}
		seenIDs[item.ID] = true
		if item.Title == "" {
			item.Title = fmt.Sprintf("用例 %d", index+1)
		}
		if item.Expected == nil {
			item.Expected = map[string]any{"summary": "任务成功完成"}
		}
		result[index] = item
	}
	return result, nil
}

func (s *Service) CreateDataset(ctx context.Context, input CreateDatasetInput) (domain.DatasetVersion, error) {
	family, version, err := s.newDataset(input)
	if err != nil {
		return domain.DatasetVersion{}, err
	}
	err = s.repo.WithinTx(ctx, func(repo domain.Repository) error { return repo.CreateDatasetFamily(ctx, family, version) })
	return version, err
}

func (s *Service) newDataset(input CreateDatasetInput) (domain.DatasetFamily, domain.DatasetVersion, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return domain.DatasetFamily{}, domain.DatasetVersion{}, problem(400, "invalid_name", "请输入测试集名称")
	}
	cases, err := normalizeCases(input.Cases)
	if err != nil {
		return domain.DatasetFamily{}, domain.DatasetVersion{}, err
	}
	now, familyID := s.now().UTC(), newID("dataset")
	description := strings.TrimSpace(input.Description)
	if description == "" {
		description = "从 " + input.Source + " 创建"
	}
	family := domain.DatasetFamily{ID: familyID, Name: name, Description: description, LatestVersionID: familyID + "-v1", CreatedAt: now}
	version := domain.DatasetVersion{ID: family.LatestVersionID, FamilyID: familyID, Name: name, Version: 1, Source: input.Source, Description: description, Schema: defaultJSON(input.Schema), Rubric: defaultJSON(input.Rubric), Cases: cases, CreatedAt: now, CaseCount: len(cases)}
	version.Evaluator = evaluatorForDataset(version)
	return family, version, nil
}

func defaultJSON(value json.RawMessage) json.RawMessage {
	if len(value) == 0 {
		return json.RawMessage(`{}`)
	}
	return value
}

func (s *Service) CreateDatasetVersion(ctx context.Context, familyID string, input CreateDatasetInput) (domain.DatasetVersion, error) {
	cases, err := normalizeCases(input.Cases)
	if err != nil {
		return domain.DatasetVersion{}, err
	}
	var created domain.DatasetVersion
	err = s.repo.WithinTx(ctx, func(repo domain.Repository) error {
		families, e := repo.ListDatasetFamilies(ctx)
		if e != nil {
			return e
		}
		var family *domain.DatasetFamily
		for index := range families {
			if families[index].ID == familyID {
				family = &families[index]
				break
			}
		}
		if family == nil {
			return problem(404, "dataset_family_not_found", "测试集资产不存在")
		}
		versions, e := repo.ListDatasetVersions(ctx, familyID)
		if e != nil {
			return e
		}
		next := 1
		if len(versions) > 0 {
			next = versions[0].Version + 1
		}
		description := strings.TrimSpace(input.Description)
		if description == "" {
			description = "从 " + input.Source + " 创建"
		}
		created = domain.DatasetVersion{ID: fmt.Sprintf("%s-v%d", familyID, next), FamilyID: familyID, Name: family.Name, Version: next, Source: input.Source, Description: description, Schema: defaultJSON(input.Schema), Rubric: defaultJSON(input.Rubric), Cases: cases, CreatedAt: s.now().UTC(), CaseCount: len(cases)}
		created.Evaluator = evaluatorForDataset(created)
		return repo.CreateDatasetVersion(ctx, created)
	})
	return created, err
}

type CreateAgentInput struct {
	Name, Handle, RunnerType, Model, ModelConnectionID, Preset, SystemPrompt, Description string
	Tools, Skills                                                                         []string
}

func normalizeTools(tools []string) []string {
	result := []string{}
	seen := map[string]bool{}
	for _, tool := range tools {
		tool = strings.TrimSpace(tool)
		if tool != "" && !seen[tool] {
			seen[tool] = true
			result = append(result, tool)
		}
	}
	return result
}

func (s *Service) CreateAgent(ctx context.Context, input CreateAgentInput) (domain.AgentVersionView, error) {
	if err := s.validateAgentConnection(ctx, input); err != nil {
		return domain.AgentVersionView{}, err
	}
	family, version, err := s.newAgent(input)
	if err != nil {
		return domain.AgentVersionView{}, err
	}
	err = s.repo.WithinTx(ctx, func(repo domain.Repository) error { return repo.CreateAgentFamily(ctx, family, version) })
	return domain.AgentVersionView{AgentVersion: version, Runtime: s.probe(ctx, version)}, err
}

func (s *Service) newAgent(input CreateAgentInput) (domain.AgentFamily, domain.AgentVersion, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return domain.AgentFamily{}, domain.AgentVersion{}, problem(400, "invalid_name", "请输入 Agent 名称")
	}
	if input.RunnerType == "" {
		input.RunnerType = "dsh"
	}
	if s.runners[input.RunnerType] == nil {
		return domain.AgentFamily{}, domain.AgentVersion{}, problem(400, "unknown_runner", "未知 Runner："+input.RunnerType)
	}
	now, familyID := s.now().UTC(), newID("agent")
	handle := strings.TrimSpace(input.Handle)
	if handle == "" {
		handle = "@" + input.RunnerType + "/" + strings.ToLower(strings.ReplaceAll(name, " ", "-"))
	}
	if !strings.HasPrefix(handle, "@") {
		handle = "@" + handle
	}
	if input.Model == "" {
		input.Model = "由运行时决定"
	}
	if input.Description == "" {
		input.Description = "自定义 Agent 配置"
	}
	family := domain.AgentFamily{ID: familyID, Name: name, Handle: handle, Description: input.Description, LatestVersionID: familyID + "-v1", CreatedAt: now}
	version := domain.AgentVersion{ID: family.LatestVersionID, FamilyID: familyID, Name: name, Handle: handle, Version: 1, RunnerType: input.RunnerType, Description: input.Description, Model: input.Model, ModelConnectionID: input.ModelConnectionID, Preset: agentPreset(input.RunnerType, input.Preset), SystemPrompt: strings.TrimSpace(input.SystemPrompt), Tools: normalizeTools(input.Tools), Skills: normalizeTools(input.Skills), CreatedAt: now}
	return family, version, nil
}

func (s *Service) CreateAgentVersion(ctx context.Context, familyID string, input CreateAgentInput) (domain.AgentVersionView, error) {
	if err := s.validateAgentConnection(ctx, input); err != nil {
		return domain.AgentVersionView{}, err
	}
	var created domain.AgentVersion
	err := s.repo.WithinTx(ctx, func(repo domain.Repository) error {
		var err error
		created, err = s.createAgentVersion(ctx, repo, familyID, input)
		return err
	})
	return domain.AgentVersionView{AgentVersion: created, Runtime: s.probe(ctx, created)}, err
}

func (s *Service) createAgentVersion(ctx context.Context, repo domain.Repository, familyID string, input CreateAgentInput) (domain.AgentVersion, error) {
	if input.RunnerType == "" {
		input.RunnerType = "dsh"
	}
	if s.runners[input.RunnerType] == nil {
		return domain.AgentVersion{}, problem(400, "unknown_runner", "未知 Runner："+input.RunnerType)
	}
	if input.Model == "" {
		input.Model = "由运行时决定"
	}
	if input.Description == "" {
		input.Description = "Agent 配置新版本"
	}
	families, err := repo.ListAgentFamilies(ctx)
	if err != nil {
		return domain.AgentVersion{}, err
	}
	var family *domain.AgentFamily
	for index := range families {
		if families[index].ID == familyID {
			family = &families[index]
			break
		}
	}
	if family == nil {
		return domain.AgentVersion{}, problem(404, "agent_family_not_found", "Agent 资产不存在")
	}
	versions, err := repo.ListAgentVersions(ctx, familyID)
	if err != nil {
		return domain.AgentVersion{}, err
	}
	next := 1
	if len(versions) > 0 {
		next = versions[0].Version + 1
	}
	created := domain.AgentVersion{ID: fmt.Sprintf("%s-v%d", familyID, next), FamilyID: familyID, Name: family.Name, Handle: family.Handle, Version: next, RunnerType: input.RunnerType, Description: input.Description, Model: input.Model, ModelConnectionID: input.ModelConnectionID, Preset: agentPreset(input.RunnerType, input.Preset), SystemPrompt: strings.TrimSpace(input.SystemPrompt), Tools: normalizeTools(input.Tools), Skills: normalizeTools(input.Skills), CreatedAt: s.now().UTC()}
	return created, repo.CreateAgentVersion(ctx, created)
}

func agentPreset(runnerType, preset string) string {
	preset = strings.TrimSpace(preset)
	if preset == "" && runnerType == "dsh" {
		return "headless"
	}
	return preset
}

func (s *Service) validateAgentConnection(ctx context.Context, input CreateAgentInput) error {
	if input.ModelConnectionID == "" {
		return nil
	}
	if s.connections == nil {
		return problem(503, "model_connections_unavailable", "模型连接服务不可用")
	}
	connection, err := s.connections.GetModelConnection(ctx, input.ModelConnectionID)
	if err != nil {
		return problem(400, "model_connection_not_found", "模型连接不存在")
	}
	if connection.Status != "verified" {
		return problem(400, "model_connection_unverified", "模型连接尚未验证")
	}
	if input.RunnerType == "codex" && connection.Protocol != contract.ProtocolOpenAIResponses {
		return problem(400, "incompatible_model_protocol", "Codex 仅支持 Responses 连接")
	}
	if input.RunnerType == "hermes" && connection.Protocol != contract.ProtocolOpenAIChat {
		return problem(400, "incompatible_model_protocol", "Hermes 仅支持 Chat Completions 连接")
	}
	if input.RunnerType == "claude-code" {
		return problem(400, "incompatible_model_connection", "Claude Code 使用自身登录")
	}
	return nil
}

func (s *Service) ListModelConnections(ctx context.Context) ([]contract.ModelConnection, error) {
	if s.connections == nil {
		return nil, problem(503, "model_connections_unavailable", "模型连接服务不可用")
	}
	return s.connections.ListModelConnections(ctx)
}
func (s *Service) CreateModelConnection(ctx context.Context, input contract.ModelConnectionInput) (contract.ModelConnection, error) {
	if s.connections == nil {
		return contract.ModelConnection{}, problem(503, "model_connections_unavailable", "模型连接服务不可用")
	}
	return s.connections.CreateModelConnection(ctx, input)
}
func (s *Service) UpdateModelConnection(ctx context.Context, id string, input contract.ModelConnectionInput) (contract.ModelConnection, error) {
	if s.connections == nil {
		return contract.ModelConnection{}, problem(503, "model_connections_unavailable", "模型连接服务不可用")
	}
	return s.connections.UpdateModelConnection(ctx, id, input)
}
func (s *Service) VerifyModelConnection(ctx context.Context, id string) (contract.ModelConnection, error) {
	if s.connections == nil {
		return contract.ModelConnection{}, problem(503, "model_connections_unavailable", "模型连接服务不可用")
	}
	return s.connections.VerifyModelConnection(ctx, id)
}
func (s *Service) DeleteModelConnection(ctx context.Context, id string) error {
	if s.connections == nil {
		return problem(503, "model_connections_unavailable", "模型连接服务不可用")
	}
	return s.connections.DeleteModelConnection(ctx, id)
}

func (s *Service) CreateExperiment(ctx context.Context, title string) (domain.ExperimentView, error) {
	now := s.now().UTC()
	if strings.TrimSpace(title) == "" {
		title = "未命名实验"
	}
	experimentID := newID("exp")
	experiment := domain.Experiment{ID: experimentID, Title: title, ControlSessionID: "control-" + experimentID, CreatedAt: now, UpdatedAt: now}
	initial := domain.Message{ID: newID("msg"), ExperimentID: experiment.ID, Role: "assistant", Content: "这次想验证什么？你可以先选一个测试集和 Agent，也可以直接描述目标。", CreatedAt: now}
	err := s.repo.WithinTx(ctx, func(repo domain.Repository) error { return repo.CreateExperiment(ctx, experiment, initial) })
	if err != nil {
		return domain.ExperimentView{}, err
	}
	return s.GetExperiment(ctx, experiment.ID)
}

func (s *Service) GetExperiment(ctx context.Context, id string) (domain.ExperimentView, error) {
	experiment, err := s.repo.GetExperiment(ctx, id)
	if err != nil {
		return domain.ExperimentView{}, mapNotFound(err, "experiment_not_found", "实验不存在")
	}
	messages, err := s.repo.ListMessages(ctx, id)
	if err != nil {
		return domain.ExperimentView{}, err
	}
	runs, err := s.repo.ListRunsByExperiment(ctx, id)
	if err != nil {
		return domain.ExperimentView{}, err
	}
	controlEvents, err := s.repo.ListControlEvents(ctx, id)
	if err != nil {
		return domain.ExperimentView{}, err
	}
	view := domain.ExperimentView{Experiment: experiment, Messages: messages, Runs: runs, ControlEvents: controlEvents, A2UI: s.projectA2UI(experiment)}
	if experiment.DatasetVersionID != "" {
		dataset, e := s.repo.GetDatasetVersion(ctx, experiment.DatasetVersionID)
		if e != nil {
			return view, e
		}
		view.Dataset = &dataset
	}
	if experiment.AgentVersionID != "" {
		agent, e := s.repo.GetAgentVersion(ctx, experiment.AgentVersionID)
		if e != nil {
			return view, e
		}
		view.Agent = &agent
	}
	return view, nil
}

type ExperimentPatch struct {
	Title            *string
	DatasetVersionID *string
	AgentVersionID   *string
}

func (s *Service) UpdateExperiment(ctx context.Context, id string, patch ExperimentPatch) (domain.ExperimentView, error) {
	err := s.repo.WithinTx(ctx, func(repo domain.Repository) error {
		experiment, e := repo.GetExperiment(ctx, id)
		if e != nil {
			return mapNotFound(e, "experiment_not_found", "实验不存在")
		}
		now := s.now().UTC()
		if patch.Title != nil && strings.TrimSpace(*patch.Title) != "" {
			experiment.Title = strings.TrimSpace(*patch.Title)
		}
		if patch.DatasetVersionID == nil && patch.AgentVersionID == nil {
			experiment.UpdatedAt = now
			return repo.UpdateExperimentControl(ctx, experiment)
		}
		message := domain.Message{ID: newID("msg"), ExperimentID: id, Role: "assistant", CreatedAt: now}
		if patch.DatasetVersionID != nil {
			dataset, e := repo.GetDatasetVersion(ctx, *patch.DatasetVersionID)
			if e != nil {
				return mapNotFound(e, "dataset_not_found", "测试集版本不存在")
			}
			experiment.DatasetVersionID = dataset.ID
			message.Content = fmt.Sprintf("已选测试集「%s v%d」，共 %d 个用例。", dataset.Name, dataset.Version, len(dataset.Cases))
		}
		if patch.AgentVersionID != nil {
			agent, e := repo.GetAgentVersion(ctx, *patch.AgentVersionID)
			if e != nil {
				return mapNotFound(e, "agent_not_found", "Agent 版本不存在")
			}
			experiment.AgentVersionID = agent.ID
			message.Content = fmt.Sprintf("已选 Agent「%s v%d」。", agent.Handle, agent.Version)
		}
		experiment.UpdatedAt = message.CreatedAt
		return repo.UpdateExperimentSelection(ctx, experiment, message)
	})
	if err != nil {
		return domain.ExperimentView{}, err
	}
	return s.GetExperiment(ctx, id)
}

func (s *Service) AddMessage(ctx context.Context, experimentID, content string) (domain.ExperimentView, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return domain.ExperimentView{}, problem(400, "empty_message", "消息不能为空")
	}
	experiment, err := s.repo.GetExperiment(ctx, experimentID)
	if err != nil {
		return domain.ExperimentView{}, mapNotFound(err, "experiment_not_found", "实验不存在")
	}
	now := s.now().UTC()
	user := domain.Message{ID: newID("msg"), ExperimentID: experimentID, Role: "user", Content: content, CreatedAt: now}
	assistant := domain.Message{ID: newID("msg"), ExperimentID: experimentID, Role: "assistant", Content: s.guidance(experiment), CreatedAt: now.Add(time.Nanosecond)}
	err = s.repo.WithinTx(ctx, func(repo domain.Repository) error { return repo.AddMessages(ctx, experimentID, user, assistant) })
	if err != nil {
		return domain.ExperimentView{}, err
	}
	return s.GetExperiment(ctx, experimentID)
}

func (s *Service) guidance(experiment domain.Experiment) string {
	if experiment.DatasetVersionID == "" && experiment.AgentVersionID == "" {
		return "请先选择或导入一个测试集，再选择一个 Agent；发送消息不会直接开始运行。"
	}
	if experiment.DatasetVersionID == "" {
		return "Agent 已选择。还需要选择或导入测试集，然后在确认卡片中显式开始运行。"
	}
	if experiment.AgentVersionID == "" {
		return "测试集已选择。还需要选择 Agent，然后在确认卡片中显式开始运行。"
	}
	return "当前配置已就绪。请在运行快照卡片中核对版本，并点击卡片内的按钮开始运行。"
}

func mapNotFound(err error, code, message string) error {
	if errors.Is(err, domain.ErrNotFound) {
		return problem(404, code, message)
	}
	return err
}
