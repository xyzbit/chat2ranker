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

	"github.com/xyzbit/chat2ranker/execution/backend/contract"
	"github.com/xyzbit/chat2ranker/execution/backend/harness"
	"github.com/xyzbit/chat2ranker/execution/backend/internal/domain"
	"github.com/xyzbit/chat2ranker/execution/backend/internal/workerprotocol"
)

type LocalConfig struct {
	WorkerBinary   string
	RepositoryRoot string
	ArtifactRoot   string
	SandboxRoot    string
	Timeout        time.Duration
	Harnesses      *harness.Registry
}

type Local struct{ config LocalConfig }

func NewLocal(config LocalConfig) *Local {
	if config.Timeout <= 0 {
		config.Timeout = 10 * time.Minute
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
	return availability
}

func (executor *Local) Run(ctx context.Context, executionID string, spec contract.Spec, emit domain.EventSink) (contract.Result, error) {
	workspace := filepath.Join(executor.config.SandboxRoot, executionID)
	defer removePrivateWorkspace(executor.config.SandboxRoot, workspace)
	artifactDir := filepath.Join(executor.config.ArtifactRoot, executionID)
	request := workerprotocol.Request{
		ProtocolVersion: workerprotocol.Version,
		ExecutionID:     executionID,
		Spec:            spec,
		WorkspaceDir:    workspace,
		ArtifactDir:     artifactDir,
		HarnessHome:     filepath.Join(artifactDir, "harness-home"),
		Metadata:        spec.Metadata,
	}
	response, err := executor.invoke(ctx, request, emit)
	if err != nil {
		return contract.Result{}, err
	}
	if response.ProtocolVersion != workerprotocol.Version || response.ExecutionID != executionID {
		return contract.Result{}, errors.New("execution-worker returned a mismatched response")
	}
	if response.Status != contract.StatusCompleted {
		return contract.Result{}, errors.New(response.Error)
	}
	return response.Result, nil
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
		if runtime.GOOS == "windows" {
			_ = command.Process.Kill()
		} else {
			_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		}
		<-done
		<-eventsDone
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
	allowed := map[string]bool{"PATH": true, "TMPDIR": true, "LANG": true, "LC_ALL": true, "NODE_EXTRA_CA_CERTS": true, "SSL_CERT_FILE": true, "SSL_CERT_DIR": true, "DEEPSEEK_API_KEY": true, "DEEPSEEK_BASE_URL": true, "ANTHROPIC_API_KEY": true, "OPENAI_API_KEY": true, "GOOGLE_API_KEY": true, "EXECUTION_HARNESS_DSH_ARGV": true, "EXECUTION_HARNESS_PI_ARGV": true, "EXECUTION_HARNESS_CLAUDE_CODE_ARGV": true, "EXECUTION_HARNESS_CODEX_ARGV": true, "EXECUTION_HARNESS_HERMES_ARGV": true}
	result := []string{"EXECUTION_REPO_ROOT=" + repositoryRoot}
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
