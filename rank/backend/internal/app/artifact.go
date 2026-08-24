package app

import (
	"context"

	"github.com/xyzbit/chat2ranker/rank/backend/internal/domain"
)

type ArtifactContent struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
}

// ArtifactReader is the Rank-side port for execution-owned artifacts.
type ArtifactReader interface {
	ReadArtifact(context.Context, string, string) (ArtifactContent, error)
}

func (s *Service) ReadArtifact(ctx context.Context, runID, caseID, artifactPath string) (ArtifactContent, error) {
	if s.artifacts == nil {
		return ArtifactContent{}, problem(503, "artifact_store_unavailable", "Execution Service 未配置")
	}
	run, err := s.repo.GetRun(ctx, runID)
	if err != nil {
		return ArtifactContent{}, mapNotFound(err, "run_not_found", "运行不存在")
	}
	var selected *domain.ArtifactRef
	for _, result := range run.Results {
		if result.CaseID != caseID {
			continue
		}
		for index := range result.Artifacts {
			if result.Artifacts[index].Path == artifactPath {
				selected = &result.Artifacts[index]
				break
			}
		}
	}
	if selected == nil {
		return ArtifactContent{}, problem(404, "artifact_not_found", "运行产物不存在")
	}
	return s.artifacts.ReadArtifact(ctx, selected.ExecutionID, selected.Path)
}
