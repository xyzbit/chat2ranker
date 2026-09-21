package sqlstore

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/xyzbit/agentkiln/rank/backend/internal/domain"
)

type storedDeployment struct {
	dbID  int64
	value domain.Deployment
}

func (s *Store) CreateDeployment(ctx context.Context, deployment domain.Deployment, aliases []domain.DeploymentAlias) error {
	var deploymentDBID int64
	err := s.q.QueryRowContext(ctx, `
		INSERT INTO deployments(public_id,tenant_id,agent_family_id,name,policy_digest,policy_json,created_at)
		VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
		deployment.ID, deployment.TenantID, deployment.AgentFamilyID, deployment.Name,
		deployment.PolicyDigest, encode(deployment.Policy), stamp(deployment.CreatedAt),
	).Scan(&deploymentDBID)
	if err != nil {
		return translate(err)
	}
	for _, alias := range aliases {
		if _, err := s.q.ExecContext(ctx, `
			INSERT INTO deployment_aliases(public_id,deployment_id,name,created_at,updated_at)
			VALUES($1,$2,$3,$4,$5)`,
			alias.ID, deploymentDBID, alias.Name, stamp(alias.CreatedAt), stamp(alias.UpdatedAt),
		); err != nil {
			return translate(err)
		}
	}
	return nil
}

func (s *Store) GetDeployment(ctx context.Context, id domain.DeploymentID) (domain.Deployment, error) {
	stored, err := s.loadDeployment(ctx, `
		SELECT id,public_id,tenant_id,agent_family_id,name,policy_digest,policy_json,created_at
		FROM deployments WHERE public_id=$1`, id)
	return stored.value, err
}

func (s *Store) GetDeploymentByName(ctx context.Context, tenantID, name string) (domain.Deployment, error) {
	stored, err := s.loadDeployment(ctx, `
		SELECT id,public_id,tenant_id,agent_family_id,name,policy_digest,policy_json,created_at
		FROM deployments WHERE tenant_id=$1 AND name=$2`, tenantID, name)
	return stored.value, err
}

func (s *Store) loadDeployment(ctx context.Context, query string, args ...any) (storedDeployment, error) {
	var stored storedDeployment
	var policyJSON, created string
	err := s.q.QueryRowContext(ctx, query, args...).Scan(
		&stored.dbID, &stored.value.ID, &stored.value.TenantID, &stored.value.AgentFamilyID,
		&stored.value.Name, &stored.value.PolicyDigest, &policyJSON, &created,
	)
	if err != nil {
		return stored, translate(err)
	}
	stored.value.CreatedAt, err = parseStamp(created)
	if err != nil {
		return stored, err
	}
	if err := decode(policyJSON, &stored.value.Policy); err != nil {
		return stored, err
	}
	stored.value.Aliases, err = s.listDeploymentAliases(ctx, stored.dbID, stored.value.ID)
	return stored, err
}

func (s *Store) ListDeployments(ctx context.Context, tenantID string) ([]domain.Deployment, error) {
	return s.listDeployments(ctx, `
		SELECT id,public_id,tenant_id,agent_family_id,name,policy_digest,policy_json,created_at
		FROM deployments WHERE tenant_id=$1 ORDER BY id`, tenantID)
}

func (s *Store) ListDeploymentsByAgentFamily(ctx context.Context, familyID string) ([]domain.Deployment, error) {
	return s.listDeployments(ctx, `
		SELECT id,public_id,tenant_id,agent_family_id,name,policy_digest,policy_json,created_at
		FROM deployments WHERE agent_family_id=$1 ORDER BY id`, familyID)
}

// ListDeploymentsByAgentVersion includes historical releases, newest release first.
func (s *Store) ListDeploymentsByAgentVersion(ctx context.Context, tenantID, versionID string) ([]domain.Deployment, error) {
	return s.listDeployments(ctx, `
		SELECT d.id,d.public_id,d.tenant_id,d.agent_family_id,d.name,d.policy_digest,d.policy_json,d.created_at
		FROM deployments d
		JOIN deployment_aliases a ON a.deployment_id=d.id
		JOIN release_revisions r ON r.deployment_alias_id=a.id
		WHERE d.tenant_id=$1 AND r.agent_version_id=$2
		GROUP BY d.id ORDER BY MAX(r.id) DESC`, tenantID, versionID)
}

func (s *Store) listDeployments(ctx context.Context, query string, arguments ...any) ([]domain.Deployment, error) {
	rows, err := s.q.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, err
	}
	stored := []storedDeployment{}
	for rows.Next() {
		var item storedDeployment
		var policyJSON, created string
		if err := rows.Scan(&item.dbID, &item.value.ID, &item.value.TenantID, &item.value.AgentFamilyID, &item.value.Name, &item.value.PolicyDigest, &policyJSON, &created); err != nil {
			return nil, err
		}
		if err := decode(policyJSON, &item.value.Policy); err != nil {
			return nil, err
		}
		item.value.CreatedAt, err = parseStamp(created)
		if err != nil {
			return nil, err
		}
		stored = append(stored, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	result := make([]domain.Deployment, len(stored))
	for index := range stored {
		stored[index].value.Aliases, err = s.listDeploymentAliases(ctx, stored[index].dbID, stored[index].value.ID)
		if err != nil {
			return nil, err
		}
		result[index] = stored[index].value
	}
	return result, nil
}

func (s *Store) listDeploymentAliases(ctx context.Context, deploymentDBID int64, deploymentID domain.DeploymentID) ([]domain.DeploymentAlias, error) {
	rows, err := s.q.QueryContext(ctx, `
		SELECT a.public_id,a.name,COALESCE(current.public_id,''),a.created_at,a.updated_at,
		       COALESCE(current.revision,0),COALESCE(current.agent_version_id,''),
		       COALESCE(previous.public_id,''),COALESCE(current.evidence_run_id,''),
		       COALESCE(current.policy_result,''),COALESCE(current.actor,''),
		       COALESCE(current.reason,''),COALESCE(current.created_at,'')
		FROM deployment_aliases a
		LEFT JOIN release_revisions current ON current.id=a.current_release_revision_id
		LEFT JOIN release_revisions previous ON previous.id=current.previous_release_revision_id
		WHERE a.deployment_id=$1 ORDER BY a.id`, deploymentDBID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	aliases := []domain.DeploymentAlias{}
	for rows.Next() {
		alias, err := scanDeploymentAlias(rows.Scan, deploymentID)
		if err != nil {
			return nil, err
		}
		aliases = append(aliases, alias)
	}
	return aliases, rows.Err()
}

func (s *Store) GetDeploymentAlias(ctx context.Context, deploymentID domain.DeploymentID, name string) (domain.DeploymentAlias, error) {
	row := s.q.QueryRowContext(ctx, `
		SELECT a.public_id,a.name,COALESCE(current.public_id,''),a.created_at,a.updated_at,
		       COALESCE(current.revision,0),COALESCE(current.agent_version_id,''),
		       COALESCE(previous.public_id,''),COALESCE(current.evidence_run_id,''),
		       COALESCE(current.policy_result,''),COALESCE(current.actor,''),
		       COALESCE(current.reason,''),COALESCE(current.created_at,'')
		FROM deployment_aliases a
		JOIN deployments d ON d.id=a.deployment_id
		LEFT JOIN release_revisions current ON current.id=a.current_release_revision_id
		LEFT JOIN release_revisions previous ON previous.id=current.previous_release_revision_id
		WHERE d.public_id=$1 AND a.name=$2`, deploymentID, name)
	alias, err := scanDeploymentAlias(row.Scan, deploymentID)
	return alias, translate(err)
}

func scanDeploymentAlias(scan scanFunc, deploymentID domain.DeploymentID) (domain.DeploymentAlias, error) {
	var alias domain.DeploymentAlias
	var created, updated, releaseCreated string
	var release domain.ReleaseRevision
	if err := scan(
		&alias.ID, &alias.Name, &alias.CurrentReleaseRevisionID, &created, &updated,
		&release.Revision, &release.AgentVersionID, &release.PreviousReleaseRevisionID,
		&release.EvidenceRunID, &release.PolicyResult, &release.Actor, &release.Reason, &releaseCreated,
	); err != nil {
		return alias, err
	}
	alias.DeploymentID = deploymentID
	var err error
	alias.CreatedAt, err = parseStamp(created)
	if err != nil {
		return alias, err
	}
	alias.UpdatedAt, err = parseStamp(updated)
	if err != nil {
		return alias, err
	}
	if alias.CurrentReleaseRevisionID != "" {
		release.ID = alias.CurrentReleaseRevisionID
		release.DeploymentAliasID = alias.ID
		release.CreatedAt, err = parseStamp(releaseCreated)
		if err != nil {
			return alias, err
		}
		alias.CurrentRelease = &release
	}
	return alias, nil
}

func (s *Store) AppendReleaseRevision(ctx context.Context, revision domain.ReleaseRevision, expectedReleaseRevisionID domain.ReleaseRevisionID) (domain.ReleaseRevision, error) {
	if s.tx == nil {
		var appended domain.ReleaseRevision
		err := s.WithinTx(ctx, func(ctx context.Context, repo domain.Repository) error {
			var err error
			appended, err = repo.AppendReleaseRevision(ctx, revision, expectedReleaseRevisionID)
			return err
		})
		return appended, err
	}
	var aliasDBID int64
	var currentDBID sql.NullInt64
	var currentPublicID domain.ReleaseRevisionID
	var currentRevision int64
	err := s.q.QueryRowContext(ctx, `
		SELECT a.id,a.current_release_revision_id,COALESCE(current.public_id,''),COALESCE(current.revision,0)
		FROM deployment_aliases a
		LEFT JOIN release_revisions current ON current.id=a.current_release_revision_id
		WHERE a.public_id=$1`, revision.DeploymentAliasID,
	).Scan(&aliasDBID, &currentDBID, &currentPublicID, &currentRevision)
	if err != nil {
		return revision, translate(err)
	}
	if currentPublicID != expectedReleaseRevisionID {
		return revision, domain.ErrConflict
	}
	revision.Revision = currentRevision + 1
	revision.PreviousReleaseRevisionID = currentPublicID
	var revisionDBID int64
	err = s.q.QueryRowContext(ctx, `
		INSERT INTO release_revisions(
			public_id,deployment_alias_id,revision,agent_version_id,previous_release_revision_id,
			evidence_run_id,policy_result,actor,reason,created_at
		) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id`,
		revision.ID, aliasDBID, revision.Revision, revision.AgentVersionID, nullableInt64(currentDBID),
		nullableText(revision.EvidenceRunID), revision.PolicyResult, revision.Actor, revision.Reason,
		stamp(revision.CreatedAt),
	).Scan(&revisionDBID)
	if err != nil {
		return revision, translate(err)
	}
	var result sql.Result
	if currentDBID.Valid {
		result, err = s.q.ExecContext(ctx, `
			UPDATE deployment_aliases SET current_release_revision_id=$1,updated_at=$2
			WHERE id=$3 AND current_release_revision_id=$4`,
			revisionDBID, stamp(revision.CreatedAt), aliasDBID, currentDBID.Int64)
	} else {
		result, err = s.q.ExecContext(ctx, `
			UPDATE deployment_aliases SET current_release_revision_id=$1,updated_at=$2
			WHERE id=$3 AND current_release_revision_id IS NULL`,
			revisionDBID, stamp(revision.CreatedAt), aliasDBID)
	}
	if err != nil {
		return revision, translate(err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return revision, fmt.Errorf("read deployment alias update count: %w", err)
	}
	if changed != 1 {
		return revision, domain.ErrConflict
	}
	return revision, nil
}

func nullableInt64(value sql.NullInt64) any {
	if !value.Valid {
		return nil
	}
	return value.Int64
}
