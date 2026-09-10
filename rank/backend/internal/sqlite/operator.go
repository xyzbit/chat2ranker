package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/xyzbit/chat2ranker/rank/backend/internal/domain"
)

// ListReleaseHistory uses the alias/revision index and does not load execution artifacts.
func (s *Store) ListReleaseHistory(ctx context.Context, aliasID domain.DeploymentAliasID, before int64, limit int) (domain.ReleaseHistoryPage, error) {
	page := domain.ReleaseHistoryPage{Items: []domain.ReleaseHistoryItem{}}
	where := "WHERE a.public_id=?"
	args := []any{aliasID}
	if before > 0 {
		where += " AND r.revision<?"
		args = append(args, before)
	}
	args = append(args, limit+1)
	rows, err := s.q.QueryContext(ctx, `SELECT r.public_id,r.revision,r.agent_version_id,COALESCE(p.public_id,''),COALESCE(p.agent_version_id,''),COALESCE(r.evidence_run_id,''),r.policy_result,r.actor,r.reason,r.created_at FROM release_revisions r JOIN deployment_aliases a ON a.id=r.deployment_alias_id LEFT JOIN release_revisions p ON p.id=r.previous_release_revision_id `+where+` ORDER BY r.revision DESC LIMIT ?`, args...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var v domain.ReleaseHistoryItem
		var created string
		v.DeploymentAliasID = aliasID
		if err := rows.Scan(&v.ID, &v.Revision, &v.AgentVersionID, &v.PreviousReleaseRevisionID, &v.PreviousAgentVersionID, &v.EvidenceRunID, &v.PolicyResult, &v.Actor, &v.Reason, &created); err != nil {
			return page, err
		}
		v.CreatedAt, err = parseStamp(created)
		if err != nil {
			return page, err
		}
		page.Items = append(page.Items, v)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		page.NextCursor = page.Items[limit-1].Revision
	}
	return page, nil
}

