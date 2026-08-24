package workerruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xyzbit/chat2ranker/execution/backend/contract"
	"github.com/xyzbit/chat2ranker/execution/backend/harness"
	"github.com/xyzbit/chat2ranker/execution/backend/internal/workerprotocol"
)

func Execute(parent context.Context, request workerprotocol.Request, emit func(workerprotocol.Event) error) workerprotocol.Response {
	started := time.Now()
	response := workerprotocol.Response{ProtocolVersion: workerprotocol.Version, ExecutionID: request.ExecutionID, Status: contract.StatusFailed}
	if err := validate(request); err != nil {
		response.Error = err.Error()
		return response
	}
	if err := prepare(request); err != nil {
		response.Error = err.Error()
		return response
	}
	_ = writeJSON(filepath.Join(request.ArtifactDir, "request.json"), request)
	timeout := time.Duration(request.Spec.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	registry, registryErr := harness.DefaultRegistry(os.Getenv("EXECUTION_REPO_ROOT"))
	if registryErr != nil {
		response.Error = registryErr.Error()
		return response
	}
	var progress func(harness.ProgressEvent) error
	if emit != nil {
		progress = func(event harness.ProgressEvent) error {
			return emit(workerprotocol.Event{Type: event.Type, Message: event.Message, Data: event.Data, At: event.At})
		}
	}
	result, err := registry.Run(ctx, harness.Invocation{ExecutionID: request.ExecutionID, Spec: request.Spec, Workspace: request.WorkspaceDir, ArtifactDir: request.ArtifactDir, HarnessHome: request.HarnessHome, Metadata: request.Metadata, Emit: progress})
	result.DurationMs = time.Since(started).Milliseconds()
	if err != nil {
		response.Error = err.Error()
	} else {
		response.Status = contract.StatusCompleted
	}
	response.Result = result
	_ = writeJSON(filepath.Join(request.ArtifactDir, "result.json"), response)
	response.Result.Artifacts = collectArtifacts(request)
	return response
}

func validate(request workerprotocol.Request) error {
	if request.ProtocolVersion != workerprotocol.Version || request.Spec.ProtocolVersion != contract.ProtocolVersion {
		return errors.New("unsupported execution protocol version")
	}
	if request.ExecutionID == "" || request.Spec.Prompt == "" || request.Spec.Harness == "" {
		return errors.New("executionId, harness and prompt are required")
	}
	if request.Spec.Kind != contract.KindAgent && request.Spec.Kind != contract.KindJudge {
		return fmt.Errorf("unsupported execution kind %q", request.Spec.Kind)
	}
	for name, value := range map[string]string{"workspaceDir": request.WorkspaceDir, "artifactDir": request.ArtifactDir, "harnessHome": request.HarnessHome} {
		if !filepath.IsAbs(value) {
			return fmt.Errorf("%s must be absolute", name)
		}
	}
	return nil
}

func prepare(request workerprotocol.Request) error {
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

func collectArtifacts(request workerprotocol.Request) []contract.ArtifactRef {
	result := []contract.ArtifactRef{}
	_ = filepath.WalkDir(request.ArtifactDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		relative, relErr := filepath.Rel(request.ArtifactDir, path)
		if relErr != nil {
			return nil
		}
		if !retainedArtifact(relative, entry) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
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
		case "session.jsonl.zstd":
			kind = "session-history"
		}
		result = append(result, contract.ArtifactRef{Kind: kind, ExecutionID: request.ExecutionID, Path: filepath.ToSlash(filepath.Join(request.ExecutionID, relative)), Size: info.Size()})
		return nil
	})
	return result
}

func retainedArtifact(relative string, entry fs.DirEntry) bool {
	clean := filepath.Clean(relative)
	if clean == "." {
		return true
	}
	parts := strings.Split(filepath.ToSlash(clean), "/")
	if parts[0] != "harness-home" {
		return true
	}
	if len(parts) == 1 {
		return entry.IsDir()
	}
	if parts[1] != "sessions" {
		return false
	}
	if entry.IsDir() {
		return true
	}
	return entry.Name() == "session.jsonl.zstd"
}

func writeJSON(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return writeBytes(path, append(data, '\n'))
}

func writeText(path, value string) error { return writeBytes(path, []byte(value+"\n")) }

func writeBytes(path string, value []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(value)
	return err
}

func ParseRequest(reader io.Reader) (workerprotocol.Request, error) {
	var request workerprotocol.Request
	err := json.NewDecoder(io.LimitReader(reader, 8<<20)).Decode(&request)
	return request, err
}

func WriteResponse(writer io.Writer, response workerprotocol.Response) error {
	return json.NewEncoder(writer).Encode(response)
}

func ExitCode(response workerprotocol.Response) int {
	if response.Status == contract.StatusCompleted {
		return 0
	}
	return 1
}
