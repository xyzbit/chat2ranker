package sqlstore

import (
	"context"
	"fmt"
)

const schemaV1 = `
CREATE TABLE IF NOT EXISTS schema_migrations(version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL);

CREATE TABLE IF NOT EXISTS dataset_families(
  id TEXT PRIMARY KEY, name TEXT NOT NULL, description TEXT NOT NULL,
  latest_version_id TEXT NOT NULL, created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS dataset_versions(
  id TEXT PRIMARY KEY, family_id TEXT NOT NULL REFERENCES dataset_families(id),
  version INTEGER NOT NULL, source TEXT NOT NULL, description TEXT NOT NULL,
  schema_json TEXT NOT NULL DEFAULT '{}', rubric_json TEXT NOT NULL DEFAULT '{}',
  cases_json TEXT NOT NULL, created_at TEXT NOT NULL,
  UNIQUE(family_id, version)
);

CREATE TABLE IF NOT EXISTS agent_families(
  id TEXT PRIMARY KEY, name TEXT NOT NULL, handle TEXT NOT NULL UNIQUE,
  description TEXT NOT NULL, latest_version_id TEXT NOT NULL, created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS agent_versions(
  id TEXT PRIMARY KEY, family_id TEXT NOT NULL REFERENCES agent_families(id),
  version INTEGER NOT NULL, runner_type TEXT NOT NULL, description TEXT NOT NULL,
  model TEXT NOT NULL, preset TEXT NOT NULL DEFAULT '', system_prompt TEXT NOT NULL DEFAULT '',
  tools_json TEXT NOT NULL, skills_json TEXT NOT NULL DEFAULT '[]', created_at TEXT NOT NULL,
  UNIQUE(family_id, version)
);

CREATE TABLE IF NOT EXISTS experiments(
  id TEXT PRIMARY KEY, title TEXT NOT NULL,
  dataset_version_id TEXT REFERENCES dataset_versions(id),
  agent_version_id TEXT REFERENCES agent_versions(id),
  control_session_id TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS messages(
  id TEXT PRIMARY KEY, experiment_id TEXT NOT NULL REFERENCES experiments(id),
  role TEXT NOT NULL, content TEXT NOT NULL, run_id TEXT, created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS messages_experiment_idx ON messages(experiment_id, created_at);

CREATE TABLE IF NOT EXISTS runs(
  id TEXT PRIMARY KEY, experiment_id TEXT NOT NULL REFERENCES experiments(id),
  idempotency_key TEXT NOT NULL, status TEXT NOT NULL,
  dataset_snapshot_json TEXT NOT NULL, agent_snapshot_json TEXT NOT NULL,
  concurrency INTEGER NOT NULL, created_at TEXT NOT NULL, started_at TEXT,
  completed_at TEXT, duration_ms INTEGER NOT NULL DEFAULT 0,
  passed INTEGER NOT NULL DEFAULT 0, total INTEGER NOT NULL,
  pass_rate INTEGER NOT NULL DEFAULT 0, cost REAL NOT NULL DEFAULT 0,
  cost_known INTEGER NOT NULL DEFAULT 1, error TEXT NOT NULL DEFAULT '',
  UNIQUE(experiment_id, idempotency_key)
);
CREATE INDEX IF NOT EXISTS runs_experiment_idx ON runs(experiment_id, created_at);
CREATE INDEX IF NOT EXISTS runs_status_idx ON runs(status, created_at);

CREATE TABLE IF NOT EXISTS run_items(
  id TEXT PRIMARY KEY, run_id TEXT NOT NULL REFERENCES runs(id),
  case_id TEXT NOT NULL, title TEXT NOT NULL, ordinal INTEGER NOT NULL,
  status TEXT NOT NULL, result_key TEXT, passed INTEGER, cost REAL, score REAL,
  output TEXT, reason TEXT, duration_ms INTEGER, created_at TEXT NOT NULL,
  started_at TEXT, completed_at TEXT, UNIQUE(run_id, case_id)
);
CREATE INDEX IF NOT EXISTS run_items_run_idx ON run_items(run_id, ordinal);

CREATE TABLE IF NOT EXISTS run_events(
  id INTEGER PRIMARY KEY AUTOINCREMENT, run_id TEXT NOT NULL REFERENCES runs(id),
  type TEXT NOT NULL, case_id TEXT, status TEXT, dataset_version_id TEXT,
  agent_version_id TEXT, passed INTEGER, cost REAL, score REAL,
  output TEXT, reason TEXT, created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS run_events_run_idx ON run_events(run_id, id);
`

