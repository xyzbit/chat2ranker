package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/execution/backend/internal/domain"
)

// reserveQuota runs under the runtime-admission transaction lock. The running
// execution record is the reservation, retained through cancelling and released
// only after confirmed runtime cleanup and a terminal outcome. Unknown attempts
// retain capacity even after their River job ends.
func (s *Store) reserveQuota(ctx context.Context, tx pgx.Tx, spec contract.Spec) error {
	if s.config.MaxModelConcurrent > 0 && spec.ModelConnectionID != "" {
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM executions
 WHERE (body->>'status' IN ('running','cancelling') OR body->>'runtimeCleanupPending'='true')
 AND body->'spec'->>'modelConnectionId'=$1
 AND COALESCE(body->'spec'->>'model','')=$2`, spec.ModelConnectionID, spec.Model).Scan(&count); err != nil {
			return err
		}
		if count >= s.config.MaxModelConcurrent {
			return domain.ErrQuotaExceeded
		}
	}
	if s.config.MaxTenantConcurrent > 0 {
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM executions
 WHERE (body->>'status' IN ('running','cancelling') OR body->>'runtimeCleanupPending'='true')
 AND COALESCE(body->'spec'->'metadata'->>'tenantId','')=$1`, spec.Metadata["tenantId"]).Scan(&count); err != nil {
			return err
		}
		if count >= s.config.MaxTenantConcurrent {
			return domain.ErrQuotaExceeded
		}
	}
	return nil
}

// checkModelRate avoids starting a runtime when its smallest possible model
// request already exceeds remaining shared capacity. Actual requests reserve
// exact estimates later; this observation is not a transferable permit.
func checkModelRate(ctx context.Context, tx pgx.Tx, value contract.Execution) error {
	if value.ModelQuota == nil {
		return nil
	}
	var requests, tokens int64
	err := tx.QueryRow(ctx, `SELECT
 CASE WHEN (q.body->>'requestsPerMinute')::bigint=0 THEN -1 ELSE greatest(0,(q.body->>'requestsPerMinute')::bigint-count(p.sequence)) END,
 CASE WHEN (q.body->>'tokensPerMinute')::bigint=0 THEN -1 ELSE greatest(0,(q.body->>'tokensPerMinute')::bigint-COALESCE(sum(p.tokens),0)) END
 FROM model_quotas q LEFT JOIN model_permits p ON p.group_id=q.id AND p.expires_at_ms>floor(extract(epoch from statement_timestamp())*1000)::bigint WHERE q.id=$1 GROUP BY q.id`, value.ModelQuota.Group).Scan(&requests, &tokens)
	if err != nil {
		return err
	}
	if requests == 0 || requests < -1 || (tokens != -1 && tokens < value.ModelQuota.MaxOutputTokens+1) {
		return domain.ErrQuotaExceeded
	}
	return nil
}
