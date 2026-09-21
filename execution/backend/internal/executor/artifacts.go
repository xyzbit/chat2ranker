package executor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
)

func (executor *Local) publishArtifacts(ctx context.Context, execution contract.Execution, result *contract.Result) error {
	pending := result.Artifacts
	result.Artifacts = nil
	if err := executor.config.Repository.RecordEvidence(ctx, execution.ID, execution.Attempt, *result); err != nil {
		return err
	}
	for _, ref := range pending {
		published, err := executor.publishArtifact(ctx, ref)
		if err != nil {
			return fmt.Errorf("artifact upload failed: %w", err)
		}
		result.Artifacts = append(result.Artifacts, published)
		if err := executor.config.Repository.RecordEvidence(ctx, execution.ID, execution.Attempt, *result); err != nil {
			return err
		}
	}
	return nil
}

func (executor *Local) publishArtifact(ctx context.Context, ref contract.ArtifactRef) (contract.ArtifactRef, error) {
	file, err := os.OpenInRoot(executor.config.ArtifactRoot, ref.Path)
	if err != nil {
		return ref, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return ref, err
	}
	if !info.Mode().IsRegular() || info.Size() > executor.config.MaxArtifactBytes {
		return ref, fmt.Errorf("artifact must be a regular file no larger than %d bytes", executor.config.MaxArtifactBytes)
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return ref, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return ref, err
	}
	ref.Size, ref.SHA256 = info.Size(), hex.EncodeToString(hash.Sum(nil))
	ref.Path = path.Join(ref.ExecutionID, "sha256", ref.SHA256, path.Base(ref.Path))
	if err := executor.config.ArtifactStore.Put(ctx, ref.Path, file, info.Size()); err != nil {
		return ref, err
	}
	return ref, nil
}
