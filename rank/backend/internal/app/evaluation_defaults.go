package app

import (
	"context"
	"github.com/xyzbit/agentkiln/rank/backend/internal/domain"
)

func (s *Service) GetAgentEvaluationDefaults(ctx context.Context, familyID string) (domain.AgentEvaluationDefaults, error) {
	versions, err := s.repo.ListAgentVersions(ctx, familyID)
	if err != nil {
		return domain.AgentEvaluationDefaults{}, err
	}
	if len(versions) == 0 {
		return domain.AgentEvaluationDefaults{}, domain.ErrNotFound
	}
	return s.repo.GetAgentEvaluationDefaults(ctx, familyID)
}

func (s *Service) SaveAgentEvaluationDefaults(ctx context.Context, familyID string, value domain.AgentEvaluationDefaults, expected int64) (domain.AgentEvaluationDefaults, error) {
	err := s.repo.WithinTx(ctx, func(ctx context.Context, repo domain.Repository) error {
		versions, err := repo.ListAgentVersions(ctx, familyID)
		if err != nil {
			return err
		}
		if len(versions) == 0 {
			return domain.ErrNotFound
		}
		old, err := repo.GetAgentEvaluationDefaults(ctx, familyID)
		if err != nil {
			return err
		}
		if expected < 0 || old.Revision != expected {
			return domain.ErrConflict
		}
		for _, id := range []string{value.DevelopmentDatasetID, value.RegressionDatasetID} {
			if id != "" {
				if _, err := latestDatasetVersion(ctx, repo, id); err != nil {
					return err
				}
			}
		}
		value.Revision = old.Revision + 1
		return repo.SaveAgentEvaluationDefaults(ctx, familyID, value, expected)
	})
	return value, err
}

func latestDatasetVersion(ctx context.Context, repo domain.Repository, familyID string) (string, error) {
	if familyID == "" {
		return "", nil
	}
	families, err := repo.ListDatasetFamilies(ctx)
	if err != nil {
		return "", err
	}
	for _, family := range families {
		if family.ID == familyID {
			return family.LatestVersionID, nil
		}
	}
	return "", problem(404, "dataset_not_found", "默认测试集不存在")
}

// Existing workspace selection wins over a family default when switching versions.
func applyDevelopmentDefault(ctx context.Context, repo domain.Repository, experiment *domain.Experiment, agent domain.AgentVersion) error {
	if experiment.DatasetVersionID != "" {
		return nil
	}
	defaults, err := repo.GetAgentEvaluationDefaults(ctx, agent.FamilyID)
	if err != nil {
		return err
	}
	experiment.DatasetVersionID, err = latestDatasetVersion(ctx, repo, defaults.DevelopmentDatasetID)
	return err
}