const schemaV2 = `
CREATE UNIQUE INDEX IF NOT EXISTS experiments_control_session_idx
  ON experiments(control_session_id) WHERE control_session_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS control_commands(
  id TEXT PRIMARY KEY,
  experiment_id TEXT NOT NULL REFERENCES experiments(id),
  control_session_id TEXT NOT NULL,
  idempotency_key TEXT NOT NULL,
  type TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  result_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(experiment_id, idempotency_key)
);
CREATE INDEX IF NOT EXISTS control_commands_experiment_idx
  ON control_commands(experiment_id, created_at);

CREATE TABLE IF NOT EXISTS control_events(
  id INTEGER PRIMARY KEY,
  experiment_id TEXT NOT NULL REFERENCES experiments(id),
  control_session_id TEXT NOT NULL,
  command_id TEXT NOT NULL REFERENCES control_commands(id),
  type TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS control_events_experiment_idx
  ON control_events(experiment_id, id);
`

const schemaV3 = `
CREATE TABLE IF NOT EXISTS run_item_executions(
  item_id TEXT PRIMARY KEY REFERENCES run_items(id) ON DELETE CASCADE,
  execution_id TEXT NOT NULL,
  judge_execution_id TEXT NOT NULL,
  cost_known INTEGER NOT NULL,
  usage_json TEXT NOT NULL,
  artifacts_json TEXT NOT NULL
);
`

const schemaV5 = `
CREATE TABLE IF NOT EXISTS run_trials(
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  item_id TEXT NOT NULL REFERENCES run_items(id) ON DELETE CASCADE,
  case_id TEXT NOT NULL,
  trial_index INTEGER NOT NULL,
  ordinal INTEGER NOT NULL,
  status TEXT NOT NULL,
  result_key TEXT,
  result_json TEXT,
  created_at TEXT NOT NULL,
  started_at TEXT,
  completed_at TEXT,
  UNIQUE(run_id,case_id,trial_index)
);
CREATE INDEX IF NOT EXISTS run_trials_run_idx ON run_trials(run_id,ordinal);
CREATE INDEX IF NOT EXISTS run_trials_status_idx ON run_trials(run_id,status,ordinal);
`

const schemaV6 = `
CREATE TABLE IF NOT EXISTS run_groups(
  id TEXT PRIMARY KEY,
  experiment_id TEXT NOT NULL REFERENCES experiments(id),
  idempotency_key TEXT NOT NULL,
  dataset_version_id TEXT NOT NULL,
  agent_version_ids_json TEXT NOT NULL,
  trial_count INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(experiment_id,idempotency_key)
);
CREATE INDEX IF NOT EXISTS run_groups_experiment_idx ON run_groups(experiment_id,created_at);
CREATE INDEX IF NOT EXISTS runs_group_idx ON runs(group_id,created_at);
`

