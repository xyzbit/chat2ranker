package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/xyzbit/agentkiln/rank/backend/internal/domain"
)

func (s *Store) CreateAPICredential(ctx context.Context, credential domain.APICredential) error {
	_, err := s.q.ExecContext(ctx, `
		INSERT INTO api_credentials(public_id,tenant_id,name,key_prefix,secret_hash,scopes_json,deployment_ids_json,created_at,revoked_at)
		VALUES(?,?,?,?,?,?,?,?,?)`, credential.ID, credential.TenantID, credential.Name,
		credential.KeyPrefix, credential.SecretHash, encode(credential.Scopes),
		encode(credential.DeploymentIDs), stamp(credential.CreatedAt), nullableStamp(credential.RevokedAt))
	return translate(err)
}

func (s *Store) GetAPICredentialBySecretHash(ctx context.Context, secretHash string) (domain.APICredential, error) {
	var credential domain.APICredential
	var scopesJSON, deploymentIDsJSON, created string
	var revoked sql.NullString
	err := s.q.QueryRowContext(ctx, `
		SELECT public_id,tenant_id,name,key_prefix,secret_hash,scopes_json,deployment_ids_json,created_at,revoked_at
		FROM api_credentials WHERE secret_hash=?`, secretHash).Scan(
		&credential.ID, &credential.TenantID, &credential.Name, &credential.KeyPrefix,
		&credential.SecretHash, &scopesJSON, &deploymentIDsJSON, &created, &revoked)
	if err != nil {
		return credential, translate(err)
	}
	if err := decode(scopesJSON, &credential.Scopes); err != nil {
		return credential, err
	}
	if err := decode(deploymentIDsJSON, &credential.DeploymentIDs); err != nil {
		return credential, err
	}
	credential.CreatedAt, err = parseStamp(created)
	if err != nil {
		return credential, err
	}
	if revoked.Valid {
		value, parseErr := parseStamp(revoked.String)
		if parseErr != nil {
			return credential, parseErr
		}
		credential.RevokedAt = &value
	}
	return credential, nil
}

