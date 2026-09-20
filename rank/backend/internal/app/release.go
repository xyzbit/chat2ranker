package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/xyzbit/agentkiln/rank/backend/internal/domain"
)

var deploymentNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

const (
	DeploymentAliasLatest     = "latest"
	DeploymentAliasStaging    = "staging"
	DeploymentAliasProduction = "production"
)

type CreateDeploymentInput struct {
	TenantID      string
	AgentFamilyID string
	Name          string
	PolicyDigest  string
	Policy        domain.DeploymentPolicy
}

type PromoteDeploymentInput struct {
	DeploymentID              domain.DeploymentID
	Alias                     string
	AgentVersionID            string
	ExpectedReleaseRevisionID domain.ReleaseRevisionID
	EvidenceRunID             string
	PolicyResult              string
	Actor                     string
	Reason                    string
}

func (s *Service) ListDeployments(ctx context.Context, tenantID string) ([]domain.Deployment, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		tenantID = s.tenantID
	}
	return s.repo.ListDeployments(ctx, tenantID)
}

func (s *Service) GetDeployment(ctx context.Context, id domain.DeploymentID) (domain.Deployment, error) {
	return s.repo.GetDeployment(ctx, id)
}

func newServingID(prefix string) string {
	value, err := uuid.NewV7()
	if err != nil {
		panic(err)
	}
	return prefix + "_" + value.String()
}

// CreateDeployment creates one stable serving target and its standard routing
// aliases. The latest alias starts at the family's current immutable version.
func (s *Service) CreateDeployment(ctx context.Context, input CreateDeploymentInput) (domain.Deployment, error) {
	input.TenantID = strings.TrimSpace(input.TenantID)
	input.Name = strings.TrimSpace(input.Name)
	input.PolicyDigest = strings.TrimSpace(input.PolicyDigest)
	if input.TenantID == "" {
		return domain.Deployment{}, problem(400, "invalid_tenant", "Deployment 缺少租户")
	}
	if !deploymentNamePattern.MatchString(input.Name) {
		return domain.Deployment{}, problem(400, "invalid_deployment_name", "Deployment 名称必须以小写字母开头，且只能包含小写字母、数字和连字符")
	}
	if input.Policy.RequestsPerMinute < 1 || input.Policy.MaxConcurrentResponses < 1 || input.Policy.ExecutionTimeoutMs < 1 {
		return domain.Deployment{}, problem(400, "invalid_deployment_policy", "Deployment 策略必须设置正数的每分钟请求数、并发数和执行超时")
	}
	if input.PolicyDigest == "" {
		payload, err := json.Marshal(input.Policy)
		if err != nil {
			return domain.Deployment{}, err
		}
		digest := sha256.Sum256(payload)
		input.PolicyDigest = "sha256:" + hex.EncodeToString(digest[:])
	}
	now := s.now().UTC()
	created := domain.Deployment{
		ID: domain.DeploymentID(newServingID("deploy")), TenantID: input.TenantID,
		AgentFamilyID: input.AgentFamilyID, Name: input.Name,
		PolicyDigest: input.PolicyDigest, Policy: input.Policy, CreatedAt: now,
	}
	aliases := make([]domain.DeploymentAlias, 0, 3)
	for _, name := range []string{DeploymentAliasLatest, DeploymentAliasStaging, DeploymentAliasProduction} {
		aliases = append(aliases, domain.DeploymentAlias{
			ID: domain.DeploymentAliasID(newServingID("alias")), DeploymentID: created.ID, Name: name,
			CreatedAt: now, UpdatedAt: now,
		})
	}
	err := s.repo.WithinTx(ctx, func(repo domain.Repository) error {
		families, err := repo.ListAgentFamilies(ctx)
		if err != nil {
			return err
		}
		var family *domain.AgentFamily
		for index := range families {
			if families[index].ID == input.AgentFamilyID {
				family = &families[index]
				break
			}
		}
		if family == nil {
			return problem(404, "agent_family_not_found", "Agent 资产不存在")
		}
		version, err := repo.GetAgentVersion(ctx, family.LatestVersionID)
		if err != nil {
			return err
		}
		if err := repo.CreateDeployment(ctx, created, aliases); err != nil {
			return err
		}
		_, err = repo.AppendReleaseRevision(ctx, domain.ReleaseRevision{
			ID: domain.ReleaseRevisionID(newServingID("rel")), DeploymentAliasID: aliases[0].ID,
			AgentVersionID: version.ID, PolicyResult: domain.ReleasePolicySystem,
			Actor: "system", Reason: "agent_version_published", CreatedAt: now,
		}, "")
		if err != nil {
			return err
		}
		created, err = repo.GetDeployment(ctx, created.ID)
		return err
	})
	return created, err
}