const schemaV9 = `
CREATE TABLE IF NOT EXISTS deployments(
  id INTEGER PRIMARY KEY,
  public_id TEXT NOT NULL UNIQUE,
  tenant_id TEXT NOT NULL,
  agent_family_id TEXT NOT NULL REFERENCES agent_families(id),
  name TEXT NOT NULL,
  policy_digest TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(tenant_id,name)
);
CREATE INDEX IF NOT EXISTS deployments_family_idx
  ON deployments(agent_family_id,id);

CREATE TABLE IF NOT EXISTS deployment_aliases(
  id INTEGER PRIMARY KEY,
  public_id TEXT NOT NULL UNIQUE,
  deployment_id INTEGER NOT NULL REFERENCES deployments(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  current_release_revision_id INTEGER REFERENCES release_revisions(id),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(deployment_id,name)
);

CREATE TABLE IF NOT EXISTS release_revisions(
  id INTEGER PRIMARY KEY,
  public_id TEXT NOT NULL UNIQUE,
  deployment_alias_id INTEGER NOT NULL REFERENCES deployment_aliases(id) ON DELETE CASCADE,
  revision INTEGER NOT NULL,
  agent_version_id TEXT NOT NULL REFERENCES agent_versions(id),
  previous_release_revision_id INTEGER REFERENCES release_revisions(id),
  evidence_run_id TEXT REFERENCES runs(id),
  policy_result TEXT NOT NULL,
  actor TEXT NOT NULL,
  reason TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(deployment_alias_id,revision)
);
CREATE INDEX IF NOT EXISTS release_revisions_alias_idx
  ON release_revisions(deployment_alias_id,revision DESC);
CREATE INDEX IF NOT EXISTS release_revisions_version_idx
  ON release_revisions(agent_version_id,id);
`

const schemaV10 = `
CREATE TABLE IF NOT EXISTS api_credentials(
  id INTEGER PRIMARY KEY,
  public_id TEXT NOT NULL UNIQUE,
  tenant_id TEXT NOT NULL,
  name TEXT NOT NULL,
  key_prefix TEXT NOT NULL,
  secret_hash TEXT NOT NULL UNIQUE,
  scopes_json TEXT NOT NULL,
  deployment_ids_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  revoked_at TEXT
);
CREATE INDEX IF NOT EXISTS api_credentials_tenant_idx
  ON api_credentials(tenant_id,id);

CREATE TABLE IF NOT EXISTS responses(
  id INTEGER PRIMARY KEY,
  public_id TEXT NOT NULL UNIQUE,
  tenant_id TEXT NOT NULL,
  credential_id INTEGER NOT NULL REFERENCES api_credentials(id),
  deployment_id INTEGER NOT NULL REFERENCES deployments(id),
  deployment_name TEXT NOT NULL,
  requested_reference TEXT NOT NULL,
  resolution_kind TEXT NOT NULL,
  release_revision_id INTEGER REFERENCES release_revisions(id),
  agent_version_id TEXT NOT NULL REFERENCES agent_versions(id),
  idempotency_key TEXT NOT NULL,
  request_hash TEXT NOT NULL,
  input_text TEXT NOT NULL,
  instructions TEXT NOT NULL,
  metadata_json TEXT NOT NULL,
  background INTEGER NOT NULL,
  status TEXT NOT NULL,
  execution_id TEXT,
  output_item_id TEXT NOT NULL,
  output_text TEXT NOT NULL,
  usage_json TEXT NOT NULL,
  cost REAL NOT NULL,
  cost_known INTEGER NOT NULL,
  error_code TEXT NOT NULL,
  error_message TEXT NOT NULL,
  created_at TEXT NOT NULL,
  started_at TEXT,
  completed_at TEXT,
  UNIQUE(credential_id,idempotency_key)
);
CREATE INDEX IF NOT EXISTS responses_tenant_idx
  ON responses(tenant_id,id);
CREATE INDEX IF NOT EXISTS responses_deployment_status_idx
  ON responses(deployment_id,status,id);

CREATE TABLE IF NOT EXISTS response_events(
  id INTEGER PRIMARY KEY,
  response_id INTEGER NOT NULL REFERENCES responses(id) ON DELETE CASCADE,
  sequence INTEGER NOT NULL,
  type TEXT NOT NULL,
  data_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(response_id,sequence)
);
CREATE INDEX IF NOT EXISTS response_events_response_idx
  ON response_events(response_id,sequence);
`