// ListAgentEvaluationSummaries avoids loading other conversations and trial artifacts.
func (s *Store) ListAgentEvaluationSummaries(ctx context.Context, versionID string) ([]domain.AgentEvaluationSummary, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT id,json_extract(dataset_snapshot_json,'$.name'),json_extract(dataset_snapshot_json,'$.version'),total,passed,status,cost,cost_known,created_at FROM runs WHERE json_extract(agent_snapshot_json,'$.id')=? ORDER BY created_at DESC,id DESC`, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.AgentEvaluationSummary{}
	for rows.Next() {
		v := domain.AgentEvaluationSummary{AgentVersionID: versionID}
		var known int
		var created string
		if err := rows.Scan(&v.ID, &v.DatasetName, &v.DatasetVersion, &v.Total, &v.Passed, &v.Status, &v.Cost, &known, &created); err != nil {
			return nil, err
		}
		v.CostKnown = known != 0
		v.CreatedAt, err = parseStamp(created)
		if err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

// ListAPICredentials returns only the public credential projection.
func (s *Store) ListAPICredentials(ctx context.Context, tenantID string) ([]domain.APICredential, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT public_id,tenant_id,name,key_prefix,scopes_json,deployment_ids_json,created_at,revoked_at FROM api_credentials WHERE tenant_id=? ORDER BY id DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.APICredential{}
	for rows.Next() {
		var v domain.APICredential
		var scopes, deployments, created string
		var revoked sql.NullString
		if err := rows.Scan(&v.ID, &v.TenantID, &v.Name, &v.KeyPrefix, &scopes, &deployments, &created, &revoked); err != nil {
			return nil, err
		}
		if err := decode(scopes, &v.Scopes); err != nil {
			return nil, err
		}
		if err := decode(deployments, &v.DeploymentIDs); err != nil {
			return nil, err
		}
		v.CreatedAt, err = parseStamp(created)
		if err != nil {
			return nil, err
		}
		if revoked.Valid {
			at, err := parseStamp(revoked.String)
			if err != nil {
				return nil, err
			}
			v.RevokedAt = &at
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

// RevokeAPICredential is idempotent and preserves the first revocation time.
func (s *Store) RevokeAPICredential(ctx context.Context, id domain.APICredentialID, tenantID string, at time.Time) error {
	result, err := s.q.ExecContext(ctx, `UPDATE api_credentials SET revoked_at=COALESCE(revoked_at,?) WHERE public_id=? AND tenant_id=?`, stamp(at), id, tenantID)
	if err != nil {
		return translate(err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// ListResponsesPage uses integer identity ordering without loading attachment
// blobs or transcripts. The cursor is validated against the selected tenant.
func (s *Store) ListResponsesPage(ctx context.Context, f domain.ResponseListFilter) (domain.ResponsePage, error) {
	page := domain.ResponsePage{Items: []domain.ServingResponse{}}
	where := ` WHERE r.tenant_id=?`
	args := []any{f.TenantID}
	if f.Before != "" {
		var id int64
		if err := s.q.QueryRowContext(ctx, `SELECT id FROM responses WHERE public_id=? AND tenant_id=?`, f.Before, f.TenantID).Scan(&id); err != nil {
			return page, translate(err)
		}
		where += ` AND r.id<?`
		args = append(args, id)
	}
	if f.DeploymentID != "" {
		where += ` AND d.public_id=?`
		args = append(args, f.DeploymentID)
	}
	if f.Status != "" {
		where += ` AND r.status=?`
		args = append(args, f.Status)
	}
	args = append(args, f.Limit+1)
	rows, err := s.q.QueryContext(ctx, `SELECT r.public_id,r.tenant_id,d.public_id,r.deployment_name,r.requested_reference,r.agent_version_id,COALESCE(rev.public_id,''),r.status,r.usage_json,r.cost,r.cost_known,r.created_at,r.started_at,r.completed_at,r.error_code,r.error_message FROM responses r JOIN deployments d ON d.id=r.deployment_id LEFT JOIN release_revisions rev ON rev.id=r.release_revision_id`+where+` ORDER BY r.id DESC LIMIT ?`, args...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var v domain.ServingResponse
		var usage, created, code, message string
		var known int
		var started, completed sql.NullString
		if err := rows.Scan(&v.ID, &v.TenantID, &v.DeploymentID, &v.DeploymentName, &v.RequestedReference, &v.AgentVersionID, &v.ReleaseRevisionID, &v.Status, &usage, &v.Cost, &known, &created, &started, &completed, &code, &message); err != nil {
			return page, err
		}
		if err := decode(usage, &v.Usage); err != nil {
			return page, err
		}
		v.CostKnown = known != 0
		v.CreatedAt, err = parseStamp(created)
		if err != nil {
			return page, err
		}
		if started.Valid {
			at, err := parseStamp(started.String)
			if err != nil {
				return page, err
			}
			v.StartedAt = &at
		}
		if completed.Valid {
			at, err := parseStamp(completed.String)
			if err != nil {
				return page, err
			}
			v.CompletedAt = &at
		}
		if code != "" || message != "" {
			v.Failure = &domain.ResponseFailure{Code: code, Message: message}
		}
		page.Items = append(page.Items, v)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	if len(page.Items) > f.Limit {
		page.Items = page.Items[:f.Limit]
		page.NextCursor = page.Items[len(page.Items)-1].ID
	}
	return page, nil
}

// ReplayWebhookDelivery requeues a failed delivery only while its endpoint is
// enabled. It retains the event ID and payload so receivers can deduplicate.
func (s *Store) ReplayWebhookDelivery(ctx context.Context, endpointID domain.WebhookEndpointID, deliveryID domain.WebhookDeliveryID, at time.Time) error {
	result, err := s.q.ExecContext(ctx, `UPDATE webhook_deliveries SET status='pending',attempts=0,next_attempt_at=?,lease_until=NULL,lease_token='',last_error='' WHERE public_id=? AND status='failed' AND endpoint_id=(SELECT id FROM webhook_endpoints WHERE public_id=? AND enabled=1)`, stamp(at), deliveryID, endpointID)
	if err != nil {
		return translate(err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return domain.ErrConflict
	}
	return nil
}
