package app

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const artifactReadLimit = 1 << 20

type ArtifactContent struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
}

func (s *Service) ReadArtifact(ctx context.Context, runID, caseID, artifactPath string) (ArtifactContent, error) {
	if s.artifactRoot == "" {
		return ArtifactContent{}, problem(503, "artifact_store_unavailable", "Artifact Store 未配置")
	}
	run, err := s.repo.GetRun(ctx, runID)
	if err != nil {
		return ArtifactContent{}, mapNotFound(err, "run_not_found", "运行不存在")
	}
	authorized := false
	for _, result := range run.Results {
		if result.CaseID != caseID {
			continue
		}
		for _, artifact := range result.Artifacts {
			if artifact.Path == artifactPath {
				authorized = true
				break
			}
		}
	}
	if !authorized {
		return ArtifactContent{}, problem(404, "artifact_not_found", "运行产物不存在")
	}
	root, err := filepath.EvalSymlinks(s.artifactRoot)
	if err != nil {
		return ArtifactContent{}, err
	}
	target, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(artifactPath)))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ArtifactContent{}, problem(404, "artifact_not_found", "运行产物不存在")
		}
		return ArtifactContent{}, err
	}
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return ArtifactContent{}, problem(403, "artifact_path_rejected", "运行产物路径越界")
	}
	file, err := os.Open(target)
	if err != nil {
		return ArtifactContent{}, err
	}
	defer file.Close()
	buffer, err := io.ReadAll(io.LimitReader(file, artifactReadLimit+1))
	if err != nil {
		return ArtifactContent{}, err
	}
	truncated := len(buffer) > artifactReadLimit
	if truncated {
		buffer = buffer[:artifactReadLimit]
	}
	return ArtifactContent{Path: artifactPath, Content: string(buffer), Truncated: truncated}, nil
}