const schemaV11 = `
CREATE INDEX IF NOT EXISTS responses_admission_idx
  ON responses(credential_id,deployment_id,created_at);
CREATE INDEX IF NOT EXISTS responses_active_admission_idx
  ON responses(credential_id,deployment_id,status,id);
`

const schemaV12 = `
CREATE TABLE IF NOT EXISTS conversations(
  id INTEGER PRIMARY KEY,
  public_id TEXT NOT NULL UNIQUE,
  tenant_id TEXT NOT NULL,
  deployment_id INTEGER NOT NULL REFERENCES deployments(id),
  agent_version_id TEXT NOT NULL REFERENCES agent_versions(id),
  release_revision_id INTEGER REFERENCES release_revisions(id),
  last_response_id INTEGER REFERENCES responses(id),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS conversations_tenant_updated_idx
  ON conversations(tenant_id,updated_at DESC,id DESC);
`

const schemaV12ResponseIndexes = `
CREATE INDEX IF NOT EXISTS responses_conversation_idx
  ON responses(conversation_id,id);
CREATE UNIQUE INDEX IF NOT EXISTS responses_conversation_active_idx
  ON responses(conversation_id)
  WHERE conversation_id IS NOT NULL AND status IN ('queued','in_progress');
`

const schemaV16 = `
CREATE TABLE IF NOT EXISTS webhook_endpoints(
  id INTEGER PRIMARY KEY,
  public_id TEXT NOT NULL UNIQUE,
  tenant_id TEXT NOT NULL,
  name TEXT NOT NULL,
  url TEXT NOT NULL,
  secret_ref TEXT NOT NULL UNIQUE,
  events_json TEXT NOT NULL,
  deployment_ids_json TEXT NOT NULL,
  enabled INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(tenant_id,name)
);
CREATE INDEX IF NOT EXISTS webhook_endpoints_tenant_idx
  ON webhook_endpoints(tenant_id,id);

CREATE TABLE IF NOT EXISTS webhook_deliveries(
  id INTEGER PRIMARY KEY,
  public_id TEXT NOT NULL UNIQUE,
  endpoint_id INTEGER NOT NULL REFERENCES webhook_endpoints(id),
  response_id INTEGER NOT NULL REFERENCES responses(id) ON DELETE CASCADE,
  event_type TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  status TEXT NOT NULL,
  attempts INTEGER NOT NULL DEFAULT 0,
  next_attempt_at TEXT NOT NULL,
  lease_until TEXT,
  lease_token TEXT NOT NULL DEFAULT '',
  last_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  delivered_at TEXT,
  UNIQUE(endpoint_id,response_id,event_type)
);
CREATE INDEX IF NOT EXISTS webhook_deliveries_due_idx
  ON webhook_deliveries(status,next_attempt_at,lease_until,id);
CREATE INDEX IF NOT EXISTS webhook_deliveries_endpoint_idx
  ON webhook_deliveries(endpoint_id,id DESC);
`

