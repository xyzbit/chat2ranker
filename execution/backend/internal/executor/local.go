package executor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/xyzbit/agentkiln/execution/backend/artifacts"
	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/execution/backend/harness"
	"github.com/xyzbit/agentkiln/execution/backend/internal/domain"
	"github.com/xyzbit/agentkiln/execution/backend/internal/modelquota"
	"github.com/xyzbit/agentkiln/execution/backend/internal/workerprotocol"
)

type LocalConfig struct {
	ModelQuotaAuthority    *modelquota.Authority
	Repository             domain.Repository
	WorkerBinary           string
	RepositoryRoot         string
	ArtifactRoot           string
	SandboxRoot            string
	Timeout                time.Duration
	TerminationGrace       time.Duration
	ArtifactStore          artifacts.Store
	MaxArtifactBytes       int64
	Harnesses              *harness.Registry
	ResolveModelConnection func(context.Context, string) (contract.ModelConnection, string, error)
}

type Local struct{ config LocalConfig }

func NewLocal(config LocalConfig) *Local {
	if config.Timeout <= 0 {
		config.Timeout = 10 * time.Minute
	}
	if config.TerminationGrace <= 0 {
		config.TerminationGrace = 5 * time.Second
	}
	if config.MaxArtifactBytes <= 0 {
		config.MaxArtifactBytes = 64 << 20
	}
	return &Local{config: config}
}

func (*Local) Name() string { return "local-process" }

func (executor *Local) Probe(_ context.Context, harness string) contract.Availability {
	availability := executor.config.Harnesses.Probe(context.Background(), harness)
	if strings.TrimSpace(executor.config.WorkerBinary) == "" {
		return contract.Availability{Label: availability.Label, Reason: "execution-worker 未配置"}
	}
	if _, err := os.Stat(executor.config.WorkerBinary); err != nil {
		return contract.Availability{Label: availability.Label, Reason: "execution-worker 尚未构建"}
	}
	availability.ModelQuota = availability.ModelQuota && executor.config.ModelQuotaAuthority != nil
	return availability
}

