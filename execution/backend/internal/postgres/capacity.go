package postgres

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"

	"github.com/xyzbit/agentkiln/execution/backend/internal/domain"
)

// CapacityState reads admission fields and River delivery eligibility in one
// repeatable-read transaction snapshot. Scheduled quota snoozes count toward next-poll demand;
// future error retries and abandoned/missing deliveries do not.
func (s *Store) CapacityState(ctx context.Context) (domain.CapacityState, error) {
	ctx, cancel := context.WithTimeout(ctx, s.config.OperationTimeout)
	defer cancel()
	state := domain.CapacityState{MaxModelConcurrent: s.config.MaxModelConcurrent, MaxTenantConcurrent: s.config.MaxTenantConcurrent}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return state, err
	}
	defer tx.Rollback(ctx)
	pools, err := tx.Query(ctx, `SELECT body FROM runtime_pools`)
	if err != nil {
		return state, err
	}
	for pools.Next() {
		var body []byte
		var pool domain.RuntimePool
		if err = pools.Scan(&body); err != nil {
			pools.Close()
			return state, err
		}
		if err = json.Unmarshal(body, &pool); err != nil {
			pools.Close()
			return state, err
		}
		state.Pools = append(state.Pools, pool)
	}
	pools.Close()
	if pools.Err() != nil {
		return state, pools.Err()
	}
	state.ModelQuotas = map[string]domain.ModelQuotaCapacity{}
	quotaRows, err := tx.Query(ctx, `SELECT q.id,
 CASE WHEN (q.body->>'requestsPerMinute')::bigint=0 THEN -1 ELSE greatest(0,(q.body->>'requestsPerMinute')::bigint-count(p.sequence)) END,
 CASE WHEN (q.body->>'tokensPerMinute')::bigint=0 THEN -1 ELSE greatest(0,(q.body->>'tokensPerMinute')::bigint-COALESCE(sum(p.tokens),0)) END
 FROM model_quotas q LEFT JOIN model_permits p ON p.group_id=q.id AND p.expires_at_ms>floor(extract(epoch from statement_timestamp())*1000)::bigint GROUP BY q.id`)
	if err != nil {
		return state, err
	}
	for quotaRows.Next() {
		var id string
		var remaining domain.ModelQuotaCapacity
		if err = quotaRows.Scan(&id, &remaining.Requests, &remaining.Tokens); err != nil {
			quotaRows.Close()
			return state, err
		}
		state.ModelQuotas[id] = remaining
	}
	quotaRows.Close()
	if quotaRows.Err() != nil {
		return state, quotaRows.Err()
	}
	rows, err := tx.Query(ctx, `SELECT e.id,
 CASE WHEN COALESCE(e.body->'spec'->'metadata'->>'responseId','')<>'' THEN 'online' ELSE 'evaluation' END,
 e.body->>'status', COALESCE(e.body->>'executor',''), COALESCE(e.body->'spec'->>'harness',''),
 COALESCE(e.body->'spec'->>'modelConnectionId',''), COALESCE(e.body->'spec'->>'model',''),
 COALESCE(e.body->'spec'->'metadata'->>'tenantId',''), e.body->'runtimePlan',
 COALESCE(jsonb_array_length(e.body->'spec'->'messages'),0)>0,
 (e.body->>'createdAt')::timestamptz, COALESCE((e.body->>'queueExpiresAt')::timestamptz,'0001-01-01T00:00:00Z'::timestamptz), COALESCE(e.body->>'runtimeCleanupPending','false')='true',
 COALESCE(j.state IN ('available','running','scheduled') OR (j.state='retryable' AND j.scheduled_at<=statement_timestamp()),false)
 , COALESCE((e.body->>'maxModelRequests')::integer,0),e.body->'modelQuota'
 FROM executions e LEFT JOIN river_job j ON j.id=e.job_id
 WHERE e.body->>'status' IN ('queued','running','cancelling') OR e.body->>'runtimeCleanupPending'='true'`)
	if err != nil {
		return state, err
	}
	defer rows.Close()
	for rows.Next() {
		var entry domain.CapacityEntry
		var plan, quotaPlan []byte
		if err := rows.Scan(&entry.ID, &entry.Queue, &entry.Status, &entry.Executor, &entry.Harness, &entry.Connection, &entry.Model, &entry.Tenant, &plan, &entry.StructuredHistory, &entry.CreatedAt, &entry.QueueExpiresAt, &entry.CleanupPending, &entry.DeliveryReady, &entry.MaxModelRequests, &quotaPlan); err != nil {
			return state, err
		}
		if len(quotaPlan) > 0 {
			if err = json.Unmarshal(quotaPlan, &entry.ModelQuota); err != nil {
				return state, err
			}
		}
		if len(plan) > 0 {
			if err = json.Unmarshal(plan, &entry.RuntimePlan); err != nil {
				return state, err
			}
		}
		state.Entries = append(state.Entries, entry)
	}
	if rows.Err() != nil {
		return state, rows.Err()
	}
	rows.Close()
	return state, tx.Commit(ctx)
}