// MigrateSQLite updates the development schema while retaining stored history.
func (s *Store) MigrateSQLite(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, schemaV1); err != nil {
		return fmt.Errorf("apply sqlite schema v1: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version,applied_at) VALUES(1,strftime('%Y-%m-%dT%H:%M:%fZ','now'))`); err != nil {
		return fmt.Errorf("record sqlite schema v1: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, schemaV2); err != nil {
		return fmt.Errorf("apply sqlite schema v2: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version,applied_at) VALUES(2,strftime('%Y-%m-%dT%H:%M:%fZ','now'))`); err != nil {
		return fmt.Errorf("record sqlite schema v2: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, schemaV3); err != nil {
		return fmt.Errorf("apply sqlite schema v3: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version,applied_at) VALUES(3,strftime('%Y-%m-%dT%H:%M:%fZ','now'))`); err != nil {
		return fmt.Errorf("record sqlite schema v3: %w", err)
	}
	for _, column := range []struct{ name, declaration string }{
		{"preset", "TEXT NOT NULL DEFAULT ''"},
		{"system_prompt", "TEXT NOT NULL DEFAULT ''"},
		{"skills_json", "TEXT NOT NULL DEFAULT '[]'"},
	} {
		if err := s.ensureColumn(ctx, "agent_versions", column.name, column.declaration); err != nil {
			return fmt.Errorf("apply sqlite schema v4: %w", err)
		}
	}
	if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version,applied_at) VALUES(4,strftime('%Y-%m-%dT%H:%M:%fZ','now'))`); err != nil {
		return fmt.Errorf("record sqlite schema v4: %w", err)
	}
	for _, column := range []struct{ table, name, declaration string }{
		{"dataset_versions", "evaluator_json", "TEXT NOT NULL DEFAULT '{}'"},
		{"runs", "evaluator_snapshot_json", "TEXT NOT NULL DEFAULT '{}'"},
		{"runs", "trial_count", "INTEGER NOT NULL DEFAULT 1"},
		{"runs", "scheduled_trials", "INTEGER NOT NULL DEFAULT 0"},
		{"runs", "valid_trials", "INTEGER NOT NULL DEFAULT 0"},
		{"runs", "infra_failures", "INTEGER NOT NULL DEFAULT 0"},
		{"runs", "grading_failures", "INTEGER NOT NULL DEFAULT 0"},
		{"runs", "reliable_cases", "INTEGER NOT NULL DEFAULT 0"},
		{"runs", "case_count", "INTEGER NOT NULL DEFAULT 0"},
		{"runs", "pass_hat_3", "REAL NOT NULL DEFAULT 0"},
		{"runs", "evaluation_complete", "INTEGER NOT NULL DEFAULT 0"},
		{"runs", "candidate_cost", "REAL NOT NULL DEFAULT 0"},
		{"runs", "evaluation_cost", "REAL NOT NULL DEFAULT 0"},
		{"run_items", "result_json", "TEXT"},
		{"run_events", "trial_id", "TEXT"},
		{"run_events", "trial_index", "INTEGER NOT NULL DEFAULT 0"},
	} {
		if err := s.ensureColumn(ctx, column.table, column.name, column.declaration); err != nil {
			return fmt.Errorf("apply sqlite schema v5: %w", err)
		}
	}
	if _, err := s.db.ExecContext(ctx, schemaV5); err != nil {
		return fmt.Errorf("apply sqlite schema v5: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version,applied_at) VALUES(5,strftime('%Y-%m-%dT%H:%M:%fZ','now'))`); err != nil {
		return fmt.Errorf("record sqlite schema v5: %w", err)
	}
	if err := s.ensureColumn(ctx, "runs", "group_id", "TEXT"); err != nil {
		return fmt.Errorf("apply sqlite schema v6: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, schemaV6); err != nil {
		return fmt.Errorf("apply sqlite schema v6: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version,applied_at) VALUES(6,strftime('%Y-%m-%dT%H:%M:%fZ','now'))`); err != nil {
		return fmt.Errorf("record sqlite schema v6: %w", err)
	}
	if err := s.ensureColumn(ctx, "agent_versions", "model_connection_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return fmt.Errorf("apply sqlite schema v7: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version,applied_at) VALUES(7,strftime('%Y-%m-%dT%H:%M:%fZ','now'))`); err != nil {
		return fmt.Errorf("record sqlite schema v7: %w", err)
	}
	if err := s.ensureColumn(ctx, "runs", "cost_estimated", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return fmt.Errorf("apply sqlite schema v8: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version,applied_at) VALUES(8,strftime('%Y-%m-%dT%H:%M:%fZ','now'))`); err != nil {
		return fmt.Errorf("record sqlite schema v8: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, schemaV9); err != nil {
		return fmt.Errorf("apply sqlite schema v9: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version,applied_at) VALUES(9,strftime('%Y-%m-%dT%H:%M:%fZ','now'))`); err != nil {
		return fmt.Errorf("record sqlite schema v9: %w", err)
	}
	if err := s.ensureColumn(ctx, "deployments", "policy_json", "TEXT NOT NULL DEFAULT '{}'"); err != nil {
		return fmt.Errorf("apply sqlite schema v10: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, schemaV10); err != nil {
		return fmt.Errorf("apply sqlite schema v10: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version,applied_at) VALUES(10,strftime('%Y-%m-%dT%H:%M:%fZ','now'))`); err != nil {
		return fmt.Errorf("record sqlite schema v10: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, schemaV11); err != nil {
		return fmt.Errorf("apply sqlite schema v11: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version,applied_at) VALUES(11,strftime('%Y-%m-%dT%H:%M:%fZ','now'))`); err != nil {
		return fmt.Errorf("record sqlite schema v11: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, schemaV12); err != nil {
		return fmt.Errorf("apply sqlite schema v12: %w", err)
	}
	for _, column := range []struct{ name, declaration string }{
		{"conversation_id", "INTEGER REFERENCES conversations(id)"},
		{"previous_response_id", "INTEGER REFERENCES responses(id)"},
	} {
		if err := s.ensureColumn(ctx, "responses", column.name, column.declaration); err != nil {
			return fmt.Errorf("apply sqlite schema v12: %w", err)
		}
	}
	if _, err := s.db.ExecContext(ctx, schemaV12ResponseIndexes); err != nil {
		return fmt.Errorf("apply sqlite schema v12 indexes: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version,applied_at) VALUES(12,strftime('%Y-%m-%dT%H:%M:%fZ','now'))`); err != nil {
		return fmt.Errorf("record sqlite schema v12: %w", err)
	}
	if err := s.ensureColumn(ctx, "responses", "text_format_json", "TEXT NOT NULL DEFAULT '{\"type\":\"text\"}'"); err != nil {
		return fmt.Errorf("apply sqlite schema v13: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version,applied_at) VALUES(13,strftime('%Y-%m-%dT%H:%M:%fZ','now'))`); err != nil {
		return fmt.Errorf("record sqlite schema v13: %w", err)
	}
	if err := s.ensureColumn(ctx, "responses", "input_items_json", "TEXT NOT NULL DEFAULT '[]'"); err != nil {
		return fmt.Errorf("apply sqlite schema v14: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version,applied_at) VALUES(14,strftime('%Y-%m-%dT%H:%M:%fZ','now'))`); err != nil {
		return fmt.Errorf("record sqlite schema v14: %w", err)
	}
	for _, column := range []struct{ name, declaration string }{
		{"caller_tools_json", "TEXT NOT NULL DEFAULT '[]'"},
		{"tool_call_json", "TEXT NOT NULL DEFAULT 'null'"},
	} {
		if err := s.ensureColumn(ctx, "responses", column.name, column.declaration); err != nil {
			return fmt.Errorf("apply sqlite schema v15: %w", err)
		}
	}
	if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version,applied_at) VALUES(15,strftime('%Y-%m-%dT%H:%M:%fZ','now'))`); err != nil {
		return fmt.Errorf("record sqlite schema v15: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, schemaV16); err != nil {
		return fmt.Errorf("apply sqlite schema v16: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version,applied_at) VALUES(16,strftime('%Y-%m-%dT%H:%M:%fZ','now'))`); err != nil {
		return fmt.Errorf("record sqlite schema v16: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS responses_tenant_history_idx ON responses(tenant_id,id DESC);
		CREATE INDEX IF NOT EXISTS responses_tenant_status_history_idx ON responses(tenant_id,status,id DESC);
		CREATE INDEX IF NOT EXISTS responses_tenant_deployment_history_idx ON responses(tenant_id,deployment_id,id DESC);
		CREATE INDEX IF NOT EXISTS api_credentials_tenant_history_idx ON api_credentials(tenant_id,id DESC);
		CREATE INDEX IF NOT EXISTS runs_agent_version_history_idx ON runs(json_extract(agent_snapshot_json,'$.id'),created_at DESC,id DESC);
		INSERT OR IGNORE INTO schema_migrations(version,applied_at) VALUES(17,strftime('%Y-%m-%dT%H:%M:%fZ','now'));
	`); err != nil {
		return fmt.Errorf("apply sqlite schema v17: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS release_policies(
		  deployment_id INTEGER PRIMARY KEY REFERENCES deployments(id),
		  revision INTEGER NOT NULL, policy_json TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS release_decisions(
		  id INTEGER PRIMARY KEY, public_id TEXT NOT NULL UNIQUE,
		  deployment_id INTEGER NOT NULL REFERENCES deployments(id),
		  alias TEXT NOT NULL, decision_json TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS release_decisions_history_idx ON release_decisions(deployment_id,alias,id DESC);
		INSERT OR IGNORE INTO schema_migrations(version,applied_at) VALUES(18,strftime('%Y-%m-%dT%H:%M:%fZ','now'));
	`); err != nil {
		return fmt.Errorf("apply sqlite schema v18: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `
	 CREATE TABLE IF NOT EXISTS agent_evaluation_defaults(agent_family_id TEXT PRIMARY KEY REFERENCES agent_families(id),revision INTEGER NOT NULL,defaults_json TEXT NOT NULL);
	 CREATE TABLE IF NOT EXISTS release_tasks(id INTEGER PRIMARY KEY,public_id TEXT NOT NULL UNIQUE,deployment_id INTEGER NOT NULL REFERENCES deployments(id),request_key TEXT NOT NULL,request_hash TEXT NOT NULL,status TEXT NOT NULL,task_json TEXT NOT NULL,UNIQUE(deployment_id,request_key));
	 CREATE INDEX IF NOT EXISTS release_tasks_status ON release_tasks(status,id);
	 CREATE INDEX IF NOT EXISTS release_tasks_run ON release_tasks(json_extract(task_json,'$.runId'));
	 INSERT OR IGNORE INTO schema_migrations(version,applied_at) VALUES(19,strftime('%Y-%m-%dT%H:%M:%fZ','now'));
	`); err != nil {
		return fmt.Errorf("apply sqlite schema v19: %w", err)
	}
	if err := s.migrateTrials(ctx); err != nil {
		return err
	}
	for _, column := range []struct{ name, declaration string }{
		{"execution_request_json", "TEXT NOT NULL DEFAULT 'null'"},
		{"cancel_requested", "INTEGER NOT NULL DEFAULT 0"},
		{"execution_sequence", "BIGINT NOT NULL DEFAULT 0"},
	} {
		if err := s.ensureColumn(ctx, "responses", column.name, column.declaration); err != nil {
			return err
		}
	}
	if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version,applied_at) VALUES(21,strftime('%Y-%m-%dT%H:%M:%fZ','now'))`); err != nil {
		return err
	}
	for _, column := range []struct{ table, name, declaration string }{
		{"runs", "cancel_requested", "INTEGER NOT NULL DEFAULT 0"},
		{"runs", "execution_format", "INTEGER NOT NULL DEFAULT 0"},
		{"runs", "execution_settings_json", "TEXT NOT NULL DEFAULT '{}'"},
		{"run_events", "event_key", "TEXT"},
	} {
		if err := s.ensureColumn(ctx, column.table, column.name, column.declaration); err != nil {
			return err
		}
	}
	_, err := s.db.ExecContext(ctx, sessionSchema+evaluationInvocationSchema+`CREATE UNIQUE INDEX IF NOT EXISTS run_events_key ON run_events(run_id,event_key);
    INSERT OR IGNORE INTO schema_migrations(version,applied_at) VALUES(22,strftime('%Y-%m-%dT%H:%M:%fZ','now'));
 INSERT OR IGNORE INTO schema_migrations(version,applied_at) VALUES(23,strftime('%Y-%m-%dT%H:%M:%fZ','now'));`)
	return err
}

func (s *Store) ensureColumn(ctx context.Context, table, name, declaration string) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid int
		var columnName, columnType string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &columnName, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return err
		}
		if columnName == name {
			found = true
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = s.db.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+name+` `+declaration)
	return err
}
