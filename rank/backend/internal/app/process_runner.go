package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/xyzbit/chat2ranker/rank/backend/internal/domain"
	"github.com/xyzbit/chat2ranker/rank/backend/internal/runnerprotocol"
)

type ProcessRunnerConfig struct {
	WorkerBinary   string
	RepositoryRoot string
	ArtifactRoot   string
	SandboxRoot    string
	Timeout        time.Duration
	JudgeRunner    string
	JudgeModel     string
}

type ProcessRunner struct {
	runnerType string
	config     ProcessRunnerConfig
}

func ProcessRunners(config ProcessRunnerConfig) RunnerRegistry {
	if config.Timeout <= 0 {
		config.Timeout = 10 * time.Minute
	}
	if config.JudgeRunner == "" {
		config.JudgeRunner = "dsh"
	}
	result := RunnerRegistry{}
	for _, runnerType := range []string{"mock", "dsh", "pi", "claude-code", "codex", "hermes"} {
		result[runnerType] = ProcessRunner{runnerType: runnerType, config: config}
	}
	return result
}

func (runner ProcessRunner) Probe(_ context.Context, _ domain.AgentVersion) domain.RuntimeAvailability {
	label := map[string]string{"mock": "隔离 Demo Runner", "dsh": "DeepSeek Harness", "pi": "Pi", "claude-code": "Claude Code", "codex": "Codex", "hermes": "Hermes"}[runner.runnerType]
	if strings.TrimSpace(runner.config.WorkerBinary) == "" {
		return domain.RuntimeAvailability{Available: false, Label: label, Reason: "未配置 rank-worker 可执行文件"}
	}
	if _, err := os.Stat(runner.config.WorkerBinary); err != nil {
		return domain.RuntimeAvailability{Available: false, Label: label, Reason: "rank-worker 尚未构建"}
	}
	if runner.runnerType == "mock" {
		return domain.RuntimeAvailability{Available: true, Label: label}
	}
	key := runnerArgvEnv(runner.runnerType)
	if strings.TrimSpace(os.Getenv(key)) != "" {
		return domain.RuntimeAvailability{Available: true, Label: label}
	}
	if runner.runnerType == "dsh" {
		if strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY")) == "" {
			return domain.RuntimeAvailability{Available: false, Label: label, Reason: "未配置 DEEPSEEK_API_KEY"}
		}
		if _, err := os.Stat(filepath.Join(runner.config.RepositoryRoot, "apps/cli/lib/bin.js")); err != nil {
			return domain.RuntimeAvailability{Available: false, Label: label, Reason: "仓库内 DSH CLI 不存在"}
		}
		return domain.RuntimeAvailability{Available: true, Label: label}
	}
	return domain.RuntimeAvailability{Available: false, Label: label, Reason: "未配置 " + key}
}

func (runner ProcessRunner) RunCase(ctx context.Context, spec ExecutionSpec) (domain.CaseResult, error) {
	caseExecutionID := executionID("case")
	candidate, caseWorkspace, err := runner.execute(ctx, executionRequest{
		ExecutionID: caseExecutionID,
		Kind:        runnerprotocol.KindCase,
		RunnerType:  runner.runnerType,
		Spec:        spec,
		Prompt:      spec.Case.Input,
	})
	defer removePrivateWorkspace(runner.config.SandboxRoot, caseWorkspace)
	if err != nil {
		return domain.CaseResult{}, err
	}
	judgeRunner := runner.config.JudgeRunner
	if runner.runnerType == "mock" {
		judgeRunner = "mock"
	}
	judgeExecutionID := executionID("judge")
	judge, judgeWorkspace, err := runner.execute(ctx, executionRequest{
		ExecutionID:     judgeExecutionID,
		Kind:            runnerprotocol.KindJudge,
		RunnerType:      judgeRunner,
		Model:           runner.config.JudgeModel,
		Spec:            spec,
		Prompt:          spec.Case.Input,
		CandidateOutput: candidate.Output,
	})
	defer removePrivateWorkspace(runner.config.SandboxRoot, judgeWorkspace)
	if err != nil {
		return domain.CaseResult{}, err
	}
	artifacts := append(append([]domain.ArtifactRef{}, candidate.Artifacts...), judge.Artifacts...)
	return domain.CaseResult{
		CaseID:           spec.Case.ID,
		Title:            spec.Case.Title,
		Passed:           judge.Passed,
		Cost:             candidate.Cost + judge.Cost,
		CostKnown:        candidate.CostKnown && judge.CostKnown,
		Score:            judge.Score,
		Output:           candidate.Output,
		Reason:           judge.Reason,
		DurationMs:       candidate.DurationMs + judge.DurationMs,
		ExecutionID:      caseExecutionID,
		JudgeExecutionID: judgeExecutionID,
		Usage:            domain.Usage{InputTokens: candidate.Usage.InputTokens + judge.Usage.InputTokens, OutputTokens: candidate.Usage.OutputTokens + judge.Usage.OutputTokens},
		Artifacts:        artifacts,
	}, nil
}