// PromoteDeploymentAlias accepts a decision from the trusted release
// controller, appends one auditable revision, and advances the alias only when
// its current revision matches the caller's expected value.
func (s *Service) PromoteDeploymentAlias(ctx context.Context, input PromoteDeploymentInput) (domain.ReleaseRevision, error) {
	input.Alias = strings.TrimSpace(input.Alias)
	input.Actor = strings.TrimSpace(input.Actor)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.Alias == DeploymentAliasLatest {
		return domain.ReleaseRevision{}, problem(400, "latest_alias_managed", "latest 由 Agent 版本发布自动维护")
	}
	if input.Actor == "" {
		return domain.ReleaseRevision{}, problem(400, "invalid_release_actor", "发布操作缺少操作者")
	}
	if input.PolicyResult != domain.ReleasePolicyPassed && input.PolicyResult != domain.ReleasePolicyManualOverride {
		return domain.ReleaseRevision{}, problem(400, "invalid_policy_result", "发布结果必须是 passed 或 manual_override")
	}
	if input.PolicyResult == domain.ReleasePolicyPassed && input.EvidenceRunID == "" {
		return domain.ReleaseRevision{}, problem(400, "release_evidence_required", "策略通过的发布必须引用评测 Run")
	}
	if input.PolicyResult == domain.ReleasePolicyManualOverride && input.Reason == "" {
		return domain.ReleaseRevision{}, problem(400, "release_override_reason_required", "人工覆盖必须填写原因")
	}
	var released domain.ReleaseRevision
	err := s.repo.WithinTx(ctx, func(repo domain.Repository) error {
		deployment, err := repo.GetDeployment(ctx, input.DeploymentID)
		if err != nil {
			return err
		}
		alias, err := repo.GetDeploymentAlias(ctx, deployment.ID, input.Alias)
		if err != nil {
			return err
		}
		version, err := repo.GetAgentVersion(ctx, input.AgentVersionID)
		if err != nil {
			return err
		}
		if version.FamilyID != deployment.AgentFamilyID {
			return problem(400, "release_agent_family_mismatch", "目标 Agent 版本不属于 Deployment 的 Agent 资产")
		}
		if input.PolicyResult == domain.ReleasePolicyPassed {
			run, err := repo.GetRun(ctx, input.EvidenceRunID)
			if err != nil {
				return err
			}
			if run.Status != domain.RunComplete || !run.EvaluationComplete || run.AgentSnapshot.ID != version.ID {
				return problem(400, "release_evidence_invalid", "评测 Run 未完成、评测不完整或未评测目标 Agent 版本")
			}
		}
		released, err = repo.AppendReleaseRevision(ctx, domain.ReleaseRevision{
			ID: domain.ReleaseRevisionID(newServingID("rel")), DeploymentAliasID: alias.ID,
			AgentVersionID: version.ID, EvidenceRunID: input.EvidenceRunID,
			PolicyResult: input.PolicyResult, Actor: input.Actor, Reason: input.Reason,
			CreatedAt: s.now().UTC(),
		}, input.ExpectedReleaseRevisionID)
		return err
	})
	return released, err
}

// ResolveDeploymentReference resolves an exact version or alias once. Callers
// persist the returned version and optional release revision before execution.
func (s *Service) ResolveDeploymentReference(ctx context.Context, tenantID, reference string) (domain.DeploymentResolution, error) {
	separator := strings.LastIndex(reference, "@")
	if separator <= 0 || separator == len(reference)-1 {
		return domain.DeploymentResolution{}, problem(400, "invalid_agent_reference", "Agent 引用必须是 <deployment>@<version-or-alias>")
	}
	name, selector := reference[:separator], reference[separator+1:]
	deployment, err := s.repo.GetDeploymentByName(ctx, strings.TrimSpace(tenantID), name)
	if err != nil {
		return domain.DeploymentResolution{}, err
	}
	resolution := domain.DeploymentResolution{Deployment: deployment, RequestedRef: reference}
	if strings.HasPrefix(selector, "v") {
		number, parseErr := strconv.Atoi(strings.TrimPrefix(selector, "v"))
		if parseErr != nil || number < 1 {
			return domain.DeploymentResolution{}, problem(400, "invalid_agent_version", "Agent 精确版本必须使用 v<正整数>")
		}
		versions, err := s.repo.ListAgentVersions(ctx, deployment.AgentFamilyID)
		if err != nil {
			return domain.DeploymentResolution{}, err
		}
		for _, version := range versions {
			if version.Version == number {
				resolution.Kind = domain.ResolutionExact
				resolution.AgentVersion = version
				return resolution, nil
			}
		}
		return domain.DeploymentResolution{}, domain.ErrNotFound
	}
	alias, err := s.repo.GetDeploymentAlias(ctx, deployment.ID, selector)
	if err != nil {
		return domain.DeploymentResolution{}, err
	}
	if alias.CurrentRelease == nil {
		return domain.DeploymentResolution{}, domain.ErrNotFound
	}
	version, err := s.repo.GetAgentVersion(ctx, alias.CurrentRelease.AgentVersionID)
	if err != nil {
		return domain.DeploymentResolution{}, err
	}
	if version.FamilyID != deployment.AgentFamilyID {
		return domain.DeploymentResolution{}, fmt.Errorf("deployment alias resolved outside its Agent family: %w", domain.ErrConflict)
	}
	release := *alias.CurrentRelease
	resolution.Kind = domain.ResolutionAlias
	resolution.Alias = &alias
	resolution.ReleaseRevision = &release
	resolution.AgentVersion = version
	return resolution, nil
}

func (s *Service) syncLatestAliases(ctx context.Context, repo domain.Repository, version domain.AgentVersion) error {
	deployments, err := repo.ListDeploymentsByAgentFamily(ctx, version.FamilyID)
	if err != nil {
		return err
	}
	for _, deployment := range deployments {
		alias, err := repo.GetDeploymentAlias(ctx, deployment.ID, DeploymentAliasLatest)
		if err != nil {
			return err
		}
		_, err = repo.AppendReleaseRevision(ctx, domain.ReleaseRevision{
			ID: domain.ReleaseRevisionID(newServingID("rel")), DeploymentAliasID: alias.ID,
			AgentVersionID: version.ID, PolicyResult: domain.ReleasePolicySystem,
			Actor: "system", Reason: "agent_version_published", CreatedAt: version.CreatedAt,
		}, alias.CurrentReleaseRevisionID)
		if err != nil {
			return err
		}
	}
	return nil
}
