package sqlite

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
  model TEXT NOT NULL, tools_json TEXT NOT NULL, created_at TEXT NOT NULL,
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
  id INTEGER PRIMARY KEY AUTOINCREMENT,
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

func (s *Store) Migrate(ctx context.Context) error {
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
	return nil
}