func (s *Store) AdmitResponse(ctx context.Context, response domain.ServingResponse, conversation *domain.Conversation, created domain.ResponseEvent, requestsPerMinute, maxConcurrent int) error {
	if s.tx == nil {
		return s.WithinTx(ctx, func(repo domain.Repository) error {
			return repo.AdmitResponse(ctx, response, conversation, created, requestsPerMinute, maxConcurrent)
		})
	}
	if conversation != nil {
		if err := s.insertConversation(ctx, *conversation); err != nil {
			return err
		}
	} else {
		current, err := s.GetConversation(ctx, response.ConversationID)
		if err != nil {
			return err
		}
		if current.LastResponseID != response.PreviousResponseID {
			return domain.ErrConversationHead
		}
		var active int
		if err := s.q.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM responses r
			JOIN conversations c ON c.id=r.conversation_id
			WHERE c.public_id=? AND r.status IN (?,?)`, response.ConversationID,
			domain.ResponseQueued, domain.ResponseInProgress).Scan(&active); err != nil {
			return err
		}
		if active != 0 {
			return domain.ErrConversationBusy
		}
	}
	credentialDBID, deploymentDBID, releaseDBID, conversationDBID, previousResponseDBID, err := s.responseForeignKeys(ctx, response)
	if err != nil {
		return err
	}
	var recent int
	err = s.q.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM responses
		WHERE credential_id=? AND deployment_id=? AND created_at>=?`,
		credentialDBID, deploymentDBID, stamp(response.CreatedAt.Add(-time.Minute))).Scan(&recent)
	if err != nil {
		return err
	}
	if recent >= requestsPerMinute {
		return domain.ErrRateLimited
	}
	var active int
	err = s.q.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM responses
		WHERE credential_id=? AND deployment_id=? AND status IN (?,?)`,
		credentialDBID, deploymentDBID, domain.ResponseQueued, domain.ResponseInProgress).Scan(&active)
	if err != nil {
		return err
	}
	if active >= maxConcurrent {
		return domain.ErrConcurrencyLimit
	}
	var responseDBID int64
	err = s.q.QueryRowContext(ctx, `
		INSERT INTO responses(
			public_id,tenant_id,credential_id,deployment_id,deployment_name,requested_reference,
			resolution_kind,release_revision_id,agent_version_id,conversation_id,previous_response_id,idempotency_key,request_hash,
			input_text,input_items_json,caller_tools_json,tool_call_json,instructions,metadata_json,text_format_json,background,status,execution_id,output_item_id,
			output_text,usage_json,cost,cost_known,error_code,error_message,created_at,started_at,completed_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) RETURNING id`,
		response.ID, response.TenantID, credentialDBID, deploymentDBID, response.DeploymentName,
		response.RequestedReference, response.ResolutionKind, releaseDBID, response.AgentVersionID,
		conversationDBID, previousResponseDBID, response.IdempotencyKey, response.RequestHash, response.Input, encode(response.InputItems), encode(response.CallerTools), encode(response.ToolCall), response.Instructions,
		encode(response.Metadata), encode(response.TextFormat), boolInt(response.Background), response.Status, nullableString(response.ExecutionID),
		response.OutputItemID, response.OutputText, encode(response.Usage), response.Cost, boolInt(response.CostKnown),
		failureCode(response.Failure), failureMessage(response.Failure), stamp(response.CreatedAt),
		nullableStamp(response.StartedAt), nullableStamp(response.CompletedAt)).Scan(&responseDBID)
	if err != nil {
		if strings.Contains(err.Error(), "responses.conversation_id") {
			return domain.ErrConversationBusy
		}
		return translate(err)
	}
	result, err := s.q.ExecContext(ctx, `
		UPDATE conversations SET last_response_id=?,updated_at=?
		WHERE id=? AND ((? IS NULL AND last_response_id IS NULL) OR last_response_id=?)`,
		responseDBID, stamp(response.CreatedAt), conversationDBID, previousResponseDBID, previousResponseDBID)
	if err != nil {
		return translate(err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return domain.ErrConversationHead
	}
	return s.appendResponseEvents(ctx, responseDBID, []domain.ResponseEvent{created})
}

func (s *Store) insertConversation(ctx context.Context, conversation domain.Conversation) error {
	var deploymentDBID int64
	if err := s.q.QueryRowContext(ctx, `SELECT id FROM deployments WHERE public_id=?`, conversation.DeploymentID).Scan(&deploymentDBID); err != nil {
		return translate(err)
	}
	var releaseDBID any
	if conversation.ReleaseRevisionID != "" {
		var id int64
		if err := s.q.QueryRowContext(ctx, `SELECT id FROM release_revisions WHERE public_id=?`, conversation.ReleaseRevisionID).Scan(&id); err != nil {
			return translate(err)
		}
		releaseDBID = id
	}
	_, err := s.q.ExecContext(ctx, `
		INSERT INTO conversations(public_id,tenant_id,deployment_id,agent_version_id,release_revision_id,last_response_id,created_at,updated_at)
		VALUES(?,?,?,?,?,NULL,?,?)`, conversation.ID, conversation.TenantID, deploymentDBID,
		conversation.AgentVersionID, releaseDBID, stamp(conversation.CreatedAt), stamp(conversation.UpdatedAt))
	return translate(err)
}

func (s *Store) responseForeignKeys(ctx context.Context, response domain.ServingResponse) (int64, int64, any, int64, any, error) {
	var credentialDBID, deploymentDBID, conversationDBID int64
	if err := s.q.QueryRowContext(ctx, `SELECT id FROM api_credentials WHERE public_id=?`, response.CredentialID).Scan(&credentialDBID); err != nil {
		return 0, 0, nil, 0, nil, translate(err)
	}
	if err := s.q.QueryRowContext(ctx, `SELECT id FROM deployments WHERE public_id=?`, response.DeploymentID).Scan(&deploymentDBID); err != nil {
		return 0, 0, nil, 0, nil, translate(err)
	}
	if err := s.q.QueryRowContext(ctx, `SELECT id FROM conversations WHERE public_id=?`, response.ConversationID).Scan(&conversationDBID); err != nil {
		return 0, 0, nil, 0, nil, translate(err)
	}
	var previousResponseDBID any
	if response.PreviousResponseID != "" {
		var id int64
		if err := s.q.QueryRowContext(ctx, `SELECT id FROM responses WHERE public_id=?`, response.PreviousResponseID).Scan(&id); err != nil {
			return 0, 0, nil, 0, nil, translate(err)
		}
		previousResponseDBID = id
	}
	if response.ReleaseRevisionID == "" {
		return credentialDBID, deploymentDBID, nil, conversationDBID, previousResponseDBID, nil
	}
	var releaseDBID int64
	if err := s.q.QueryRowContext(ctx, `SELECT id FROM release_revisions WHERE public_id=?`, response.ReleaseRevisionID).Scan(&releaseDBID); err != nil {
		return 0, 0, nil, 0, nil, translate(err)
	}
	return credentialDBID, deploymentDBID, releaseDBID, conversationDBID, previousResponseDBID, nil
}

const responseSelect = `
	SELECT r.public_id,r.tenant_id,c.public_id,d.public_id,r.deployment_name,r.requested_reference,
	       r.resolution_kind,COALESCE(rr.public_id,''),r.agent_version_id,COALESCE(conversation.public_id,''),
	       COALESCE(previous.public_id,''),r.idempotency_key,r.request_hash,
	       r.input_text,r.input_items_json,r.caller_tools_json,r.tool_call_json,r.instructions,r.metadata_json,r.text_format_json,r.background,r.status,COALESCE(r.execution_id,''),
	       r.output_item_id,r.output_text,r.usage_json,r.cost,r.cost_known,r.error_code,r.error_message,
	       r.created_at,r.started_at,r.completed_at
	FROM responses r
	JOIN api_credentials c ON c.id=r.credential_id
	JOIN deployments d ON d.id=r.deployment_id
	LEFT JOIN release_revisions rr ON rr.id=r.release_revision_id
	LEFT JOIN conversations conversation ON conversation.id=r.conversation_id
	LEFT JOIN responses previous ON previous.id=r.previous_response_id`

func (s *Store) GetConversation(ctx context.Context, id domain.ConversationID) (domain.Conversation, error) {
	var value domain.Conversation
	var created, updated string
	err := s.q.QueryRowContext(ctx, `
		SELECT conversation.public_id,conversation.tenant_id,deployment.public_id,
		       conversation.agent_version_id,COALESCE(revision.public_id,''),COALESCE(last_response.public_id,''),
		       conversation.created_at,conversation.updated_at
		FROM conversations conversation
		JOIN deployments deployment ON deployment.id=conversation.deployment_id
		LEFT JOIN release_revisions revision ON revision.id=conversation.release_revision_id
		LEFT JOIN responses last_response ON last_response.id=conversation.last_response_id
		WHERE conversation.public_id=?`, id).Scan(
		&value.ID, &value.TenantID, &value.DeploymentID, &value.AgentVersionID,
		&value.ReleaseRevisionID, &value.LastResponseID, &created, &updated)
	if err != nil {
		return value, translate(err)
	}
	value.CreatedAt, err = parseStamp(created)
	if err != nil {
		return value, err
	}
	value.UpdatedAt, err = parseStamp(updated)
	return value, err
}

func (s *Store) GetResponse(ctx context.Context, id domain.ResponseID) (domain.ServingResponse, error) {
	value, err := scanResponse(s.q.QueryRowContext(ctx, responseSelect+` WHERE r.public_id=?`, id).Scan)
	return value, translate(err)
}

func (s *Store) GetResponseByIdempotencyKey(ctx context.Context, credentialID domain.APICredentialID, key string) (domain.ServingResponse, error) {
	value, err := scanResponse(s.q.QueryRowContext(ctx, responseSelect+` WHERE c.public_id=? AND r.idempotency_key=?`, credentialID, key).Scan)
	return value, translate(err)
}

func (s *Store) ListConversationResponses(ctx context.Context, conversationID domain.ConversationID) ([]domain.ServingResponse, error) {
	rows, err := s.q.QueryContext(ctx, responseSelect+` WHERE conversation.public_id=? ORDER BY r.id`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	responses := []domain.ServingResponse{}
	for rows.Next() {
		value, scanErr := scanResponse(rows.Scan)
		if scanErr != nil {
			return nil, scanErr
		}
		responses = append(responses, value)
	}
	return responses, rows.Err()
}

func (s *Store) ListActiveResponses(ctx context.Context) ([]domain.ServingResponse, error) {
	rows, err := s.q.QueryContext(ctx, responseSelect+` WHERE r.status IN (?,?) ORDER BY r.id`, domain.ResponseQueued, domain.ResponseInProgress)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	responses := []domain.ServingResponse{}
	for rows.Next() {
		value, scanErr := scanResponse(rows.Scan)
		if scanErr != nil {
			return nil, scanErr
		}
		responses = append(responses, value)
	}
	return responses, rows.Err()
}

func scanResponse(scan scanFunc) (domain.ServingResponse, error) {
	var value domain.ServingResponse
	var inputItemsJSON, callerToolsJSON, toolCallJSON, metadataJSON, textFormatJSON, usageJSON, created string
	var background, costKnown int
	var errorCode, errorMessage string
	var started, completed sql.NullString
	err := scan(
		&value.ID, &value.TenantID, &value.CredentialID, &value.DeploymentID, &value.DeploymentName,
		&value.RequestedReference, &value.ResolutionKind, &value.ReleaseRevisionID, &value.AgentVersionID,
		&value.ConversationID, &value.PreviousResponseID, &value.IdempotencyKey, &value.RequestHash,
		&value.Input, &inputItemsJSON, &callerToolsJSON, &toolCallJSON, &value.Instructions, &metadataJSON, &textFormatJSON,
		&background, &value.Status, &value.ExecutionID, &value.OutputItemID, &value.OutputText, &usageJSON,
		&value.Cost, &costKnown, &errorCode, &errorMessage, &created, &started, &completed)
	if err != nil {
		return value, err
	}
	if err := decode(inputItemsJSON, &value.InputItems); err != nil {
		return value, err
	}
	if err := decode(callerToolsJSON, &value.CallerTools); err != nil {
		return value, err
	}
	if err := decode(toolCallJSON, &value.ToolCall); err != nil {
		return value, err
	}
	if err := decode(metadataJSON, &value.Metadata); err != nil {
		return value, err
	}
	if err := decode(textFormatJSON, &value.TextFormat); err != nil {
		return value, err
	}
	if err := decode(usageJSON, &value.Usage); err != nil {
		return value, err
	}
	value.Background, value.CostKnown = background != 0, costKnown != 0
	value.CreatedAt, err = parseStamp(created)
	if err != nil {
		return value, err
	}
	if started.Valid {
		parsed, parseErr := parseStamp(started.String)
		if parseErr != nil {
			return value, parseErr
		}
		value.StartedAt = &parsed
	}
	if completed.Valid {
		parsed, parseErr := parseStamp(completed.String)
		if parseErr != nil {
			return value, parseErr
		}
		value.CompletedAt = &parsed
	}
	if errorCode != "" || errorMessage != "" {
		value.Failure = &domain.ResponseFailure{Code: errorCode, Message: errorMessage}
	}
	return value, nil
}

func (s *Store) UpdateResponse(ctx context.Context, response domain.ServingResponse, expectedStatus string, events []domain.ResponseEvent) error {
	if s.tx == nil {
		return s.WithinTx(ctx, func(repo domain.Repository) error {
			return repo.UpdateResponse(ctx, response, expectedStatus, events)
		})
	}
	result, err := s.q.ExecContext(ctx, `
		UPDATE responses SET status=?,execution_id=?,output_text=?,tool_call_json=?,usage_json=?,cost=?,cost_known=?,
		       error_code=?,error_message=?,started_at=?,completed_at=?
		WHERE public_id=? AND status=?`, response.Status, nullableString(response.ExecutionID), response.OutputText, encode(response.ToolCall),
		encode(response.Usage), response.Cost, boolInt(response.CostKnown), failureCode(response.Failure),
		failureMessage(response.Failure), nullableStamp(response.StartedAt), nullableStamp(response.CompletedAt),
		response.ID, expectedStatus)
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
	var responseDBID int64
	if err := s.q.QueryRowContext(ctx, `SELECT id FROM responses WHERE public_id=?`, response.ID).Scan(&responseDBID); err != nil {
		return translate(err)
	}
	return s.appendResponseEvents(ctx, responseDBID, events)
}

func (s *Store) appendResponseEvents(ctx context.Context, responseDBID int64, events []domain.ResponseEvent) error {
	if len(events) == 0 {
		return nil
	}
	var sequence int64
	if err := s.q.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence),0) FROM response_events WHERE response_id=?`, responseDBID).Scan(&sequence); err != nil {
		return err
	}
	for _, event := range events {
		sequence++
		if _, err := s.q.ExecContext(ctx, `
			INSERT INTO response_events(response_id,sequence,type,data_json,created_at)
			VALUES(?,?,?,?,?)`, responseDBID, sequence, event.Type, string(event.Data), stamp(event.CreatedAt)); err != nil {
			return translate(err)
		}
	}
	return nil
}

func (s *Store) ListResponseEvents(ctx context.Context, responseID domain.ResponseID, afterSequence int64) ([]domain.ResponseEvent, error) {
	rows, err := s.q.QueryContext(ctx, `
		SELECT e.sequence,e.type,e.data_json,e.created_at
		FROM response_events e JOIN responses r ON r.id=e.response_id
		WHERE r.public_id=? AND e.sequence>? ORDER BY e.sequence`, responseID, afterSequence)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []domain.ResponseEvent{}
	for rows.Next() {
		var event domain.ResponseEvent
		var dataJSON, created string
		event.ResponseID = responseID
		if err := rows.Scan(&event.Sequence, &event.Type, &dataJSON, &created); err != nil {
			return nil, err
		}
		event.Data = []byte(dataJSON)
		event.CreatedAt, err = parseStamp(created)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func failureCode(value *domain.ResponseFailure) string {
	if value == nil {
		return ""
	}
	return value.Code
}

func failureMessage(value *domain.ResponseFailure) string {
	if value == nil {
		return ""
	}
	return value.Message
}
