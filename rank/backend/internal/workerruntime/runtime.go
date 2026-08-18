package workerruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/xyzbit/chat2ranker/rank/backend/internal/domain"
	"github.com/xyzbit/chat2ranker/rank/backend/internal/runnerprotocol"
)

const retainedOutputBytes = 4 << 20

type limitedBuffer struct {
	data      bytes.Buffer
	remaining int
	truncated bool
}

func newLimitedBuffer(limit int) *limitedBuffer {
	return &limitedBuffer{remaining: limit}
}

func (buffer *limitedBuffer) Write(value []byte) (int, error) {
	original := len(value)
	if buffer.remaining == 0 {
		buffer.truncated = true
		return original, nil
	}
	kept := value
	if len(kept) > buffer.remaining {
		kept = kept[:buffer.remaining]
		buffer.truncated = true
	}
	_, _ = buffer.data.Write(kept)
	buffer.remaining -= len(kept)
	return original, nil
}

func (buffer *limitedBuffer) String() string {
	if buffer.truncated {
		return buffer.data.String() + "\n[output truncated]"
	}
	return buffer.data.String()
}

func Execute(parent context.Context, request runnerprotocol.Request) runnerprotocol.Response {
	started := time.Now()
	response := runnerprotocol.Response{ProtocolVersion: runnerprotocol.Version, ExecutionID: request.ExecutionID, Kind: request.Kind, Status: "failed"}
	if err := validate(request); err != nil {
		response.Error = err.Error()
		return response
	}
	if err := prepare(request); err != nil {
		response.Error = err.Error()
		return response
	}
	_ = writeJSON(filepath.Join(request.ArtifactDir, "request.json"), request)
	timeout := time.Duration(request.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	var err error
	if request.RunnerType == "mock" {
		response, err = executeMock(ctx, request)
	} else {
		response, err = executeCommand(ctx, request)
	}
	response.ProtocolVersion = runnerprotocol.Version
	response.ExecutionID = request.ExecutionID
	response.Kind = request.Kind
	response.DurationMs = time.Since(started).Milliseconds()
	if err != nil {
		response.Status = "failed"
		response.Error = err.Error()
	} else {
		response.Status = "complete"
	}
	response.Artifacts = collectArtifacts(request)
	_ = writeJSON(filepath.Join(request.ArtifactDir, "result.json"), response)
	response.Artifacts = collectArtifacts(request)
	return response
}

func validate(request runnerprotocol.Request) error {
	if request.ProtocolVersion != runnerprotocol.Version {
		return fmt.Errorf("unsupported runner protocol version %d", request.ProtocolVersion)
	}
	if request.ExecutionID == "" || request.RunID == "" || request.CaseID == "" {
		return errors.New("executionId, runId and caseId are required")
	}
	if request.Kind != runnerprotocol.KindCase && request.Kind != runnerprotocol.KindJudge {
		return fmt.Errorf("unsupported execution kind %q", request.Kind)
	}
	for name, value := range map[string]string{"workspaceDir": request.WorkspaceDir, "artifactDir": request.ArtifactDir, "harnessHome": request.HarnessHome} {
		if !filepath.IsAbs(value) {
			return fmt.Errorf("%s must be absolute", name)
		}
	}
	return nil
}

func prepare(request runnerprotocol.Request) error {
	for _, directory := range []string{request.WorkspaceDir, request.ArtifactDir, request.HarnessHome} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return err
		}
		if err := os.Chmod(directory, 0o700); err != nil {
			return err
		}
	}
	return nil
}

