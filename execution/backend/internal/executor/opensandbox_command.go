package executor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strconv"
	"strings"
	"time"

	sandbox "github.com/alibaba/OpenSandbox/sdks/sandbox/go"
	"github.com/klauspost/compress/zstd"
	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/execution/backend/internal/sandboxruntime"
)

// sandboxCommandExit is a completed nonzero process exit, not a transport error.
type sandboxCommandExit int

func (code sandboxCommandExit) Error() string {
	return fmt.Sprintf("sandbox command exited with status %d", code)
}

func (e *OpenSandbox) command(ctx context.Context, client *sandbox.ExecdClient, command string, output func(string, string) error) error {
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) < time.Millisecond {
		return context.DeadlineExceeded
	}
	uid := int32(1000)
	complete := false
	var exitErr error
	err := client.RunCommand(ctx, sandbox.RunCommandRequest{Command: command, UID: &uid, GID: &uid, Timeout: time.Until(deadline).Milliseconds(), Cwd: sandboxruntime.Root + "/workspace", Envs: map[string]string{
		"EXECD_ACCESS_TOKEN": "", "HOME": sandboxruntime.Root + "/workspace", "PATH": "/usr/local/bin:/usr/bin:/bin", "EXECUTION_REPO_ROOT": "/opt/rank", "DSH_TELEMETRY_DISABLED": "1",
	}}, func(event sandbox.StreamEvent) error {
		var payload struct {
			Type  string `json:"type"`
			Text  string `json:"text"`
			Error struct {
				Name  string `json:"ename"`
				Value string `json:"evalue"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(event.Data), &payload); err != nil {
			return err
		}
		// Transport keepalives may outlive the command terminal frame.
		if payload.Type == "ping" {
			return nil
		}
		if complete {
			return errors.New("sandbox command emitted data after termination")
		}
		switch payload.Type {
		case "stdout", "stderr":
			return output(payload.Type, payload.Text+"\n")
		case "error":
			code, err := strconv.Atoi(payload.Error.Value)
			if payload.Error.Name != "CommandExecError" || err != nil || code < 1 || code > 255 {
				return errors.New("sandbox command reported failure without a normal exit status")
			}
			complete, exitErr = true, sandboxCommandExit(code)
		case "execution_complete":
			complete = true
		case "init":
		default:
			return errors.New("unsupported sandbox command event")
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !complete {
		return errors.New("sandbox command stream ended without completion")
	}
	return exitErr
}

func (e *OpenSandbox) publishRemoteArtifacts(ctx context.Context, client *sandbox.ExecdClient, execution contract.Execution, result *contract.Result, secrets []string) error {
	id := execution.ID
	pending := result.Artifacts
	result.Artifacts = nil
	if err := e.config.Repository.RecordEvidence(ctx, id, execution.Attempt, *result); err != nil {
		return err
	}
	for _, ref := range pending {
		if ref.ExecutionID != id || !fs.ValidPath(ref.Path) || !strings.HasPrefix(ref.Path, id+"/") || strings.ContainsAny(ref.Path, "\\\x00") {
			return errors.New("sandbox artifact identity or path is invalid")
		}
		var data bytes.Buffer
		complete := false
		command := "exec /usr/local/bin/execution-sandbox -mode artifact -limit " + strconv.FormatInt(e.config.MaxArtifactBytes, 10) + " -path " + shellLiteral(ref.Path)
		err := e.command(ctx, client, command, func(kind, text string) error {
			if kind != "stdout" {
				return nil
			}
			if complete {
				return errors.New("sandbox artifact continued after its digest")
			}
			var chunk sandboxruntime.ArtifactChunk
			if err := json.Unmarshal([]byte(text), &chunk); err != nil {
				return err
			}
			if chunk.Complete {
				digest := sha256.Sum256(data.Bytes())
				if len(chunk.Data) != 0 || chunk.Size != int64(data.Len()) || chunk.SHA256 != hex.EncodeToString(digest[:]) {
					return errors.New("sandbox artifact digest differs")
				}
				complete = true
				return nil
			}
			if int64(data.Len()+len(chunk.Data)) > e.config.MaxArtifactBytes {
				return errors.New("sandbox artifact exceeds size limit")
			}
			data.Write(chunk.Data)
			return nil
		})
		if err != nil || !complete {
			return errors.New("sandbox artifact transfer failed")
		}
		content, err := e.redactArtifact(data.Bytes(), ref.Path, secrets)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(content)
		ref.SHA256 = hex.EncodeToString(digest[:])
		ref.Size = int64(len(content))
		ref.Path = path.Join(id, "sha256", ref.SHA256, path.Base(ref.Path))
		if err := e.config.ArtifactStore.Put(ctx, ref.Path, bytes.NewReader(content), ref.Size); err != nil {
			return errors.New("sandbox artifact storage failed")
		}
		result.Artifacts = append(result.Artifacts, ref)
		if err := e.config.Repository.RecordEvidence(ctx, id, execution.Attempt, *result); err != nil {
			return err
		}
	}
	return nil
}

func (e *OpenSandbox) redactArtifact(data []byte, name string, secrets []string) ([]byte, error) {
	compressed := strings.HasSuffix(name, ".zstd")
	if compressed {
		decoder, err := zstd.NewReader(nil, zstd.WithDecoderMaxMemory(uint64(e.config.MaxArtifactBytes)), zstd.WithDecodeAllCapLimit(true))
		if err != nil {
			return nil, err
		}
		decoded, err := decoder.DecodeAll(data, make([]byte, 0, int(e.config.MaxArtifactBytes)))
		decoder.Close()
		if err != nil {
			return nil, errors.New("sandbox history artifact cannot be decoded within its size limit")
		}
		data = decoded
	}
	data = []byte(redactJSON(redact(string(data), secrets), secrets))
	if compressed {
		encoder, err := zstd.NewWriter(nil)
		if err != nil {
			return nil, err
		}
		data = encoder.EncodeAll(data, nil)
		encoder.Close()
	}
	if int64(len(data)) > e.config.MaxArtifactBytes {
		return nil, errors.New("sanitized artifact exceeds size limit")
	}
	return data, nil
}

func shellLiteral(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