type executionRequest struct {
	ExecutionID     string
	Kind            string
	RunnerType      string
	Model           string
	Spec            ExecutionSpec
	Prompt          string
	CandidateOutput string
}

func (runner ProcessRunner) execute(ctx context.Context, input executionRequest) (runnerprotocol.Response, string, error) {
	runPart := safeComponent(input.Spec.RunID)
	casePart := safeComponent(input.Spec.Case.ID)
	workspace := filepath.Join(runner.config.SandboxRoot, runPart, input.ExecutionID)
	artifactDir := filepath.Join(runner.config.ArtifactRoot, runPart, casePart, input.ExecutionID)
	harnessHome := filepath.Join(artifactDir, "harness-home")
	request := runnerprotocol.Request{
		ProtocolVersion: runnerprotocol.Version,
		ExecutionID:     input.ExecutionID,
		Kind:            input.Kind,
		RunID:           runPart,
		CaseID:          casePart,
		RunnerType:      input.RunnerType,
		Model:           input.Model,
		Tools:           input.Spec.Agent.Tools,
		Prompt:          input.Prompt,
		Expected:        input.Spec.Case.Expected,
		CandidateOutput: input.CandidateOutput,
		WorkspaceDir:    workspace,
		ArtifactDir:     artifactDir,
		HarnessHome:     harnessHome,
		TimeoutMs:       runner.config.Timeout.Milliseconds(),
		Metadata:        map[string]string{"agentVersionId": input.Spec.Agent.ID, "caseTitle": input.Spec.Case.Title},
	}
	if request.Model == "" {
		request.Model = input.Spec.Agent.Model
	}
	response, err := runner.invokeWorker(ctx, request)
	if err != nil {
		return response, workspace, err
	}
	if response.ProtocolVersion != runnerprotocol.Version || response.ExecutionID != input.ExecutionID || response.Kind != input.Kind {
		return response, workspace, errors.New("rank-worker returned a mismatched response")
	}
	if response.Status != "complete" {
		return response, workspace, errors.New(response.Error)
	}
	return response, workspace, nil
}

func (runner ProcessRunner) invokeWorker(ctx context.Context, request runnerprotocol.Request) (runnerprotocol.Response, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return runnerprotocol.Response{}, err
	}
	command := exec.Command(runner.config.WorkerBinary)
	command.Stdin = bytes.NewReader(append(payload, '\n'))
	command.Env = workerEnvironment(runner.config.RepositoryRoot)
	if runtime.GOOS != "windows" {
		command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Start(); err != nil {
		return runnerprotocol.Response{}, fmt.Errorf("start rank-worker: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err = <-done:
	case <-ctx.Done():
		if runtime.GOOS == "windows" {
			_ = command.Process.Kill()
		} else {
			_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		}
		<-done
		return runnerprotocol.Response{}, ctx.Err()
	}
	var response runnerprotocol.Response
	if decodeErr := json.Unmarshal(stdout.Bytes(), &response); decodeErr != nil {
		return response, fmt.Errorf("decode rank-worker response: %w: %s", decodeErr, strings.TrimSpace(stderr.String()))
	}
	if err != nil && response.Error == "" {
		return response, fmt.Errorf("rank-worker exited: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return response, nil
}

func workerEnvironment(repositoryRoot string) []string {
	allowed := map[string]bool{"PATH": true, "TMPDIR": true, "LANG": true, "LC_ALL": true, "NODE_EXTRA_CA_CERTS": true, "SSL_CERT_FILE": true, "SSL_CERT_DIR": true, "DEEPSEEK_API_KEY": true, "DEEPSEEK_BASE_URL": true, "ANTHROPIC_API_KEY": true, "OPENAI_API_KEY": true, "GOOGLE_API_KEY": true, "RANK_RUNNER_DSH_ARGV": true, "RANK_RUNNER_PI_ARGV": true, "RANK_RUNNER_CLAUDE_CODE_ARGV": true, "RANK_RUNNER_CODEX_ARGV": true, "RANK_RUNNER_HERMES_ARGV": true}
	result := []string{"RANK_REPO_ROOT=" + repositoryRoot}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if allowed[name] {
			result = append(result, entry)
		}
	}
	return result
}

func runnerArgvEnv(runnerType string) string {
	return "RANK_RUNNER_" + strings.ToUpper(strings.ReplaceAll(runnerType, "-", "_")) + "_ARGV"
}

func executionID(prefix string) string {
	value := make([]byte, 12)
	if _, err := rand.Read(value); err != nil {
		panic(err)
	}
	return prefix + "-" + hex.EncodeToString(value)
}

func safeComponent(value string) string {
	value = strings.Map(func(character rune) rune {
		if unicode.IsLetter(character) || unicode.IsDigit(character) || character == '-' || character == '_' {
			return character
		}
		return '_'
	}, value)
	if value == "" {
		return "unknown"
	}
	return value
}

func removePrivateWorkspace(root, target string) {
	if root == "" || target == "" {
		return
	}
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || relative == ".." {
		return
	}
	_ = os.RemoveAll(target)
}