func executeMock(ctx context.Context, request runnerprotocol.Request) (runnerprotocol.Response, error) {
	select {
	case <-ctx.Done():
		return runnerprotocol.Response{}, ctx.Err()
	case <-time.After(35 * time.Millisecond):
	}
	if request.Kind == runnerprotocol.KindJudge {
		passed := stringValue(request.Expected["demoOutcome"]) != "fail"
		reason := "满足测试集断言"
		if !passed {
			reason = stringValue(request.Expected["failureReason"])
			if reason == "" {
				reason = "未满足测试集断言"
			}
		}
		score := 0.0
		if passed {
			score = 1
		}
		return runnerprotocol.Response{Passed: passed, Score: score, Reason: reason, Cost: 0.006, CostKnown: true, Usage: domain.Usage{InputTokens: 40, OutputTokens: 8}}, nil
	}
	summary := stringValue(request.Expected["summary"])
	if summary == "" {
		summary = "任务成功完成"
	}
	output := fmt.Sprintf("已完成任务：%s。结果包含：%s。", request.Prompt, summary)
	if stringValue(request.Expected["demoOutcome"]) == "fail" {
		output = fmt.Sprintf("已完成任务：%s，但部分证据无法验证。", request.Prompt)
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(request.CaseID + request.Model))
	cost := float64(18+hash.Sum32()%19) / 1000
	return runnerprotocol.Response{Output: output, Cost: cost, CostKnown: true, Usage: domain.Usage{InputTokens: 70, OutputTokens: 30}}, nil
}

func executeCommand(ctx context.Context, request runnerprotocol.Request) (runnerprotocol.Response, error) {
	argv, err := commandArgv(request)
	if err != nil {
		return runnerprotocol.Response{}, err
	}
	stdout := newLimitedBuffer(retainedOutputBytes)
	stderr := newLimitedBuffer(retainedOutputBytes)
	command := exec.Command(argv[0], argv[1:]...)
	command.Dir = request.WorkspaceDir
	command.Env = commandEnvironment(request)
	if runtime.GOOS != "windows" {
		command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		return runnerprotocol.Response{}, fmt.Errorf("start %s runner: %w", request.RunnerType, err)
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
		err = <-done
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			err = fmt.Errorf("%s runner timed out after %dms", request.RunnerType, request.TimeoutMs)
		} else {
			err = ctx.Err()
		}
	}
	stdoutText, stderrText := strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String())
	_ = writeText(filepath.Join(request.ArtifactDir, "stdout.txt"), stdoutText)
	_ = writeText(filepath.Join(request.ArtifactDir, "stderr.txt"), stderrText)
	_ = writeJSON(filepath.Join(request.ArtifactDir, "trace.jsonl"), map[string]any{"type": "process.exit", "argv": redactArgv(argv), "error": errorText(err), "at": time.Now().UTC()})
	if err != nil {
		return runnerprotocol.Response{}, fmt.Errorf("%s runner failed: %w: %s", request.RunnerType, err, stderrText)
	}
	if request.Kind == runnerprotocol.KindJudge {
		var verdict struct {
			Passed bool    `json:"passed"`
			Score  float64 `json:"score"`
			Reason string  `json:"reason"`
		}
		if err := decodeLastJSONObject(stdoutText, &verdict); err != nil {
			return runnerprotocol.Response{}, fmt.Errorf("judge returned invalid JSON: %w", err)
		}
		return runnerprotocol.Response{Passed: verdict.Passed, Score: verdict.Score, Reason: verdict.Reason, Output: stdoutText, CostKnown: false}, nil
	}
	return runnerprotocol.Response{Output: stdoutText, CostKnown: false}, nil
}

func commandArgv(request runnerprotocol.Request) ([]string, error) {
	key := "RANK_RUNNER_" + strings.ToUpper(strings.ReplaceAll(request.RunnerType, "-", "_")) + "_ARGV"
	if configured := strings.TrimSpace(os.Getenv(key)); configured != "" {
		var argv []string
		if err := json.Unmarshal([]byte(configured), &argv); err != nil || len(argv) == 0 {
			return nil, fmt.Errorf("%s must be a non-empty JSON string array", key)
		}
		for index := range argv {
			argv[index] = strings.NewReplacer("{prompt}", request.Prompt, "{workspace}", request.WorkspaceDir, "{harnessHome}", request.HarnessHome, "{model}", request.Model).Replace(argv[index])
		}
		return argv, nil
	}
	if request.RunnerType != "dsh" {
		return nil, fmt.Errorf("runner %q is not configured; set %s", request.RunnerType, key)
	}
	repositoryRoot := strings.TrimSpace(os.Getenv("RANK_REPO_ROOT"))
	if repositoryRoot == "" {
		return nil, errors.New("RANK_REPO_ROOT is required for the built-in DSH runner")
	}
	prompt := request.Prompt
	if request.Kind == runnerprotocol.KindJudge {
		prompt = judgePrompt(request)
	}
	return []string{"node", filepath.Join(repositoryRoot, "apps/cli/lib/bin.js"), "--profile", "headless", prompt}, nil
}

