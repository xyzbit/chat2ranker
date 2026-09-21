package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/execution/backend/internal/modelquota"
)

// ConfigureModelQuota serializes account membership and limit edits. A connection
// belongs to one account group; moving live work requires draining it first.
func (s *Store) ConfigureModelQuota(ctx context.Context, value contract.ModelQuota) error {
	if err := modelquota.Validate(value); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, s.config.OperationTimeout)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, s.config.Schema+"/admission"); err != nil {
		return err
	}
	var old contract.ModelQuota
	err = tx.QueryRow(ctx, `SELECT body FROM model_quotas WHERE id=$1 FOR UPDATE`, value.ID).Scan(&old)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	oldConnections := slices.Clone(old.Connections)
	newConnections := slices.Clone(value.Connections)
	slices.Sort(oldConnections)
	slices.Sort(newConnections)
	if !slices.Equal(oldConnections, newConnections) {
		var active bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM executions WHERE
 (body->>'status' IN ('queued','running','cancelling') OR body->>'runtimeCleanupPending'='true')
 AND (body->'modelQuota'->>'group'=$1 OR body->'spec'->>'modelConnectionId'=ANY($2)))`, value.ID, append(oldConnections, newConnections...)).Scan(&active); err != nil {
			return err
		}
		if active {
			return errors.New("drain queued, active and unconfirmed work before changing model quota membership")
		}
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM model_permits WHERE group_id=$1 AND expires_at_ms>floor(extract(epoch from clock_timestamp())*1000)::bigint)`, value.ID).Scan(&active); err != nil {
			return err
		}
		if active {
			return errors.New("wait for the provider accounting window before changing model quota membership")
		}
	}
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO model_quotas(id,body) VALUES($1,$2) ON CONFLICT(id) DO UPDATE SET body=excluded.body`, value.ID, body); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM model_quota_connections WHERE group_id=$1`, value.ID); err != nil {
		return err
	}
	for _, connection := range value.Connections {
		if _, err = tx.Exec(ctx, `INSERT INTO model_quota_connections(connection_id,group_id) VALUES($1,$2)`, connection, value.ID); err != nil {
			return mapError(err)
		}
	}
	return tx.Commit(ctx)
}

// ModelQuotaForConnection resolves current group membership before submission.
// Create verifies it under the admission lock so configuration cannot race it.
func (s *Store) ModelQuotaForConnection(ctx context.Context, id string) (*contract.ModelQuotaPlan, error) {
	var quota contract.ModelQuota
	err := s.pool.QueryRow(ctx, `SELECT q.body FROM model_quotas q JOIN model_quota_connections c ON c.group_id=q.id WHERE c.connection_id=$1`, id).Scan(&quota)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &contract.ModelQuotaPlan{Group: quota.ID, MaxOutputTokens: quota.MaxOutputTokens}, nil
}