func (executor *Local) Run(ctx context.Context, execution contract.Execution, emit domain.EventSink) (contract.Result, error) {
	if executor.config.Repository == nil {
		return contract.Result{}, errors.New("local executor requires an evidence repository")
	}
	executionID, spec := execution.ID, execution.Spec
	timeout := executor.config.Timeout
	if spec.TimeoutMs > 0 {
		timeout = min(timeout, time.Duration(spec.TimeoutMs)*time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	workspace := filepath.Join(executor.config.SandboxRoot, executionID)
	cleanupWorkspace := true
	defer func() {
		if cleanupWorkspace {
			removePrivateWorkspace(executor.config.SandboxRoot, workspace)
		}
	}()
	artifactDir := filepath.Join(executor.config.ArtifactRoot, executionID)
	request := workerprotocol.Request{MaxModelRequests: execution.MaxModelRequests,
		ProtocolVersion: workerprotocol.Version,
		ExecutionID:     executionID,
		Spec:            spec,
		WorkspaceDir:    workspace,
		ArtifactDir:     artifactDir,
		HarnessHome:     filepath.Join(artifactDir, "harness-home"),
		Metadata:        spec.Metadata,
	}
	if deadline, ok := ctx.Deadline(); ok {
		timeout = min(timeout, time.Until(deadline))
	}
	if timeout < time.Millisecond {
		return contract.Result{}, context.DeadlineExceeded
	}
	request.Spec.TimeoutMs = timeout.Milliseconds()
	if spec.ModelConnectionID != "" {
		if executor.config.ResolveModelConnection == nil {
			return contract.Result{}, errors.New("model connection resolver is not configured")
		}
		connection, credential, err := executor.config.ResolveModelConnection(ctx, spec.ModelConnectionID)
		if err != nil {
			return contract.Result{}, err
		}
		request.ModelConnection, request.Credential = &connection, credential
	}
	quota, quotaErr := executor.config.ModelQuotaAuthority.Issue(execution)
	if quotaErr != nil {
		return contract.Result{}, quotaErr
	}
	request.ModelQuota = quota
	response, err := executor.invoke(ctx, request, emit)
	if errors.Is(err, domain.ErrRuntimeStopUnconfirmed) {
		cleanupWorkspace = false
	}
	if err != nil {
		return contract.Result{}, err
	}
	if err := workerprotocol.ValidateResponse(response, executionID); err != nil {
		return contract.Result{}, err
	}
	var responseErr error
	if response.Status == contract.StatusFailed {
		responseErr = errors.New(response.Error)
	}
	if executor.config.ArtifactStore != nil {
		if err := executor.publishArtifacts(ctx, execution, &response.Result); err != nil {
			return response.Result, errors.Join(responseErr, err)
		}
		removePrivateWorkspace(executor.config.ArtifactRoot, artifactDir)
	} else if err := executor.config.Repository.RecordEvidence(ctx, execution.ID, execution.Attempt, response.Result); err != nil {
		return response.Result, errors.Join(responseErr, err)
	}
	return response.Result, responseErr
}

func (executor *Local) invoke(ctx context.Context, request workerprotocol.Request, emit domain.EventSink) (workerprotocol.Response, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return workerprotocol.Response{}, err
	}
	command := exec.Command(executor.config.WorkerBinary)
	command.Stdin = bytes.NewReader(append(payload, '\n'))
	command.Env = workerEnvironment(executor.config.RepositoryRoot)
	if runtime.GOOS != "windows" {
		command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	stderrPipe, err := command.StderrPipe()
	if err != nil {
		return workerprotocol.Response{}, fmt.Errorf("open execution-worker event stream: %w", err)
	}
	if err := command.Start(); err != nil {
		return workerprotocol.Response{}, fmt.Errorf("start execution-worker: %w", err)
	}
	var eventErr error
	var eventErrMu sync.Mutex
	eventsDone := make(chan struct{})
	go func() {
		defer close(eventsDone)
		scanner := bufio.NewScanner(stderrPipe)
		scanner.Buffer(make([]byte, 64<<10), 2<<20)
		for scanner.Scan() {
			line := scanner.Text()
			event, matched, decodeErr := workerprotocol.DecodeEvent(line)
			if !matched {
				stderr.WriteString(line + "\n")
				continue
			}
			if decodeErr != nil {
				eventErrMu.Lock()
				if eventErr == nil {
					eventErr = decodeErr
				}
				eventErrMu.Unlock()
				continue
			}
			if emit != nil {
				if sinkErr := emit(domain.ExecutorEvent{Type: event.Type, Message: event.Message, Data: event.Data, At: event.At}); sinkErr != nil {
					eventErrMu.Lock()
					if eventErr == nil {
						eventErr = sinkErr
					}
					eventErrMu.Unlock()
				}
			}
		}
		if scanErr := scanner.Err(); scanErr != nil && !errors.Is(scanErr, os.ErrClosed) {
			eventErrMu.Lock()
			if eventErr == nil {
				eventErr = scanErr
			}
			eventErrMu.Unlock()
		}
	}()
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err = <-done:
	case <-ctx.Done():
		confirmed := true
		if runtime.GOOS == "windows" {
			_ = command.Process.Kill()
			confirmed = false
			<-done
		} else {
			_ = syscall.Kill(-command.Process.Pid, syscall.SIGTERM)
			timer := time.NewTimer(executor.config.TerminationGrace)
			select {
			case <-done:
				timer.Stop()
			case <-timer.C:
				confirmed = false
				_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
				<-done
			}
		}
		<-eventsDone
		if !confirmed {
			return workerprotocol.Response{}, fmt.Errorf("%w: %v", domain.ErrRuntimeStopUnconfirmed, ctx.Err())
		}
		return workerprotocol.Response{}, ctx.Err()
	}
	<-eventsDone
	eventErrMu.Lock()
	streamErr := eventErr
	eventErrMu.Unlock()
	if streamErr != nil {
		return workerprotocol.Response{}, fmt.Errorf("execution-worker event stream: %w", streamErr)
	}
	var response workerprotocol.Response
	if decodeErr := json.Unmarshal(stdout.Bytes(), &response); decodeErr != nil {
		return response, fmt.Errorf("decode execution-worker response: %w: %s", decodeErr, strings.TrimSpace(stderr.String()))
	}
	if err != nil && response.Error == "" {
		return response, fmt.Errorf("execution-worker exited: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return response, nil
}

func workerEnvironment(repositoryRoot string) []string {
	allowed := map[string]bool{"PATH": true, "TMPDIR": true, "LANG": true, "LC_ALL": true, "NODE_EXTRA_CA_CERTS": true, "SSL_CERT_FILE": true, "SSL_CERT_DIR": true, "DEEPSEEK_API_KEY": true, "DEEPSEEK_BASE_URL": true, "ANTHROPIC_API_KEY": true, "OPENAI_API_KEY": true, "GOOGLE_API_KEY": true, "RANK_DSH_BIN": true, "EXECUTION_HARNESS_DSH_ARGV": true, "EXECUTION_HARNESS_PI_ARGV": true, "EXECUTION_HARNESS_CLAUDE_CODE_ARGV": true, "EXECUTION_HARNESS_CODEX_ARGV": true, "EXECUTION_HARNESS_HERMES_ARGV": true}
	result := []string{"EXECUTION_REPO_ROOT=" + repositoryRoot}
	if userHome, err := os.UserHomeDir(); err == nil {
		result = append(result, "EXECUTION_USER_HOME="+userHome)
	}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if allowed[name] {
			result = append(result, entry)
		}
	}
	return result
}

func removePrivateWorkspace(root, target string) {
	if root == "" || target == "" {
		return
	}
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return
	}
	_ = os.RemoveAll(target)
}

var _ domain.Executor = (*Local)(nil)

// Cleanup cannot confirm a lost local process tree from another process owner.
func (*Local) Cleanup(context.Context, contract.Execution) error {
	return domain.ErrRuntimeStopUnconfirmed
}
