// Package sandboxruntime supplies the protected container lifetime and confined
// artifact reader used by the OpenSandbox image. It holds no platform credentials.
package sandboxruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const Root = "/run/rank"

// ArtifactChunk frames binary bytes below the remote command event size limit.
// The final chunk supplies size and digest; readers reject incomplete transfers.
type ArtifactChunk struct {
	Data     []byte `json:"data,omitempty"`
	Complete bool   `json:"complete,omitempty"`
	Size     int64  `json:"size,omitempty"`
	SHA256   string `json:"sha256,omitempty"`
}

// StreamArtifact opens within root, rejects special files, and bounds bytes read
// even if another process grows the file after its initial stat.
func StreamArtifact(out io.Writer, root, name string, limit int64) error {
	if limit < 1 {
		return errors.New("positive artifact limit required")
	}
	f, err := os.OpenInRoot(root, name)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return errors.New("artifact is not a bounded regular file")
	}
	encoder := json.NewEncoder(out)
	hash := sha256.New()
	var count int64
	buffer := make([]byte, 32<<10)
	for {
		n, err := f.Read(buffer)
		count += int64(n)
		if count > limit {
			return errors.New("artifact grew beyond its size limit")
		}
		if n > 0 {
			hash.Write(buffer[:n])
			if err := encoder.Encode(ArtifactChunk{Data: buffer[:n]}); err != nil {
				return err
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
	}
	return encoder.Encode(ArtifactChunk{Complete: true, Size: count, SHA256: hex.EncodeToString(hash.Sum(nil))})
}

// Init is the container's root PID 1. Its absolute deadline survives worker and
// lifecycle-service loss. The unprivileged Harness cannot signal or rewrite it.
// Exiting PID 1 lets the kernel terminate every process in the PID namespace.
func Init(ctx context.Context, expires time.Time, maxLifetime time.Duration) error {
	if os.Getpid() != 1 || os.Geteuid() != 0 {
		return errors.New("sandbox init requires root PID 1")
	}
	remaining := time.Until(expires)
	if remaining <= 0 || remaining > maxLifetime {
		return errors.New("sandbox deadline is expired or exceeds its configured lifetime")
	}
	if os.Getenv("EXECD_ACCESS_TOKEN") == "" {
		return errors.New("sandbox daemon authentication is required")
	}
	for _, name := range []string{"input", "workspace", "artifacts"} {
		p := filepath.Join(Root, name)
		if err := os.MkdirAll(p, 0700); err != nil {
			return err
		}
		if err := os.Chown(p, 1000, 1000); err != nil {
			return err
		}
	}
	// /run/rank permits traversal, while each child's mode protects its contents.
	if err := os.Chmod(Root, 0755); err != nil {
		return err
	}
	command := exec.Command("/opt/opensandbox/execd")
	command.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=/root", "EXECD_ACCESS_TOKEN=" + os.Getenv("EXECD_ACCESS_TOKEN")}
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	if err := command.Start(); err != nil {
		return fmt.Errorf("start sandbox daemon: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	timer := time.NewTimer(time.Until(expires))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return context.DeadlineExceeded
	case err := <-done:
		if err != nil {
			return err
		}
		return errors.New("sandbox daemon stopped")
	}
}