// reserveModelPermit locks the Execution before the quota group. It keeps the
// execution's cancellation/owner checks atomic with the provider debit.
func (s *Store) ReserveModelPermit(ctx context.Context, id string, attempt int, input contract.ModelPermitRequest, validity time.Duration) (contract.ModelPermit, error) {
	if validity < time.Millisecond || validity > time.Second {
		return contract.ModelPermit{}, modelquota.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, s.config.OperationTimeout)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return contract.ModelPermit{}, err
	}
	defer tx.Rollback(ctx)
	var value contract.Execution
	if err = tx.QueryRow(ctx, `SELECT body FROM executions WHERE id=$1 FOR UPDATE`, id).Scan(&value); err != nil {
		return contract.ModelPermit{}, mapError(err)
	}
	if err = modelquota.ValidateAttempt(value, attempt, input, value.ModelQuota); err != nil {
		return contract.ModelPermit{}, err
	}
	var quota contract.ModelQuota
	if err = tx.QueryRow(ctx, `SELECT body FROM model_quotas WHERE id=$1 FOR UPDATE`, value.ModelQuota.Group).Scan(&quota); err != nil {
		return contract.ModelPermit{}, err
	}
	var now int64
	if err = tx.QueryRow(ctx, `SELECT floor(extract(epoch from clock_timestamp())*1000)::bigint`).Scan(&now); err != nil {
		return contract.ModelPermit{}, err
	}
	if now >= value.StartedAt.Add(time.Duration(value.RuntimeTimeoutMs)*time.Millisecond).UnixMilli() {
		return contract.ModelPermit{}, modelquota.ErrInactive
	}
	var existing modelquota.Reservation
	err = tx.QueryRow(ctx, `SELECT input_tokens,tokens,valid_until_ms,expires_at_ms FROM model_permits WHERE execution_id=$1 AND attempt=$2 AND sequence=$3`, id, attempt, input.Sequence).Scan(&existing.InputTokens, &existing.Tokens, &existing.ValidUntilMs, &existing.ExpiresAtMs)
	if err == nil {
		if existing.InputTokens != input.InputTokens {
			return contract.ModelPermit{}, modelquota.ErrConflict
		}
		if existing.ValidUntilMs <= now {
			return contract.ModelPermit{}, modelquota.ErrConflict
		}
		return contract.ModelPermit{Granted: true, ValidForMs: existing.ValidUntilMs - now}, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return contract.ModelPermit{}, err
	}
	rows, err := tx.Query(ctx, `SELECT tokens,expires_at_ms FROM model_permits WHERE group_id=$1 AND expires_at_ms>$2`, quota.ID, now)
	if err != nil {
		return contract.ModelPermit{}, err
	}
	var records []modelquota.Reservation
	for rows.Next() {
		var record modelquota.Reservation
		if err = rows.Scan(&record.Tokens, &record.ExpiresAtMs); err != nil {
			rows.Close()
			return contract.ModelPermit{}, err
		}
		records = append(records, record)
	}
	rows.Close()
	if rows.Err() != nil {
		return contract.ModelPermit{}, rows.Err()
	}
	reserved := input.InputTokens + value.ModelQuota.MaxOutputTokens
	permit, err := modelquota.Decide(quota, records, reserved, now)
	if err != nil {
		return permit, err
	}
	if permit.Granted {
		permit.ValidForMs = validity.Milliseconds()
		_, err = tx.Exec(ctx, `INSERT INTO model_permits(execution_id,attempt,sequence,group_id,input_tokens,tokens,valid_until_ms,expires_at_ms) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, id, attempt, input.Sequence, quota.ID, input.InputTokens, reserved, now+permit.ValidForMs, now+permit.ValidForMs+modelquota.MinuteMillis)
		if err != nil {
			return contract.ModelPermit{}, err
		}
	}
	return permit, tx.Commit(ctx)
}

// ReconcileModelPermit records reported total input/output once. Unknown usage
// leaves the estimate untouched; reports may raise a debit above its estimate.
func (s *Store) ReconcileModelPermit(ctx context.Context, id string, attempt, sequence int, actual int64) error {
	if actual < 0 || actual > modelquota.MaxTokens || sequence < 1 {
		return modelquota.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, s.config.OperationTimeout)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var value contract.Execution
	if err = tx.QueryRow(ctx, `SELECT body FROM executions WHERE id=$1 FOR UPDATE`, id).Scan(&value); err != nil {
		return mapError(err)
	}
	if value.Attempt != attempt || value.ModelQuota == nil {
		return modelquota.ErrInactive
	}
	if _, err = tx.Exec(ctx, `SELECT id FROM model_quotas WHERE id=$1 FOR UPDATE`, value.ModelQuota.Group); err != nil {
		return err
	}
	var previous *int64
	if err = tx.QueryRow(ctx, `SELECT actual_tokens FROM model_permits WHERE execution_id=$1 AND attempt=$2 AND sequence=$3 FOR UPDATE`, id, attempt, sequence).Scan(&previous); err != nil {
		return mapError(err)
	}
	if previous != nil && *previous != actual {
		return modelquota.ErrConflict
	}
	if _, err = tx.Exec(ctx, `UPDATE model_permits SET tokens=$4,actual_tokens=$4 WHERE execution_id=$1 AND attempt=$2 AND sequence=$3`, id, attempt, sequence, actual); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// validateModelQuotaPlan runs under Create's admission lock and rejects stale resolution.
func validateModelQuotaPlan(ctx context.Context, tx pgx.Tx, value contract.Execution) error {
	var quota contract.ModelQuota
	err := tx.QueryRow(ctx, `SELECT q.body FROM model_quotas q JOIN model_quota_connections c ON c.group_id=q.id WHERE c.connection_id=$1`, value.Spec.ModelConnectionID).Scan(&quota)
	if errors.Is(err, pgx.ErrNoRows) {
		if value.ModelQuota != nil {
			return modelquota.ErrConflict
		}
		return nil
	}
	if err != nil {
		return err
	}
	if value.ModelQuota == nil || value.ModelQuota.Group != quota.ID || value.ModelQuota.MaxOutputTokens != quota.MaxOutputTokens {
		return modelquota.ErrConflict
	}
	return nil
}

// PruneModelPermits deletes elapsed debits only after their Execution is terminal.
// Active attempts retain expired identities so a replay cannot renew a grant.
func (s *Store) PruneModelPermits(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM model_permits WHERE (execution_id,attempt,sequence) IN (
 SELECT p.execution_id,p.attempt,p.sequence FROM model_permits p JOIN executions e ON e.id=p.execution_id
 WHERE p.expires_at_ms<floor(extract(epoch from statement_timestamp())*1000)::bigint
 AND e.body->>'status' IN ('completed','failed','cancelled') LIMIT 1000)`)
	return err
}
