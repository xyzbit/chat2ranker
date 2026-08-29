package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/xyzbit/chat2ranker/rank/backend/internal/domain"
	_ "modernc.org/sqlite"
)

type querier interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type Store struct {
	db *sql.DB
	q  querier
	tx *sql.Tx
}

func Open(ctx context.Context, dsn string) (*Store, error) {
	separator := "?"
	if strings.Contains(dsn, "?") {
		separator = "&"
	}
	configuredDSN := dsn + separator + "_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", configuredDSN)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	store := &Store{db: db, q: db}
	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys = ON; PRAGMA busy_timeout = 5000;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("configure sqlite: %w", err)
	}
	if err := store.Migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) WithinTx(ctx context.Context, fn func(domain.Repository) error) error {
	if s.tx != nil {
		return fn(s)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	txStore := &Store{db: s.db, q: tx, tx: tx}
	if err := fn(txStore); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

func encode(value any) string {
	payload, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(payload)
}

func decode(value string, target any) error {
	if value == "" {
		value = "null"
	}
	return json.Unmarshal([]byte(value), target)
}

func stamp(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }

func parseStamp(value string) (time.Time, error) { return time.Parse(time.RFC3339Nano, value) }

func nullableStamp(value *time.Time) any {
	if value == nil {
		return nil
	}
	return stamp(*value)
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func translate(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil && (strings.Contains(err.Error(), "UNIQUE constraint failed") || strings.Contains(err.Error(), "constraint failed")) {
		return fmt.Errorf("%w: %v", domain.ErrConflict, err)
	}
	return err
}

func (s *Store) ListDatasetFamilies(ctx context.Context) ([]domain.DatasetFamily, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT id,name,description,latest_version_id,created_at FROM dataset_families ORDER BY created_at,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.DatasetFamily{}
	for rows.Next() {
		var item domain.DatasetFamily
		var created string
		if err := rows.Scan(&item.ID, &item.Name, &item.Description, &item.LatestVersionID, &created); err != nil {
			return nil, err
		}
		item.CreatedAt, err = parseStamp(created)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListDatasetVersions(ctx context.Context, familyID string) ([]domain.DatasetVersion, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT v.id,v.family_id,f.name,v.version,v.source,v.description,v.schema_json,v.rubric_json,v.evaluator_json,v.cases_json,v.created_at FROM dataset_versions v JOIN dataset_families f ON f.id=v.family_id WHERE v.family_id=? ORDER BY v.version DESC`, familyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.DatasetVersion{}
	for rows.Next() {
		item, err := scanDatasetVersion(rows.Scan)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetDatasetVersion(ctx context.Context, id string) (domain.DatasetVersion, error) {
	row := s.q.QueryRowContext(ctx, `SELECT v.id,v.family_id,f.name,v.version,v.source,v.description,v.schema_json,v.rubric_json,v.evaluator_json,v.cases_json,v.created_at FROM dataset_versions v JOIN dataset_families f ON f.id=v.family_id WHERE v.id=?`, id)
	item, err := scanDatasetVersion(row.Scan)
	return item, translate(err)
}

type scanFunc func(...any) error

func scanDatasetVersion(scan scanFunc) (domain.DatasetVersion, error) {
	var item domain.DatasetVersion
	var schemaJSON, rubricJSON, evaluatorJSON, casesJSON, created string
	if err := scan(&item.ID, &item.FamilyID, &item.Name, &item.Version, &item.Source, &item.Description, &schemaJSON, &rubricJSON, &evaluatorJSON, &casesJSON, &created); err != nil {
		return item, err
	}
	item.Schema = json.RawMessage(schemaJSON)
	item.Rubric = json.RawMessage(rubricJSON)
	if err := decode(evaluatorJSON, &item.Evaluator); err != nil {
		return item, err
	}
	if err := decode(casesJSON, &item.Cases); err != nil {
		return item, err
	}
	item.CaseCount = len(item.Cases)
	var err error
	item.CreatedAt, err = parseStamp(created)
	return item, err
}

func (s *Store) CreateDatasetFamily(ctx context.Context, family domain.DatasetFamily, version domain.DatasetVersion) error {
	if _, err := s.q.ExecContext(ctx, `INSERT INTO dataset_families(id,name,description,latest_version_id,created_at) VALUES(?,?,?,?,?)`, family.ID, family.Name, family.Description, family.LatestVersionID, stamp(family.CreatedAt)); err != nil {
		return translate(err)
	}
	return s.insertDatasetVersion(ctx, version)
}

func (s *Store) CreateDatasetVersion(ctx context.Context, version domain.DatasetVersion) error {
	if err := s.insertDatasetVersion(ctx, version); err != nil {
		return err
	}
	_, err := s.q.ExecContext(ctx, `UPDATE dataset_families SET latest_version_id=?,description=? WHERE id=?`, version.ID, version.Description, version.FamilyID)
	return translate(err)
}

func (s *Store) insertDatasetVersion(ctx context.Context, version domain.DatasetVersion) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO dataset_versions(id,family_id,version,source,description,schema_json,rubric_json,evaluator_json,cases_json,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, version.ID, version.FamilyID, version.Version, version.Source, version.Description, string(version.Schema), string(version.Rubric), encode(version.Evaluator), encode(version.Cases), stamp(version.CreatedAt))
	return translate(err)
}

func (s *Store) ListAgentFamilies(ctx context.Context) ([]domain.AgentFamily, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT id,name,handle,description,latest_version_id,created_at FROM agent_families ORDER BY created_at,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.AgentFamily{}
	for rows.Next() {
		var item domain.AgentFamily
		var created string
		if err := rows.Scan(&item.ID, &item.Name, &item.Handle, &item.Description, &item.LatestVersionID, &created); err != nil {
			return nil, err
		}
		item.CreatedAt, err = parseStamp(created)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListAgentVersions(ctx context.Context, familyID string) ([]domain.AgentVersion, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT v.id,v.family_id,f.name,f.handle,v.version,v.runner_type,v.description,v.model,v.model_connection_id,v.preset,v.system_prompt,v.tools_json,v.skills_json,v.created_at FROM agent_versions v JOIN agent_families f ON f.id=v.family_id WHERE v.family_id=? ORDER BY v.version DESC`, familyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.AgentVersion{}
	for rows.Next() {
		item, err := scanAgentVersion(rows.Scan)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetAgentVersion(ctx context.Context, id string) (domain.AgentVersion, error) {
	row := s.q.QueryRowContext(ctx, `SELECT v.id,v.family_id,f.name,f.handle,v.version,v.runner_type,v.description,v.model,v.model_connection_id,v.preset,v.system_prompt,v.tools_json,v.skills_json,v.created_at FROM agent_versions v JOIN agent_families f ON f.id=v.family_id WHERE v.id=?`, id)
	item, err := scanAgentVersion(row.Scan)
	return item, translate(err)
}

func scanAgentVersion(scan scanFunc) (domain.AgentVersion, error) {
	var item domain.AgentVersion
	var toolsJSON, skillsJSON, created string
	if err := scan(&item.ID, &item.FamilyID, &item.Name, &item.Handle, &item.Version, &item.RunnerType, &item.Description, &item.Model, &item.ModelConnectionID, &item.Preset, &item.SystemPrompt, &toolsJSON, &skillsJSON, &created); err != nil {
		return item, err
	}
	if err := decode(toolsJSON, &item.Tools); err != nil {
		return item, err
	}
	if err := decode(skillsJSON, &item.Skills); err != nil {
		return item, err
	}
	var err error
	item.CreatedAt, err = parseStamp(created)
	return item, err
}

func (s *Store) CreateAgentFamily(ctx context.Context, family domain.AgentFamily, version domain.AgentVersion) error {
	if _, err := s.q.ExecContext(ctx, `INSERT INTO agent_families(id,name,handle,description,latest_version_id,created_at) VALUES(?,?,?,?,?,?)`, family.ID, family.Name, family.Handle, family.Description, family.LatestVersionID, stamp(family.CreatedAt)); err != nil {
		return translate(err)
	}
	return s.insertAgentVersion(ctx, version)
}

func (s *Store) CreateAgentVersion(ctx context.Context, version domain.AgentVersion) error {
	if err := s.insertAgentVersion(ctx, version); err != nil {
		return err
	}
	_, err := s.q.ExecContext(ctx, `UPDATE agent_families SET latest_version_id=?,description=? WHERE id=?`, version.ID, version.Description, version.FamilyID)
	return translate(err)
}

func (s *Store) insertAgentVersion(ctx context.Context, version domain.AgentVersion) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO agent_versions(id,family_id,version,runner_type,description,model,model_connection_id,preset,system_prompt,tools_json,skills_json,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, version.ID, version.FamilyID, version.Version, version.RunnerType, version.Description, version.Model, version.ModelConnectionID, version.Preset, version.SystemPrompt, encode(version.Tools), encode(version.Skills), stamp(version.CreatedAt))
	return translate(err)
}

func (s *Store) ListExperiments(ctx context.Context) ([]domain.ExperimentSummary, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT e.id,e.title,COALESCE(e.dataset_version_id,''),COALESCE(e.agent_version_id,''),COALESCE(e.control_session_id,''),e.created_at,e.updated_at,(SELECT COUNT(*) FROM runs r WHERE r.experiment_id=e.id) FROM experiments e ORDER BY e.updated_at DESC`)
	if err != nil {
		return nil, err
	}
	items := []domain.ExperimentSummary{}
	for rows.Next() {
		var item domain.ExperimentSummary
		var created, updated string
		if err := rows.Scan(&item.ID, &item.Title, &item.DatasetVersionID, &item.AgentVersionID, &item.ControlSessionID, &created, &updated, &item.RunCount); err != nil {
			return nil, err
		}
		item.CreatedAt, err = parseStamp(created)
		if err != nil {
			return nil, err
		}
		item.UpdatedAt, err = parseStamp(updated)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for index := range items {
		latest, latestErr := s.latestRunSummary(ctx, items[index].ID)
		if latestErr == nil {
			items[index].LatestRun = &latest
		} else if !errors.Is(latestErr, domain.ErrNotFound) {
			return nil, latestErr
		}
		summary, summaryErr := s.experimentRunSummary(ctx, items[index].ID)
		if summaryErr == nil {
			items[index].RunSummary = &summary
		} else if !errors.Is(summaryErr, domain.ErrNotFound) {
			return nil, summaryErr
		}
	}
	return items, nil
}

func (s *Store) experimentRunSummary(ctx context.Context, experimentID string) (domain.ExperimentRunSummary, error) {
	var item domain.ExperimentRunSummary
	var costKnown, costEstimated int
	if err := s.q.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(passed),0),COALESCE(SUM(total),0),COALESCE(SUM(cost),0),COALESCE(MIN(cost_known),0),COALESCE(MAX(cost_estimated),0) FROM runs WHERE experiment_id=? AND status='complete'`, experimentID).Scan(&item.Completed, &item.Passed, &item.Total, &item.Cost, &costKnown, &costEstimated); err != nil {
		return item, translate(err)
	}
	if item.Completed == 0 {
		return item, domain.ErrNotFound
	}
	item.CostKnown = costKnown != 0
	item.CostEstimated = costEstimated != 0
	return item, nil
}

func (s *Store) latestRunSummary(ctx context.Context, experimentID string) (domain.RunSummary, error) {
	var item domain.RunSummary
	var costKnown, costEstimated int
	var created string
	var completed sql.NullString
	err := s.q.QueryRowContext(ctx, `SELECT id,status,passed,total,pass_rate,trial_count,reliable_cases,case_count,cost,cost_known,cost_estimated,duration_ms,created_at,completed_at FROM runs WHERE experiment_id=? ORDER BY created_at DESC LIMIT 1`, experimentID).Scan(&item.ID, &item.Status, &item.Passed, &item.Total, &item.PassRate, &item.TrialCount, &item.ReliableCases, &item.CaseCount, &item.Cost, &costKnown, &costEstimated, &item.DurationMs, &created, &completed)
	if err != nil {
		return item, translate(err)
	}
	item.CreatedAt, err = parseStamp(created)
	if err != nil {
		return item, err
	}
	if completed.Valid {
		value, err := parseStamp(completed.String)
		if err != nil {
			return item, err
		}
		item.CompletedAt = &value
	}
	item.CostKnown = costKnown != 0
	item.CostEstimated = costEstimated != 0
	return item, nil
}

func (s *Store) GetExperiment(ctx context.Context, id string) (domain.Experiment, error) {
	var item domain.Experiment
	var created, updated string
	err := s.q.QueryRowContext(ctx, `SELECT id,title,COALESCE(dataset_version_id,''),COALESCE(agent_version_id,''),COALESCE(control_session_id,''),created_at,updated_at FROM experiments WHERE id=?`, id).Scan(&item.ID, &item.Title, &item.DatasetVersionID, &item.AgentVersionID, &item.ControlSessionID, &created, &updated)
	if err != nil {
		return item, translate(err)
	}
	item.CreatedAt, err = parseStamp(created)
	if err != nil {
		return item, err
	}
	item.UpdatedAt, err = parseStamp(updated)
	return item, err
}

func (s *Store) CreateExperiment(ctx context.Context, experiment domain.Experiment, initial domain.Message) error {
	if _, err := s.q.ExecContext(ctx, `INSERT INTO experiments(id,title,dataset_version_id,agent_version_id,control_session_id,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, experiment.ID, experiment.Title, nullableText(experiment.DatasetVersionID), nullableText(experiment.AgentVersionID), nullableText(experiment.ControlSessionID), stamp(experiment.CreatedAt), stamp(experiment.UpdatedAt)); err != nil {
		return translate(err)
	}
	return s.insertMessage(ctx, initial)
}

func nullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (s *Store) UpdateExperimentSelection(ctx context.Context, experiment domain.Experiment, message domain.Message) error {
	result, err := s.q.ExecContext(ctx, `UPDATE experiments SET title=?,dataset_version_id=?,agent_version_id=?,updated_at=? WHERE id=?`, experiment.Title, nullableText(experiment.DatasetVersionID), nullableText(experiment.AgentVersionID), stamp(experiment.UpdatedAt), experiment.ID)
	if err != nil {
		return translate(err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return domain.ErrNotFound
	}
	return s.insertMessage(ctx, message)
}

func (s *Store) BindControlSession(ctx context.Context, experimentID, controlSessionID string, updatedAt time.Time) error {
	result, err := s.q.ExecContext(ctx, `UPDATE experiments SET control_session_id=?,updated_at=? WHERE id=? AND (control_session_id IS NULL OR control_session_id=?)`, controlSessionID, stamp(updatedAt), experimentID, controlSessionID)
	if err != nil {
		return translate(err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		var existing string
		if scanErr := s.q.QueryRowContext(ctx, `SELECT COALESCE(control_session_id,'') FROM experiments WHERE id=?`, experimentID).Scan(&existing); scanErr != nil {
			return translate(scanErr)
		}
		return domain.ErrConflict
	}
	return nil
}

func (s *Store) UpdateExperimentControl(ctx context.Context, experiment domain.Experiment) error {
	result, err := s.q.ExecContext(ctx, `UPDATE experiments SET title=?,dataset_version_id=?,agent_version_id=?,updated_at=? WHERE id=? AND control_session_id=?`, experiment.Title, nullableText(experiment.DatasetVersionID), nullableText(experiment.AgentVersionID), stamp(experiment.UpdatedAt), experiment.ID, experiment.ControlSessionID)
	if err != nil {
		return translate(err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return domain.ErrConflict
	}
	return nil
}

func (s *Store) ListMessages(ctx context.Context, experimentID string) ([]domain.Message, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT id,experiment_id,role,content,COALESCE(run_id,''),created_at FROM messages WHERE experiment_id=? ORDER BY created_at,id`, experimentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.Message{}
	for rows.Next() {
		var item domain.Message
		var created string
		if err := rows.Scan(&item.ID, &item.ExperimentID, &item.Role, &item.Content, &item.RunID, &created); err != nil {
			return nil, err
		}
		item.CreatedAt, err = parseStamp(created)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) AddMessages(ctx context.Context, experimentID string, messages ...domain.Message) error {
	for _, message := range messages {
		if err := s.insertMessage(ctx, message); err != nil {
			return err
		}
	}
	if len(messages) > 0 {
		_, err := s.q.ExecContext(ctx, `UPDATE experiments SET updated_at=? WHERE id=?`, stamp(messages[len(messages)-1].CreatedAt), experimentID)
		return translate(err)
	}
	return nil
}

func (s *Store) AppendControlMessages(ctx context.Context, experimentID string, messages ...domain.Message) error {
	for _, message := range messages {
		if _, err := s.q.ExecContext(ctx, `INSERT OR IGNORE INTO messages(id,experiment_id,role,content,run_id,created_at) VALUES(?,?,?,?,?,?)`, message.ID, message.ExperimentID, message.Role, message.Content, nullableText(message.RunID), stamp(message.CreatedAt)); err != nil {
			return translate(err)
		}
	}
	if len(messages) > 0 {
		_, err := s.q.ExecContext(ctx, `UPDATE experiments SET updated_at=CASE WHEN updated_at < ? THEN ? ELSE updated_at END WHERE id=?`, stamp(messages[len(messages)-1].CreatedAt), stamp(messages[len(messages)-1].CreatedAt), experimentID)
		return translate(err)
	}
	return nil
}

func (s *Store) GetControlCommandByIdempotencyKey(ctx context.Context, experimentID, key string) (domain.ControlCommand, error) {
	var item domain.ControlCommand
	var payload, result, created string
	err := s.q.QueryRowContext(ctx, `SELECT id,experiment_id,control_session_id,idempotency_key,type,payload_json,result_json,created_at FROM control_commands WHERE experiment_id=? AND idempotency_key=?`, experimentID, key).Scan(&item.ID, &item.ExperimentID, &item.ControlSessionID, &item.IdempotencyKey, &item.Type, &payload, &result, &created)
	if err != nil {
		return item, translate(err)
	}
	item.Payload = json.RawMessage(payload)
	item.Result = json.RawMessage(result)
	item.CreatedAt, err = parseStamp(created)
	return item, err
}

func (s *Store) CreateControlCommand(ctx context.Context, command domain.ControlCommand, event domain.ControlEvent) error {
	if _, err := s.q.ExecContext(ctx, `INSERT INTO control_commands(id,experiment_id,control_session_id,idempotency_key,type,payload_json,result_json,created_at) VALUES(?,?,?,?,?,?,?,?)`, command.ID, command.ExperimentID, command.ControlSessionID, command.IdempotencyKey, command.Type, string(command.Payload), string(command.Result), stamp(command.CreatedAt)); err != nil {
		return translate(err)
	}
	_, err := s.q.ExecContext(ctx, `INSERT INTO control_events(experiment_id,control_session_id,command_id,type,payload_json,created_at) VALUES(?,?,?,?,?,?)`, event.ExperimentID, event.ControlSessionID, event.CommandID, event.Type, string(event.Payload), stamp(event.CreatedAt))
	return translate(err)
}

func (s *Store) ListControlEvents(ctx context.Context, experimentID string) ([]domain.ControlEvent, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT id,experiment_id,control_session_id,command_id,type,payload_json,created_at FROM control_events WHERE experiment_id=? ORDER BY id`, experimentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.ControlEvent{}
	for rows.Next() {
		var item domain.ControlEvent
		var payload, created string
		if err := rows.Scan(&item.ID, &item.ExperimentID, &item.ControlSessionID, &item.CommandID, &item.Type, &payload, &created); err != nil {
			return nil, err
		}
		item.Payload = json.RawMessage(payload)
		item.CreatedAt, err = parseStamp(created)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) insertMessage(ctx context.Context, message domain.Message) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO messages(id,experiment_id,role,content,run_id,created_at) VALUES(?,?,?,?,?,?)`, message.ID, message.ExperimentID, message.Role, message.Content, nullableText(message.RunID), stamp(message.CreatedAt))
	return translate(err)
}

func (s *Store) GetRun(ctx context.Context, id string) (domain.Run, error) {
	row := s.q.QueryRowContext(ctx, `SELECT id,experiment_id,COALESCE(group_id,''),idempotency_key,status,dataset_snapshot_json,agent_snapshot_json,evaluator_snapshot_json,trial_count,concurrency,created_at,started_at,completed_at,duration_ms,passed,total,pass_rate,scheduled_trials,valid_trials,infra_failures,grading_failures,reliable_cases,case_count,pass_hat_3,evaluation_complete,cost,candidate_cost,evaluation_cost,cost_known,cost_estimated,error FROM runs WHERE id=?`, id)
	run, err := scanRun(row.Scan)
	if err != nil {
		return run, translate(err)
	}
	items, err := s.ListRunItems(ctx, id)
	if err != nil {
		return run, err
	}
	for _, item := range items {
		if item.Result != nil {
			run.Results = append(run.Results, *item.Result)
		}
	}
	trials, err := s.ListRunTrials(ctx, id)
	if err != nil {
		return run, err
	}
	trialsByCase := map[string][]domain.TrialResult{}
	for _, trial := range trials {
		if trial.Status == domain.TrialComplete {
			run.CompletedTrials++
		}
		if trial.Result != nil {
			trialsByCase[trial.CaseID] = append(trialsByCase[trial.CaseID], *trial.Result)
		}
	}
	for index := range run.Results {
		run.Results[index].Trials = trialsByCase[run.Results[index].CaseID]
	}
	run.Events, err = s.ListRunEvents(ctx, id, 0)
	return run, err
}

func (s *Store) GetRunByIdempotencyKey(ctx context.Context, experimentID, key string) (domain.Run, error) {
	var id string
	if err := s.q.QueryRowContext(ctx, `SELECT id FROM runs WHERE experiment_id=? AND idempotency_key=?`, experimentID, key).Scan(&id); err != nil {
		return domain.Run{}, translate(err)
	}
	return s.GetRun(ctx, id)
}

func (s *Store) ListRunsByExperiment(ctx context.Context, experimentID string) ([]domain.Run, error) {
	return s.listRuns(ctx, `SELECT id FROM runs WHERE experiment_id=? ORDER BY created_at`, experimentID)
}

func (s *Store) ListActiveRuns(ctx context.Context) ([]domain.Run, error) {
	return s.listRuns(ctx, `SELECT id FROM runs WHERE status IN ('queued','preparing','running','scoring') ORDER BY created_at`)
}

func (s *Store) GetRunGroup(ctx context.Context, id string) (domain.RunGroup, error) {
	var group domain.RunGroup
	var agentIDsJSON, created string
	if err := s.q.QueryRowContext(ctx, `SELECT id,experiment_id,idempotency_key,dataset_version_id,agent_version_ids_json,trial_count,created_at FROM run_groups WHERE id=?`, id).Scan(&group.ID, &group.ExperimentID, &group.IdempotencyKey, &group.DatasetVersionID, &agentIDsJSON, &group.TrialCount, &created); err != nil {
		return group, translate(err)
	}
	if err := decode(agentIDsJSON, &group.AgentVersionIDs); err != nil {
		return group, err
	}
	var err error
	if group.CreatedAt, err = parseStamp(created); err != nil {
		return group, err
	}
	rows, err := s.q.QueryContext(ctx, `SELECT id,status FROM runs WHERE group_id=? ORDER BY created_at,id`, id)
	if err != nil {
		return group, err
	}
	defer rows.Close()
	statuses := []string{}
	for rows.Next() {
		var runID, status string
		if err := rows.Scan(&runID, &status); err != nil {
			return group, err
		}
		group.RunIDs = append(group.RunIDs, runID)
		statuses = append(statuses, status)
	}
	if err := rows.Err(); err != nil {
		return group, err
	}
	group.Status = runGroupStatus(statuses)
	return group, nil
}

func (s *Store) GetRunGroupByIdempotencyKey(ctx context.Context, experimentID, key string) (domain.RunGroup, error) {
	var id string
	if err := s.q.QueryRowContext(ctx, `SELECT id FROM run_groups WHERE experiment_id=? AND idempotency_key=?`, experimentID, key).Scan(&id); err != nil {
		return domain.RunGroup{}, translate(err)
	}
	return s.GetRunGroup(ctx, id)
}

func (s *Store) CreateRunGroup(ctx context.Context, group domain.RunGroup) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO run_groups(id,experiment_id,idempotency_key,dataset_version_id,agent_version_ids_json,trial_count,created_at) VALUES(?,?,?,?,?,?,?)`, group.ID, group.ExperimentID, group.IdempotencyKey, group.DatasetVersionID, encode(group.AgentVersionIDs), group.TrialCount, stamp(group.CreatedAt))
	return translate(err)
}

func runGroupStatus(statuses []string) string {
	if len(statuses) == 0 {
		return domain.RunQueued
	}
	complete, cancelled, failed, queued := 0, 0, 0, 0
	for _, status := range statuses {
		switch status {
		case domain.RunComplete:
			complete++
		case domain.RunCancelled:
			cancelled++
		case domain.RunFailed:
			failed++
		case domain.RunQueued:
			queued++
		default:
			return domain.RunRunning
		}
	}
	if queued == len(statuses) {
		return domain.RunQueued
	}
	if queued > 0 {
		return domain.RunRunning
	}
	if complete == len(statuses) {
		return domain.RunComplete
	}
	if cancelled == len(statuses) {
		return domain.RunCancelled
	}
	if failed == len(statuses) {
		return domain.RunFailed
	}
	return "partial"
}

func (s *Store) listRuns(ctx context.Context, query string, args ...any) ([]domain.Run, error) {
	rows, err := s.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	runs := make([]domain.Run, 0, len(ids))
	for _, id := range ids {
		run, err := s.GetRun(ctx, id)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, nil
}

func scanRun(scan scanFunc) (domain.Run, error) {
	run := domain.Run{Results: []domain.CaseResult{}, Events: []domain.RunEvent{}}
	var datasetJSON, agentJSON, evaluatorJSON, created string
	var started, completed sql.NullString
	var costKnown, costEstimated, evaluationComplete int
	if err := scan(&run.ID, &run.ExperimentID, &run.GroupID, &run.IdempotencyKey, &run.Status, &datasetJSON, &agentJSON, &evaluatorJSON, &run.TrialCount, &run.Concurrency, &created, &started, &completed, &run.DurationMs, &run.Passed, &run.Total, &run.PassRate, &run.ScheduledTrials, &run.ValidTrials, &run.InfraFailures, &run.GradingFailures, &run.ReliableCases, &run.CaseCount, &run.PassHat3, &evaluationComplete, &run.Cost, &run.CandidateCost, &run.EvaluationCost, &costKnown, &costEstimated, &run.Error); err != nil {
		return run, err
	}
	if err := decode(datasetJSON, &run.DatasetSnapshot); err != nil {
		return run, err
	}
	if err := decode(agentJSON, &run.AgentSnapshot); err != nil {
		return run, err
	}
	if err := decode(evaluatorJSON, &run.EvaluatorSnapshot); err != nil {
		return run, err
	}
	run.CostKnown = costKnown != 0
	run.CostEstimated = costEstimated != 0
	run.EvaluationComplete = evaluationComplete != 0
	var err error
	run.CreatedAt, err = parseStamp(created)
	if err != nil {
		return run, err
	}
	if started.Valid {
		value, err := parseStamp(started.String)
		if err != nil {
			return run, err
		}
		run.StartedAt = &value
	}
	if completed.Valid {
		value, err := parseStamp(completed.String)
		if err != nil {
			return run, err
		}
		run.CompletedAt = &value
	}
	return run, nil
}

func (s *Store) CreateRun(ctx context.Context, run domain.Run, items []domain.RunItem, trials []domain.RunTrial, created domain.RunEvent) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO runs(id,experiment_id,group_id,idempotency_key,status,dataset_snapshot_json,agent_snapshot_json,evaluator_snapshot_json,trial_count,concurrency,created_at,duration_ms,passed,total,pass_rate,scheduled_trials,valid_trials,infra_failures,grading_failures,reliable_cases,case_count,pass_hat_3,evaluation_complete,cost,candidate_cost,evaluation_cost,cost_known,cost_estimated,error) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, run.ID, run.ExperimentID, nullableText(run.GroupID), run.IdempotencyKey, run.Status, encode(run.DatasetSnapshot), encode(run.AgentSnapshot), encode(run.EvaluatorSnapshot), run.TrialCount, run.Concurrency, stamp(run.CreatedAt), run.DurationMs, run.Passed, run.Total, run.PassRate, run.ScheduledTrials, run.ValidTrials, run.InfraFailures, run.GradingFailures, run.ReliableCases, run.CaseCount, run.PassHat3, boolInt(run.EvaluationComplete), run.Cost, run.CandidateCost, run.EvaluationCost, boolInt(run.CostKnown), boolInt(run.CostEstimated), run.Error)
	if err != nil {
		return translate(err)
	}
	for _, item := range items {
		_, err = s.q.ExecContext(ctx, `INSERT INTO run_items(id,run_id,case_id,title,ordinal,status,created_at) VALUES(?,?,?,?,?,?,?)`, item.ID, item.RunID, item.CaseID, item.Title, item.Ordinal, item.Status, stamp(item.CreatedAt))
		if err != nil {
			return translate(err)
		}
	}
	for _, trial := range trials {
		_, err = s.q.ExecContext(ctx, `INSERT INTO run_trials(id,run_id,item_id,case_id,trial_index,ordinal,status,created_at) VALUES(?,?,?,?,?,?,?,?)`, trial.ID, trial.RunID, trial.ItemID, trial.CaseID, trial.TrialIndex, trial.Ordinal, trial.Status, stamp(trial.CreatedAt))
		if err != nil {
			return translate(err)
		}
	}
	if err := s.AppendRunEvent(ctx, created); err != nil {
		return err
	}
	_, err = s.q.ExecContext(ctx, `UPDATE experiments SET updated_at=? WHERE id=?`, stamp(run.CreatedAt), run.ExperimentID)
	return translate(err)
}

func (s *Store) UpdateRunStatus(ctx context.Context, runID, status string, at time.Time, runError string) error {
	var result sql.Result
	var err error
	if status == domain.RunPreparing || status == domain.RunRunning {
		result, err = s.q.ExecContext(ctx, `UPDATE runs SET status=?,started_at=COALESCE(started_at,?),error=? WHERE id=? AND status NOT IN ('complete','failed','cancelled')`, status, stamp(at), runError, runID)
	} else if status == domain.RunFailed {
		result, err = s.q.ExecContext(ctx, `UPDATE runs SET status=?,completed_at=?,error=? WHERE id=? AND status NOT IN ('complete','failed','cancelled')`, status, stamp(at), runError, runID)
	} else {
		result, err = s.q.ExecContext(ctx, `UPDATE runs SET status=?,error=? WHERE id=? AND status NOT IN ('complete','failed','cancelled')`, status, runError, runID)
	}
	if err != nil {
		return translate(err)
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return domain.ErrConflict
	}
	return nil
}

func (s *Store) ListRunItems(ctx context.Context, runID string) ([]domain.RunItem, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT i.id,i.run_id,i.case_id,i.title,i.ordinal,i.status,COALESCE(i.result_key,''),i.passed,i.cost,i.score,i.output,i.reason,i.duration_ms,i.created_at,i.started_at,i.completed_at,COALESCE(e.execution_id,''),COALESCE(e.judge_execution_id,''),COALESCE(e.cost_known,1),COALESCE(e.usage_json,'{}'),COALESCE(e.artifacts_json,'[]'),COALESCE(i.result_json,'') FROM run_items i LEFT JOIN run_item_executions e ON e.item_id=i.id WHERE i.run_id=? ORDER BY i.ordinal`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.RunItem{}
	for rows.Next() {
		var item domain.RunItem
		var passed, cost, score sql.NullFloat64
		var duration sql.NullInt64
		var output, reason sql.NullString
		var created, executionID, judgeExecutionID, usageJSON, artifactsJSON, resultJSON string
		var costKnown int
		var started, completed sql.NullString
		if err := rows.Scan(&item.ID, &item.RunID, &item.CaseID, &item.Title, &item.Ordinal, &item.Status, &item.ResultKey, &passed, &cost, &score, &output, &reason, &duration, &created, &started, &completed, &executionID, &judgeExecutionID, &costKnown, &usageJSON, &artifactsJSON, &resultJSON); err != nil {
			return nil, err
		}
		item.CreatedAt, err = parseStamp(created)
		if err != nil {
			return nil, err
		}
		if started.Valid {
			value, e := parseStamp(started.String)
			if e != nil {
				return nil, e
			}
			item.StartedAt = &value
		}
		if completed.Valid {
			value, e := parseStamp(completed.String)
			if e != nil {
				return nil, e
			}
			item.CompletedAt = &value
		}
		if resultJSON != "" {
			item.Result = &domain.CaseResult{}
			if err := decode(resultJSON, item.Result); err != nil {
				return nil, err
			}
		} else if passed.Valid {
			item.Result = &domain.CaseResult{CaseID: item.CaseID, Title: item.Title, Passed: passed.Float64 != 0, Cost: cost.Float64, CostKnown: costKnown != 0, Score: score.Float64, Output: output.String, Reason: reason.String, DurationMs: duration.Int64, ExecutionID: executionID, JudgeExecutionID: judgeExecutionID}
			if err := json.Unmarshal([]byte(usageJSON), &item.Result.Usage); err != nil {
				return nil, err
			}
			if err := json.Unmarshal([]byte(artifactsJSON), &item.Result.Artifacts); err != nil {
				return nil, err
			}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListRunTrials(ctx context.Context, runID string) ([]domain.RunTrial, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT id,run_id,item_id,case_id,trial_index,ordinal,status,COALESCE(result_key,''),COALESCE(result_json,''),created_at,started_at,completed_at FROM run_trials WHERE run_id=? ORDER BY ordinal`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.RunTrial{}
	for rows.Next() {
		var item domain.RunTrial
		var resultJSON, created string
		var started, completed sql.NullString
		if err := rows.Scan(&item.ID, &item.RunID, &item.ItemID, &item.CaseID, &item.TrialIndex, &item.Ordinal, &item.Status, &item.ResultKey, &resultJSON, &created, &started, &completed); err != nil {
			return nil, err
		}
		item.CreatedAt, err = parseStamp(created)
		if err != nil {
			return nil, err
		}
		if started.Valid {
			value, parseErr := parseStamp(started.String)
			if parseErr != nil {
				return nil, parseErr
			}
			item.StartedAt = &value
		}
		if completed.Valid {
			value, parseErr := parseStamp(completed.String)
			if parseErr != nil {
				return nil, parseErr
			}
			item.CompletedAt = &value
		}
		if resultJSON != "" {
			item.Result = &domain.TrialResult{}
			if err := decode(resultJSON, item.Result); err != nil {
				return nil, err
			}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ResetActiveRunTrials(ctx context.Context, runID string) error {
	_, err := s.q.ExecContext(ctx, `UPDATE run_trials SET status='queued',started_at=NULL WHERE run_id=? AND status='running'`, runID)
	return translate(err)
}

func (s *Store) ClaimRunTrial(ctx context.Context, runID, trialID string, startedAt time.Time) (bool, error) {
	result, err := s.q.ExecContext(ctx, `UPDATE run_trials SET status='running',started_at=? WHERE id=? AND run_id=? AND status='queued'`, stamp(startedAt), trialID, runID)
	if err != nil {
		return false, translate(err)
	}
	count, _ := result.RowsAffected()
	return count == 1, nil
}

func (s *Store) CompleteRunTrial(ctx context.Context, trialID, resultKey string, result domain.TrialResult, completedAt time.Time, events []domain.RunEvent) (bool, error) {
	var status, existingKey string
	if err := s.q.QueryRowContext(ctx, `SELECT status,COALESCE(result_key,'') FROM run_trials WHERE id=?`, trialID).Scan(&status, &existingKey); err != nil {
		return false, translate(err)
	}
	if status == domain.TrialComplete {
		if existingKey == resultKey {
			return false, nil
		}
		return false, domain.ErrConflict
	}
	updated, err := s.q.ExecContext(ctx, `UPDATE run_trials SET status='complete',result_key=?,result_json=?,completed_at=? WHERE id=? AND status='running'`, resultKey, encode(result), stamp(completedAt), trialID)
	if err != nil {
		return false, translate(err)
	}
	count, _ := updated.RowsAffected()
	if count != 1 {
		return false, domain.ErrConflict
	}
	for _, event := range events {
		if err := s.AppendRunEvent(ctx, event); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (s *Store) SaveRunItemAggregate(ctx context.Context, itemID string, result domain.CaseResult, completedAt time.Time) error {
	updated, err := s.q.ExecContext(ctx, `UPDATE run_items SET status='complete',result_key=?,passed=?,cost=?,score=?,output=?,reason=?,duration_ms=?,result_json=?,completed_at=? WHERE id=?`, "aggregate:"+itemID, boolInt(result.Passed), result.Cost, result.Score, result.Output, result.Reason, result.DurationMs, encode(result), stamp(completedAt), itemID)
	if err != nil {
		return translate(err)
	}
	if count, _ := updated.RowsAffected(); count != 1 {
		return domain.ErrNotFound
	}
	_, err = s.q.ExecContext(ctx, `INSERT INTO run_item_executions(item_id,execution_id,judge_execution_id,cost_known,usage_json,artifacts_json) VALUES(?,?,?,?,?,?) ON CONFLICT(item_id) DO UPDATE SET execution_id=excluded.execution_id,judge_execution_id=excluded.judge_execution_id,cost_known=excluded.cost_known,usage_json=excluded.usage_json,artifacts_json=excluded.artifacts_json`, itemID, result.ExecutionID, result.JudgeExecutionID, boolInt(result.CostKnown), encode(result.Usage), encode(result.Artifacts))
	return translate(err)
}

func (s *Store) FinishRun(ctx context.Context, run domain.Run, message domain.Message, event domain.RunEvent) error {
	result, err := s.q.ExecContext(ctx, `UPDATE runs SET status='complete',completed_at=?,duration_ms=?,passed=?,total=?,pass_rate=?,valid_trials=?,infra_failures=?,grading_failures=?,reliable_cases=?,case_count=?,pass_hat_3=?,evaluation_complete=?,cost=?,candidate_cost=?,evaluation_cost=?,cost_known=?,cost_estimated=?,error='' WHERE id=? AND status NOT IN ('complete','failed','cancelled')`, nullableStamp(run.CompletedAt), run.DurationMs, run.Passed, run.Total, run.PassRate, run.ValidTrials, run.InfraFailures, run.GradingFailures, run.ReliableCases, run.CaseCount, run.PassHat3, boolInt(run.EvaluationComplete), run.Cost, run.CandidateCost, run.EvaluationCost, boolInt(run.CostKnown), boolInt(run.CostEstimated), run.ID)
	if err != nil {
		return translate(err)
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return domain.ErrConflict
	}
	if err := s.insertMessage(ctx, message); err != nil {
		return err
	}
	if err := s.AppendRunEvent(ctx, event); err != nil {
		return err
	}
	_, err = s.q.ExecContext(ctx, `UPDATE experiments SET updated_at=? WHERE id=?`, stamp(message.CreatedAt), run.ExperimentID)
	return translate(err)
}

func (s *Store) CancelRun(ctx context.Context, runID string, at time.Time, event domain.RunEvent) error {
	result, err := s.q.ExecContext(ctx, `UPDATE runs SET status='cancelled',completed_at=?,error='运行已停止' WHERE id=? AND status IN ('queued','preparing','running','scoring')`, stamp(at), runID)
	if err != nil {
		return translate(err)
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return domain.ErrConflict
	}
	if _, err = s.q.ExecContext(ctx, `UPDATE run_items SET status='cancelled',completed_at=? WHERE run_id=? AND status IN ('queued','running')`, stamp(at), runID); err != nil {
		return translate(err)
	}
	if _, err = s.q.ExecContext(ctx, `UPDATE run_trials SET status='cancelled',completed_at=? WHERE run_id=? AND status IN ('queued','running')`, stamp(at), runID); err != nil {
		return translate(err)
	}
	return s.AppendRunEvent(ctx, event)
}

func (s *Store) AppendRunEvent(ctx context.Context, event domain.RunEvent) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO run_events(run_id,type,case_id,trial_id,trial_index,status,dataset_version_id,agent_version_id,passed,cost,score,output,reason,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, event.RunID, event.Type, nullableText(event.CaseID), nullableText(event.TrialID), event.TrialIndex, nullableText(event.Status), nullableText(event.DatasetVersionID), nullableText(event.AgentVersionID), nullableBool(event.Passed), nullableFloat(event.Cost), nullableFloat(event.Score), nullableText(event.Output), nullableText(event.Reason), stamp(event.At))
	return translate(err)
}

func nullableBool(value *bool) any {
	if value == nil {
		return nil
	}
	return boolInt(*value)
}
func nullableFloat(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func (s *Store) ListRunEvents(ctx context.Context, runID string, afterSequence int64) ([]domain.RunEvent, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT id,run_id,type,COALESCE(case_id,''),COALESCE(trial_id,''),trial_index,COALESCE(status,''),COALESCE(dataset_version_id,''),COALESCE(agent_version_id,''),passed,cost,score,COALESCE(output,''),COALESCE(reason,''),created_at FROM run_events WHERE run_id=? AND id>? ORDER BY id`, runID, afterSequence)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.RunEvent{}
	for rows.Next() {
		var item domain.RunEvent
		var passed sql.NullInt64
		var cost, score sql.NullFloat64
		var created string
		if err := rows.Scan(&item.ID, &item.RunID, &item.Type, &item.CaseID, &item.TrialID, &item.TrialIndex, &item.Status, &item.DatasetVersionID, &item.AgentVersionID, &passed, &cost, &score, &item.Output, &item.Reason, &created); err != nil {
			return nil, err
		}
		item.At, err = parseStamp(created)
		if err != nil {
			return nil, err
		}
		if passed.Valid {
			value := passed.Int64 != 0
			item.Passed = &value
		}
		if cost.Valid {
			value := cost.Float64
			item.Cost = &value
		}
		if score.Valid {
			value := score.Float64
			item.Score = &value
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
