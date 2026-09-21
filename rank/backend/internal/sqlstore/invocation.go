package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/xyzbit/agentkiln/rank/backend/internal/domain"
)

const evaluationInvocationSchema = `CREATE TABLE IF NOT EXISTS evaluation_invocations(
 id TEXT PRIMARY KEY,run_id TEXT NOT NULL REFERENCES runs(id),
 trial_id TEXT NOT NULL REFERENCES run_trials(id),request_json TEXT NOT NULL,
 execution_id TEXT NOT NULL DEFAULT '',result_json TEXT NOT NULL DEFAULT '',
 sequence BIGINT NOT NULL DEFAULT 0,submission_started INTEGER NOT NULL DEFAULT 0);
 CREATE INDEX IF NOT EXISTS evaluation_invocations_run ON evaluation_invocations(run_id);`

// CreateEvaluationInvocation requires a live, uncancelled Run. The caller's
// transaction also commits its business admission or prerequisite result.
func (s *Store) CreateEvaluationInvocation(ctx context.Context, value domain.EvaluationInvocation) error {
	result, err := s.q.ExecContext(ctx, `INSERT INTO evaluation_invocations(id,run_id,trial_id,request_json)
 SELECT $1,$2,$3,$4 FROM runs r JOIN run_trials t ON t.run_id=r.id
 WHERE r.id=$2 AND t.id=$3 AND r.cancel_requested=0 AND r.status IN ('queued','preparing','running','scoring')
 ON CONFLICT(id) DO NOTHING`, value.ID, value.RunID, value.TrialID, string(value.Request))
	if err != nil {
		return translate(err)
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return domain.ErrConflict
	}
	return nil
}

const invocationSelect = `SELECT id,run_id,trial_id,request_json,execution_id,result_json,sequence,submission_started FROM evaluation_invocations`

func scanInvocation(scan scanFunc) (domain.EvaluationInvocation, error) {
	var value domain.EvaluationInvocation
	var request, result string
	var started int
	err := scan(&value.ID, &value.RunID, &value.TrialID, &request, &value.ExecutionID, &result, &value.Sequence, &started)
	value.SubmissionStarted = started != 0
	value.Request = []byte(request)
	value.Result = []byte(result)
	return value, translate(err)
}

func (s *Store) GetEvaluationInvocation(ctx context.Context, id domain.InvocationID) (domain.EvaluationInvocation, error) {
	return scanInvocation(s.q.QueryRowContext(ctx, invocationSelect+` WHERE id=$1`, id).Scan)
}

func (s *Store) ListEvaluationInvocations(ctx context.Context, runID string) ([]domain.EvaluationInvocation, error) {
	rows, err := s.q.QueryContext(ctx, invocationSelect+` WHERE run_id=$1 ORDER BY id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []domain.EvaluationInvocation{}
	for rows.Next() {
		value, err := scanInvocation(rows.Scan)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) SaveInvocationExecution(ctx context.Context, id domain.InvocationID, executionID string) error {
	result, err := s.q.ExecContext(ctx, `UPDATE evaluation_invocations SET execution_id=$1 WHERE id=$2 AND (execution_id='' OR execution_id=$1)`, executionID, id)
	return changedInvocation(result, err)
}

func (s *Store) CompleteInvocation(ctx context.Context, id domain.InvocationID, result json.RawMessage) error {
	updated, err := s.q.ExecContext(ctx, `UPDATE evaluation_invocations SET result_json=$1 WHERE id=$2 AND result_json=''`, string(result), id)
	return changedInvocation(updated, err)
}

func (s *Store) AdvanceInvocationSequence(ctx context.Context, id domain.InvocationID, before, after int64) error {
	if after <= before {
		return domain.ErrConflict
	}
	result, err := s.q.ExecContext(ctx, `UPDATE evaluation_invocations SET sequence=$1 WHERE id=$2 AND sequence=$3`, after, id, before)
	return changedInvocation(result, err)
}

func changedInvocation(result sql.Result, err error) error {
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

func (s *Store) RequestRunCancellation(ctx context.Context, id string) error {
	_, err := s.q.ExecContext(ctx, `UPDATE runs SET cancel_requested=1 WHERE id=$1 AND status IN ('queued','preparing','running','scoring')`, id)
	return translate(err)
}

// AppendRunEventOnce gives derived phase events a stable key across recovery.
func (s *Store) AppendRunEventOnce(ctx context.Context, key string, event domain.RunEvent) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO run_events(run_id,event_key,type,case_id,trial_id,trial_index,status,dataset_version_id,agent_version_id,passed,cost,score,output,reason,created_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) ON CONFLICT(run_id,event_key) DO NOTHING`, event.RunID, key, event.Type, nullableText(event.CaseID), nullableText(event.TrialID), event.TrialIndex, nullableText(event.Status), nullableText(event.DatasetVersionID), nullableText(event.AgentVersionID), nullableBool(event.Passed), nullableFloat(event.Cost), nullableFloat(event.Score), nullableText(event.Output), nullableText(event.Reason), stamp(event.At))
	return translate(err)
}

// BeginInvocationSubmission records possible acceptance before the HTTP call.
// An intent never marked here can be cancelled without launching remote work.
func (s *Store) BeginInvocationSubmission(ctx context.Context, id domain.InvocationID) error {
	result, err := s.q.ExecContext(ctx, `UPDATE evaluation_invocations SET submission_started=1 WHERE id=$1 AND run_id IN (SELECT id FROM runs WHERE cancel_requested=0 AND status IN ('queued','preparing','running','scoring'))`, id)
	return changedInvocation(result, err)
}
