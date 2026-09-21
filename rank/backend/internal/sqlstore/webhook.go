package sqlstore

import (
	"context"
	"database/sql"
	"time"

	"github.com/xyzbit/agentkiln/rank/backend/internal/domain"
)

func (s *Store) CreateWebhookEndpoint(ctx context.Context, endpoint domain.WebhookEndpoint) error {
	_, err := s.q.ExecContext(ctx, `
		INSERT INTO webhook_endpoints(public_id,tenant_id,name,url,secret_ref,events_json,deployment_ids_json,enabled,created_at,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, endpoint.ID, endpoint.TenantID, endpoint.Name, endpoint.URL, endpoint.SecretRef,
		encode(endpoint.Events), encode(endpoint.DeploymentIDs), boolInt(endpoint.Enabled), stamp(endpoint.CreatedAt), stamp(endpoint.UpdatedAt))
	return translate(err)
}

const webhookEndpointSelect = `
	SELECT public_id,tenant_id,name,url,secret_ref,events_json,deployment_ids_json,enabled,created_at,updated_at
	FROM webhook_endpoints`

func (s *Store) GetWebhookEndpoint(ctx context.Context, id domain.WebhookEndpointID) (domain.WebhookEndpoint, error) {
	value, err := scanWebhookEndpoint(s.q.QueryRowContext(ctx, webhookEndpointSelect+` WHERE public_id=$1`, id).Scan)
	return value, translate(err)
}

func (s *Store) ListWebhookEndpoints(ctx context.Context, tenantID string) ([]domain.WebhookEndpoint, error) {
	rows, err := s.q.QueryContext(ctx, webhookEndpointSelect+` WHERE tenant_id=$1 ORDER BY id`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []domain.WebhookEndpoint{}
	for rows.Next() {
		value, err := scanWebhookEndpoint(rows.Scan)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func scanWebhookEndpoint(scan scanFunc) (domain.WebhookEndpoint, error) {
	var value domain.WebhookEndpoint
	var eventsJSON, deploymentIDsJSON, created, updated string
	var enabled int
	if err := scan(&value.ID, &value.TenantID, &value.Name, &value.URL, &value.SecretRef, &eventsJSON, &deploymentIDsJSON, &enabled, &created, &updated); err != nil {
		return value, err
	}
	if err := decode(eventsJSON, &value.Events); err != nil {
		return value, err
	}
	if err := decode(deploymentIDsJSON, &value.DeploymentIDs); err != nil {
		return value, err
	}
	value.Enabled = enabled != 0
	var err error
	value.CreatedAt, err = parseStamp(created)
	if err != nil {
		return value, err
	}
	value.UpdatedAt, err = parseStamp(updated)
	return value, err
}

func (s *Store) SetWebhookEndpointEnabled(ctx context.Context, id domain.WebhookEndpointID, enabled bool, updatedAt time.Time) error {
	result, err := s.q.ExecContext(ctx, `UPDATE webhook_endpoints SET enabled=$1,updated_at=$2 WHERE public_id=$3`, boolInt(enabled), stamp(updatedAt), id)
	if err != nil {
		return translate(err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) CreateWebhookDelivery(ctx context.Context, delivery domain.WebhookDelivery) error {
	_, err := s.q.ExecContext(ctx, `
		INSERT INTO webhook_deliveries(public_id,endpoint_id,response_id,event_type,payload_json,status,attempts,next_attempt_at,lease_until,lease_token,last_error,created_at,delivered_at)
		SELECT $1,endpoint.id,response.id,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11
		FROM webhook_endpoints endpoint,responses response
		WHERE endpoint.public_id=$12 AND response.public_id=$13`,
		delivery.ID, delivery.EventType, string(delivery.Payload), delivery.Status, delivery.Attempts,
		stamp(delivery.NextAttemptAt), nullableStamp(delivery.LeaseUntil), delivery.LeaseToken, delivery.LastError, stamp(delivery.CreatedAt), nullableStamp(delivery.DeliveredAt),
		delivery.EndpointID, delivery.ResponseID)
	return translate(err)
}

const webhookDeliverySelect = `
	SELECT delivery.public_id,endpoint.public_id,response.public_id,delivery.event_type,delivery.payload_json,
	       delivery.status,delivery.attempts,delivery.next_attempt_at,delivery.lease_until,delivery.lease_token,delivery.last_error,
	       delivery.created_at,delivery.delivered_at
	FROM webhook_deliveries delivery
	JOIN webhook_endpoints endpoint ON endpoint.id=delivery.endpoint_id
	JOIN responses response ON response.id=delivery.response_id`

func (s *Store) ListWebhookDeliveries(ctx context.Context, endpointID domain.WebhookEndpointID) ([]domain.WebhookDelivery, error) {
	return s.listWebhookDeliveries(ctx, webhookDeliverySelect+` WHERE endpoint.public_id=$1 ORDER BY delivery.id DESC`, endpointID)
}

func (s *Store) ListDueWebhookDeliveries(ctx context.Context, now time.Time, limit int) ([]domain.WebhookDelivery, error) {
	return s.listWebhookDeliveries(ctx, webhookDeliverySelect+`
		WHERE (delivery.status=$1 AND delivery.next_attempt_at<=$2)
		   OR (delivery.status=$3 AND delivery.lease_until<=$4)
		ORDER BY delivery.next_attempt_at,delivery.id LIMIT $5`,
		domain.WebhookDeliveryPending, stamp(now), domain.WebhookDeliveryDelivering, stamp(now), limit)
}

func (s *Store) listWebhookDeliveries(ctx context.Context, query string, args ...any) ([]domain.WebhookDelivery, error) {
	rows, err := s.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []domain.WebhookDelivery{}
	for rows.Next() {
		value, err := scanWebhookDelivery(rows.Scan)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func scanWebhookDelivery(scan scanFunc) (domain.WebhookDelivery, error) {
	var value domain.WebhookDelivery
	var payload, next, created string
	var lease, delivered sql.NullString
	if err := scan(&value.ID, &value.EndpointID, &value.ResponseID, &value.EventType, &payload,
		&value.Status, &value.Attempts, &next, &lease, &value.LeaseToken, &value.LastError, &created, &delivered); err != nil {
		return value, err
	}
	value.Payload = []byte(payload)
	var err error
	value.NextAttemptAt, err = parseStamp(next)
	if err != nil {
		return value, err
	}
	value.CreatedAt, err = parseStamp(created)
	if err != nil {
		return value, err
	}
	if lease.Valid {
		parsed, err := parseStamp(lease.String)
		if err != nil {
			return value, err
		}
		value.LeaseUntil = &parsed
	}
	if delivered.Valid {
		parsed, err := parseStamp(delivered.String)
		if err != nil {
			return value, err
		}
		value.DeliveredAt = &parsed
	}
	return value, nil
}

func (s *Store) ClaimWebhookDelivery(ctx context.Context, id domain.WebhookDeliveryID, now, leaseUntil time.Time, leaseToken string) (domain.WebhookDelivery, bool, error) {
	result, err := s.q.ExecContext(ctx, `
		UPDATE webhook_deliveries
		SET status=$1,attempts=attempts+1,lease_until=$2,lease_token=$3
		WHERE public_id=$4 AND ((status=$5 AND next_attempt_at<=$6) OR (status=$7 AND lease_until<=$8))`,
		domain.WebhookDeliveryDelivering, stamp(leaseUntil), leaseToken, id,
		domain.WebhookDeliveryPending, stamp(now), domain.WebhookDeliveryDelivering, stamp(now))
	if err != nil {
		return domain.WebhookDelivery{}, false, translate(err)
	}
	changed, err := result.RowsAffected()
	if err != nil || changed == 0 {
		return domain.WebhookDelivery{}, false, err
	}
	value, err := scanWebhookDelivery(s.q.QueryRowContext(ctx, webhookDeliverySelect+` WHERE delivery.public_id=$1`, id).Scan)
	return value, true, translate(err)
}

func (s *Store) CompleteWebhookDelivery(ctx context.Context, id domain.WebhookDeliveryID, leaseToken string, deliveredAt time.Time) error {
	result, err := s.q.ExecContext(ctx, `
		UPDATE webhook_deliveries SET status=$1,lease_until=NULL,lease_token='',last_error='',delivered_at=$2
		WHERE public_id=$3 AND status=$4 AND lease_token=$5`, domain.WebhookDeliveryDelivered, stamp(deliveredAt), id, domain.WebhookDeliveryDelivering, leaseToken)
	return changedOne(result, err)
}

func (s *Store) RetryWebhookDelivery(ctx context.Context, id domain.WebhookDeliveryID, leaseToken string, nextAttemptAt time.Time, terminal bool, message string) error {
	status := domain.WebhookDeliveryPending
	if terminal {
		status = domain.WebhookDeliveryFailed
	}
	result, err := s.q.ExecContext(ctx, `
		UPDATE webhook_deliveries SET status=$1,next_attempt_at=$2,lease_until=NULL,lease_token='',last_error=$3
		WHERE public_id=$4 AND status=$5 AND lease_token=$6`, status, stamp(nextAttemptAt), message, id, domain.WebhookDeliveryDelivering, leaseToken)
	return changedOne(result, err)
}

func changedOne(result sql.Result, err error) error {
	if err != nil {
		return translate(err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return domain.ErrConflict
	}
	return nil
}
