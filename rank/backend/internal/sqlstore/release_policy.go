package sqlstore

import (
	"context"
	"database/sql"
	"errors"

	"github.com/xyzbit/agentkiln/rank/backend/internal/domain"
)

func (s *Store) GetReleasePolicy(ctx context.Context, id domain.DeploymentID) (domain.ReleasePolicy, error) {
	var payload string
	err := s.q.QueryRowContext(ctx, `SELECT p.policy_json FROM release_policies p JOIN deployments d ON d.id=p.deployment_id WHERE d.public_id=$1`, id).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ReleasePolicy{}, nil
	}
	if err != nil {
		return domain.ReleasePolicy{}, err
	}
	var policy domain.ReleasePolicy
	err = decode(payload, &policy)
	return policy, err
}

func (s *Store) SaveReleasePolicy(ctx context.Context, id domain.DeploymentID, policy domain.ReleasePolicy, expectedRevision int64) error {
	result, err := s.q.ExecContext(ctx, `
		INSERT INTO release_policies(deployment_id,revision,policy_json)
		SELECT id,$1,$2 FROM deployments WHERE public_id=$3 AND $4=0
		ON CONFLICT(deployment_id) DO NOTHING`, policy.Revision, encode(policy), id, expectedRevision)
	if err != nil {
		return translate(err)
	}
	if expectedRevision != 0 {
		result, err = s.q.ExecContext(ctx, `UPDATE release_policies SET revision=$1,policy_json=$2 WHERE deployment_id=(SELECT id FROM deployments WHERE public_id=$3) AND revision=$4`, policy.Revision, encode(policy), id, expectedRevision)
		if err != nil {
			return translate(err)
		}
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return domain.ErrConflict
	}
	return nil
}

func (s *Store) AppendReleaseDecision(ctx context.Context, decision domain.ReleaseDecision) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO release_decisions(public_id,deployment_id,alias,decision_json) VALUES($1,(SELECT id FROM deployments WHERE public_id=$2),$3,$4)`, decision.ID, decision.DeploymentID, decision.Alias, encode(decision))
	return translate(err)
}

func (s *Store) ListReleaseDecisions(ctx context.Context, id domain.DeploymentID, alias string, before int64, limit int) (domain.ReleaseDecisionPage, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT a.id,a.decision_json FROM release_decisions a JOIN deployments d ON d.id=a.deployment_id WHERE d.public_id=$1 AND a.alias=$2 AND (CAST($3 AS BIGINT)=0 OR a.id<$4) ORDER BY a.id DESC LIMIT $5`, id, alias, before, before, limit+1)
	if err != nil {
		return domain.ReleaseDecisionPage{}, err
	}
	defer rows.Close()
	page := domain.ReleaseDecisionPage{Items: []domain.ReleaseDecision{}}
	var cursor int64
	for rows.Next() {
		var rowID int64
		var payload string
		if err := rows.Scan(&rowID, &payload); err != nil {
			return page, err
		}
		if len(page.Items) == limit {
			page.NextCursor = cursor
			break
		}
		var decision domain.ReleaseDecision
		if err := decode(payload, &decision); err != nil {
			return page, err
		}
		page.Items = append(page.Items, decision)
		cursor = rowID
	}
	return page, rows.Err()
}