func judgePrompt(request runnerprotocol.Request) string {
	expected, _ := json.Marshal(request.Expected)
	return "Act as an isolated evaluator. Compare the candidate output with the expected criteria. Return only JSON with fields passed (boolean), score (0..1), and reason (short string).\nExpected: " + string(expected) + "\nCandidate:\n" + request.CandidateOutput
}

func commandEnvironment(request runnerprotocol.Request) []string {
	allowed := map[string]bool{"PATH": true, "TMPDIR": true, "LANG": true, "LC_ALL": true, "NODE_EXTRA_CA_CERTS": true, "SSL_CERT_FILE": true, "SSL_CERT_DIR": true, "DEEPSEEK_API_KEY": true, "DEEPSEEK_BASE_URL": true, "ANTHROPIC_API_KEY": true, "OPENAI_API_KEY": true, "GOOGLE_API_KEY": true}
	environment := []string{}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if allowed[name] {
			environment = append(environment, entry)
		}
	}
	environment = append(environment,
		"HOME="+request.HarnessHome,
		"DSH_HOME="+request.HarnessHome,
		"DSH_TELEMETRY_DISABLED=1",
		"DSH_PERMISSION_MODE=read-only",
		"RANK_EXECUTION_ID="+request.ExecutionID,
		"RANK_WORKSPACE="+request.WorkspaceDir,
	)
	return environment
}

func collectArtifacts(request runnerprotocol.Request) []domain.ArtifactRef {
	result := []domain.ArtifactRef{}
	_ = filepath.WalkDir(request.ArtifactDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return nil
		}
		relative, relErr := filepath.Rel(request.ArtifactDir, path)
		if relErr != nil {
			return nil
		}
		kind := "artifact"
		switch filepath.Base(path) {
		case "request.json":
			kind = "request"
		case "result.json":
			kind = "result"
		case "trace.jsonl":
			kind = "trace"
		case "stdout.txt":
			kind = "stdout"
		case "stderr.txt":
			kind = "stderr"
		}
		result = append(result, domain.ArtifactRef{Kind: kind, ExecutionID: request.ExecutionID, Path: filepath.ToSlash(filepath.Join(request.RunID, request.CaseID, request.ExecutionID, relative)), Size: info.Size()})
		return nil
	})
	return result
}

func writeJSON(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return writeBytes(path, append(data, '\n'))
}

func writeText(path, value string) error {
	return writeBytes(path, []byte(value+"\n"))
}

func writeBytes(path string, value []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(value)
	return err
}

func decodeLastJSONObject(value string, target any) error {
	for index := strings.LastIndex(value, "{"); index >= 0; index = strings.LastIndex(value[:index], "{") {
		candidate := strings.TrimSpace(value[index:])
		if json.Unmarshal([]byte(candidate), target) == nil {
			return nil
		}
	}
	return errors.New("no JSON object found")
}

func redactArgv(argv []string) []string {
	result := append([]string(nil), argv...)
	for index, value := range result {
		if len(value) > 400 {
			result[index] = value[:400] + "…"
		}
	}
	return result
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func ParseRequest(reader io.Reader) (runnerprotocol.Request, error) {
	var request runnerprotocol.Request
	decoder := json.NewDecoder(io.LimitReader(reader, 8<<20))
	if err := decoder.Decode(&request); err != nil {
		return request, err
	}
	return request, nil
}

func WriteResponse(writer io.Writer, response runnerprotocol.Response) error {
	return json.NewEncoder(writer).Encode(response)
}

func ExitCode(response runnerprotocol.Response) int {
	if response.Status == "complete" {
		return 0
	}
	return 1
}

func ParseBoolEnv(name string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}
