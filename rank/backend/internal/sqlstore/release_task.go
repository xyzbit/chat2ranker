package sqlstore

import (
	"context"
	"database/sql"
	"errors"

	"github.com/xyzbit/agentkiln/rank/backend/internal/domain"
)

func (s *Store) GetAgentEvaluationDefaults(ctx context.Context, id string) (domain.AgentEvaluationDefaults, error) {
	var payload string
	err := s.q.QueryRowContext(ctx, `SELECT defaults_json FROM agent_evaluation_defaults WHERE agent_family_id=$1`, id).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.AgentEvaluationDefaults{}, nil
	}
	if err != nil {
		return domain.AgentEvaluationDefaults{}, err
	}
	var value domain.AgentEvaluationDefaults
	err = decode(payload, &value)
	return value, err
}

func (s *Store) SaveAgentEvaluationDefaults(ctx context.Context, id string, value domain.AgentEvaluationDefaults, expected int64) error {
	result, err := s.q.ExecContext(ctx, `INSERT INTO agent_evaluation_defaults(agent_family_id,revision,defaults_json) VALUES($1,$2,$3) ON CONFLICT(agent_family_id) DO UPDATE SET revision=excluded.revision,defaults_json=excluded.defaults_json WHERE agent_evaluation_defaults.revision=$4`, id, value.Revision, encode(value), expected)
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

func (s *Store) CreateReleaseTask(ctx context.Context, task domain.ReleaseTask) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO release_tasks(public_id,deployment_id,request_key,request_hash,status,task_json) VALUES($1,(SELECT id FROM deployments WHERE public_id=$2),$3,$4,$5,$6)`, task.ID, task.DeploymentID, task.IdempotencyKey, task.RequestHash, task.Status, encode(task))
	return translate(err)
}

func (s *Store) readReleaseTask(ctx context.Context, query string, args ...any) (domain.ReleaseTask, error) {
	var payload, key, hash string
	if err := s.q.QueryRowContext(ctx, query, args...).Scan(&payload, &key, &hash); err != nil {
		return domain.ReleaseTask{}, translate(err)
	}
	var task domain.ReleaseTask
	err := decode(payload, &task)
	task.IdempotencyKey, task.RequestHash = key, hash
	return task, err
}

func (s *Store) GetReleaseTask(ctx context.Context, id domain.ReleaseTaskID) (domain.ReleaseTask, error) {
	return s.readReleaseTask(ctx, `SELECT task_json,request_key,request_hash FROM release_tasks WHERE public_id=$1`, id)
}

func (s *Store) GetReleaseTaskByKey(ctx context.Context, id domain.DeploymentID, key string) (domain.ReleaseTask, error) {
	return s.readReleaseTask(ctx, `SELECT task_json,request_key,request_hash FROM release_tasks WHERE deployment_id=(SELECT id FROM deployments WHERE public_id=$1) AND request_key=$2`, id, key)
}

// Empty deployment selects active tasks for recovery and completion dispatch.
func (s *Store) ListReleaseTasks(ctx context.Context, id domain.DeploymentID, experimentID string) ([]domain.ReleaseTask, error) {
	query := `SELECT task_json,request_key,request_hash FROM release_tasks WHERE ($1='' OR deployment_id=(SELECT id FROM deployments WHERE public_id=$2)) AND ($3='' OR (task_json ->> 'experimentId')=$4) ORDER BY id DESC LIMIT 100`
	args := []any{id, id, experimentID, experimentID}
	if id == "" && experimentID == "" {
		query = `SELECT task_json,request_key,request_hash FROM release_tasks WHERE status IN ('regression','ready') ORDER BY id`
		args = nil
	}
	rows, err := s.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.ReleaseTask{}
	for rows.Next() {
		var task domain.ReleaseTask
		var payload string
		if err := rows.Scan(&payload, &task.IdempotencyKey, &task.RequestHash); err != nil {
			return nil, err
		}
		if err := decode(payload, &task); err != nil {
			return nil, err
		}
		items = append(items, task)
	}
	return items, rows.Err()
}

func (s *Store) SaveReleaseTask(ctx context.Context, task domain.ReleaseTask, expected string) error {
	result, err := s.q.ExecContext(ctx, `UPDATE release_tasks SET status=$1,task_json=$2 WHERE public_id=$3 AND status=$4`, task.Status, encode(task), task.ID, expected)
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
