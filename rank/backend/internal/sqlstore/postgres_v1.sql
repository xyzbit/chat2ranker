-- Rank PostgreSQL schema v1. Foreign keys follow tables to allow circular references.

CREATE TABLE agent_evaluation_defaults(agent_family_id TEXT PRIMARY KEY,revision BIGINT NOT NULL,defaults_json TEXT NOT NULL);

CREATE TABLE agent_families(
  id TEXT PRIMARY KEY, name TEXT NOT NULL, handle TEXT NOT NULL UNIQUE,
  description TEXT NOT NULL, latest_version_id TEXT NOT NULL, created_at TEXT NOT NULL
);

CREATE TABLE agent_versions(
  id TEXT PRIMARY KEY, family_id TEXT NOT NULL,
  version BIGINT NOT NULL, runner_type TEXT NOT NULL, description TEXT NOT NULL,
  model TEXT NOT NULL, preset TEXT NOT NULL DEFAULT '', system_prompt TEXT NOT NULL DEFAULT '',
  tools_json TEXT NOT NULL, skills_json TEXT NOT NULL DEFAULT '[]', created_at TEXT NOT NULL, model_connection_id TEXT NOT NULL DEFAULT '',
  UNIQUE(family_id, version)
);

CREATE TABLE api_credentials(
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
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

CREATE TABLE control_commands(
  id TEXT PRIMARY KEY,
  experiment_id TEXT NOT NULL,
  control_session_id TEXT NOT NULL,
  idempotency_key TEXT NOT NULL,
  type TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  result_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(experiment_id, idempotency_key)
);

CREATE TABLE control_events(
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  experiment_id TEXT NOT NULL,
  control_session_id TEXT NOT NULL,
  command_id TEXT NOT NULL,
  type TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE "conversations"(
 id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY, public_id TEXT NOT NULL UNIQUE, tenant_id TEXT NOT NULL,
 deployment_id BIGINT, agent_version_id TEXT NOT NULL,
 release_revision_id BIGINT, last_response_id BIGINT,
 created_at TEXT NOT NULL, updated_at TEXT NOT NULL, trial BIGINT NOT NULL DEFAULT 0
);

CREATE TABLE dataset_families(
  id TEXT PRIMARY KEY, name TEXT NOT NULL, description TEXT NOT NULL,
  latest_version_id TEXT NOT NULL, created_at TEXT NOT NULL
);

CREATE TABLE dataset_versions(
  id TEXT PRIMARY KEY, family_id TEXT NOT NULL,
  version BIGINT NOT NULL, source TEXT NOT NULL, description TEXT NOT NULL,
  schema_json TEXT NOT NULL DEFAULT '{}', rubric_json TEXT NOT NULL DEFAULT '{}',
  cases_json TEXT NOT NULL, created_at TEXT NOT NULL, evaluator_json TEXT NOT NULL DEFAULT '{}',
  UNIQUE(family_id, version)
);

CREATE TABLE deployment_aliases(
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  public_id TEXT NOT NULL UNIQUE,
  deployment_id BIGINT NOT NULL,
  name TEXT NOT NULL,
  current_release_revision_id BIGINT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(deployment_id,name)
);

CREATE TABLE deployments(
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  public_id TEXT NOT NULL UNIQUE,
  tenant_id TEXT NOT NULL,
  agent_family_id TEXT NOT NULL,
  name TEXT NOT NULL,
  policy_digest TEXT NOT NULL,
  created_at TEXT NOT NULL, policy_json TEXT NOT NULL DEFAULT '{}',
  UNIQUE(tenant_id,name)
);

CREATE TABLE experiments(
  id TEXT PRIMARY KEY, title TEXT NOT NULL,
  dataset_version_id TEXT,
  agent_version_id TEXT,
  control_session_id TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);

CREATE TABLE messages(
  id TEXT PRIMARY KEY, experiment_id TEXT NOT NULL,
  role TEXT NOT NULL, content TEXT NOT NULL, run_id TEXT, created_at TEXT NOT NULL
);

CREATE TABLE release_decisions(
		  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY, public_id TEXT NOT NULL UNIQUE,
		  deployment_id BIGINT NOT NULL,
		  alias TEXT NOT NULL, decision_json TEXT NOT NULL
		);

CREATE TABLE release_policies(
		  deployment_id BIGINT PRIMARY KEY,
		  revision BIGINT NOT NULL, policy_json TEXT NOT NULL
		);

CREATE TABLE release_revisions(
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  public_id TEXT NOT NULL UNIQUE,
  deployment_alias_id BIGINT NOT NULL,
  revision BIGINT NOT NULL,
  agent_version_id TEXT NOT NULL,
  previous_release_revision_id BIGINT,
  evidence_run_id TEXT,
  policy_result TEXT NOT NULL,
  actor TEXT NOT NULL,
  reason TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(deployment_alias_id,revision)
);

CREATE TABLE release_tasks(id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,public_id TEXT NOT NULL UNIQUE,deployment_id BIGINT NOT NULL,request_key TEXT NOT NULL,request_hash TEXT NOT NULL,status TEXT NOT NULL,task_json JSONB NOT NULL,UNIQUE(deployment_id,request_key));

CREATE TABLE response_events(
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  response_id BIGINT NOT NULL,
  sequence BIGINT NOT NULL,
  type TEXT NOT NULL,
  data_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(response_id,sequence)
);

CREATE TABLE "responses"(
 id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY, public_id TEXT NOT NULL UNIQUE, tenant_id TEXT NOT NULL,
 credential_id BIGINT, deployment_id BIGINT,
 deployment_name TEXT NOT NULL, requested_reference TEXT NOT NULL, resolution_kind TEXT NOT NULL,
 release_revision_id BIGINT, agent_version_id TEXT NOT NULL,
 idempotency_key TEXT NOT NULL, request_hash TEXT NOT NULL, input_text TEXT NOT NULL, instructions TEXT NOT NULL,
 metadata_json TEXT NOT NULL, background BIGINT NOT NULL, status TEXT NOT NULL, execution_id TEXT,
 output_item_id TEXT NOT NULL, output_text TEXT NOT NULL, usage_json TEXT NOT NULL, cost DOUBLE PRECISION NOT NULL,
 cost_known BIGINT NOT NULL, error_code TEXT NOT NULL, error_message TEXT NOT NULL, created_at TEXT NOT NULL,
 started_at TEXT, completed_at TEXT, conversation_id BIGINT,
 previous_response_id BIGINT, caller_tools_json TEXT NOT NULL DEFAULT '[]',
 tool_call_json TEXT NOT NULL DEFAULT 'null', input_items_json TEXT NOT NULL DEFAULT '[]',
 text_format_json TEXT NOT NULL DEFAULT '{"type":"text"}', UNIQUE(credential_id,idempotency_key)
);

CREATE TABLE run_events(
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY, run_id TEXT NOT NULL,
  type TEXT NOT NULL, case_id TEXT, status TEXT, dataset_version_id TEXT,
  agent_version_id TEXT, passed BIGINT, cost DOUBLE PRECISION, score DOUBLE PRECISION,
  output TEXT, reason TEXT, created_at TEXT NOT NULL
, trial_id TEXT, trial_index BIGINT NOT NULL DEFAULT 0);

CREATE TABLE run_groups(
  id TEXT PRIMARY KEY,
  experiment_id TEXT NOT NULL,
  idempotency_key TEXT NOT NULL,
  dataset_version_id TEXT NOT NULL,
  agent_version_ids_json TEXT NOT NULL,
  trial_count BIGINT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(experiment_id,idempotency_key)
);

CREATE TABLE run_item_executions(
  item_id TEXT PRIMARY KEY,
  execution_id TEXT NOT NULL,
  judge_execution_id TEXT NOT NULL,
  cost_known BIGINT NOT NULL,
  usage_json TEXT NOT NULL,
  artifacts_json TEXT NOT NULL
);

CREATE TABLE run_items(
  id TEXT PRIMARY KEY, run_id TEXT NOT NULL,
  case_id TEXT NOT NULL, title TEXT NOT NULL, ordinal BIGINT NOT NULL,
  status TEXT NOT NULL, result_key TEXT, passed BIGINT, cost DOUBLE PRECISION, score DOUBLE PRECISION,
  output TEXT, reason TEXT, duration_ms BIGINT, created_at TEXT NOT NULL,
  started_at TEXT, completed_at TEXT, result_json TEXT, UNIQUE(run_id, case_id)
);

CREATE TABLE run_trials(
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL,
  item_id TEXT NOT NULL,
  case_id TEXT NOT NULL,
  trial_index BIGINT NOT NULL,
  ordinal BIGINT NOT NULL,
  status TEXT NOT NULL,
  result_key TEXT,
  result_json TEXT,
  created_at TEXT NOT NULL,
  started_at TEXT,
  completed_at TEXT,
  UNIQUE(run_id,case_id,trial_index)
);

CREATE TABLE runs(
  id TEXT PRIMARY KEY, experiment_id TEXT NOT NULL,
  idempotency_key TEXT NOT NULL, status TEXT NOT NULL,
  dataset_snapshot_json JSONB NOT NULL, agent_snapshot_json JSONB NOT NULL,
  concurrency BIGINT NOT NULL, created_at TEXT NOT NULL, started_at TEXT,
  completed_at TEXT, duration_ms BIGINT NOT NULL DEFAULT 0,
  passed BIGINT NOT NULL DEFAULT 0, total BIGINT NOT NULL,
  pass_rate BIGINT NOT NULL DEFAULT 0, cost DOUBLE PRECISION NOT NULL DEFAULT 0,
  cost_known BIGINT NOT NULL DEFAULT 1, error TEXT NOT NULL DEFAULT '', evaluator_snapshot_json TEXT NOT NULL DEFAULT '{}', trial_count BIGINT NOT NULL DEFAULT 1, scheduled_trials BIGINT NOT NULL DEFAULT 0, valid_trials BIGINT NOT NULL DEFAULT 0, infra_failures BIGINT NOT NULL DEFAULT 0, grading_failures BIGINT NOT NULL DEFAULT 0, reliable_cases BIGINT NOT NULL DEFAULT 0, case_count BIGINT NOT NULL DEFAULT 0, pass_hat_3 DOUBLE PRECISION NOT NULL DEFAULT 0, evaluation_complete BIGINT NOT NULL DEFAULT 0, candidate_cost DOUBLE PRECISION NOT NULL DEFAULT 0, evaluation_cost DOUBLE PRECISION NOT NULL DEFAULT 0, group_id TEXT, cost_estimated BIGINT NOT NULL DEFAULT 0,
  UNIQUE(experiment_id, idempotency_key)
);

CREATE TABLE webhook_deliveries(
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  public_id TEXT NOT NULL UNIQUE,
  endpoint_id BIGINT NOT NULL,
  response_id BIGINT NOT NULL,
  event_type TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  status TEXT NOT NULL,
  attempts BIGINT NOT NULL DEFAULT 0,
  next_attempt_at TEXT NOT NULL,
  lease_until TEXT,
  lease_token TEXT NOT NULL DEFAULT '',
  last_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  delivered_at TEXT,
  UNIQUE(endpoint_id,response_id,event_type)
);

CREATE TABLE webhook_endpoints(
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  public_id TEXT NOT NULL UNIQUE,
  tenant_id TEXT NOT NULL,
  name TEXT NOT NULL,
  url TEXT NOT NULL,
  secret_ref TEXT NOT NULL UNIQUE,
  events_json TEXT NOT NULL,
  deployment_ids_json TEXT NOT NULL,
  enabled BIGINT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(tenant_id,name)
);

ALTER TABLE agent_evaluation_defaults ADD FOREIGN KEY (agent_family_id) REFERENCES agent_families(id);

ALTER TABLE agent_versions ADD FOREIGN KEY (family_id) REFERENCES agent_families(id);

ALTER TABLE control_commands ADD FOREIGN KEY (experiment_id) REFERENCES experiments(id);

ALTER TABLE control_events ADD FOREIGN KEY (experiment_id) REFERENCES experiments(id);

ALTER TABLE control_events ADD FOREIGN KEY (command_id) REFERENCES control_commands(id);

ALTER TABLE conversations ADD FOREIGN KEY (deployment_id) REFERENCES deployments(id);

ALTER TABLE conversations ADD FOREIGN KEY (agent_version_id) REFERENCES agent_versions(id);

ALTER TABLE conversations ADD FOREIGN KEY (release_revision_id) REFERENCES release_revisions(id);

ALTER TABLE conversations ADD FOREIGN KEY (last_response_id) REFERENCES responses(id);

ALTER TABLE dataset_versions ADD FOREIGN KEY (family_id) REFERENCES dataset_families(id);

ALTER TABLE deployment_aliases ADD FOREIGN KEY (deployment_id) REFERENCES deployments(id) ON DELETE CASCADE;

ALTER TABLE deployment_aliases ADD FOREIGN KEY (current_release_revision_id) REFERENCES release_revisions(id);

ALTER TABLE deployments ADD FOREIGN KEY (agent_family_id) REFERENCES agent_families(id);

ALTER TABLE experiments ADD FOREIGN KEY (dataset_version_id) REFERENCES dataset_versions(id);

ALTER TABLE experiments ADD FOREIGN KEY (agent_version_id) REFERENCES agent_versions(id);

ALTER TABLE messages ADD FOREIGN KEY (experiment_id) REFERENCES experiments(id);

ALTER TABLE release_decisions ADD FOREIGN KEY (deployment_id) REFERENCES deployments(id);

ALTER TABLE release_policies ADD FOREIGN KEY (deployment_id) REFERENCES deployments(id);

ALTER TABLE release_revisions ADD FOREIGN KEY (deployment_alias_id) REFERENCES deployment_aliases(id) ON DELETE CASCADE;

ALTER TABLE release_revisions ADD FOREIGN KEY (agent_version_id) REFERENCES agent_versions(id);

ALTER TABLE release_revisions ADD FOREIGN KEY (previous_release_revision_id) REFERENCES release_revisions(id);

ALTER TABLE release_revisions ADD FOREIGN KEY (evidence_run_id) REFERENCES runs(id);

ALTER TABLE release_tasks ADD FOREIGN KEY (deployment_id) REFERENCES deployments(id);

ALTER TABLE response_events ADD FOREIGN KEY (response_id) REFERENCES responses(id) ON DELETE CASCADE;

ALTER TABLE responses ADD FOREIGN KEY (credential_id) REFERENCES api_credentials(id);

ALTER TABLE responses ADD FOREIGN KEY (deployment_id) REFERENCES deployments(id);

ALTER TABLE responses ADD FOREIGN KEY (release_revision_id) REFERENCES release_revisions(id);

ALTER TABLE responses ADD FOREIGN KEY (agent_version_id) REFERENCES agent_versions(id);

ALTER TABLE responses ADD FOREIGN KEY (conversation_id) REFERENCES conversations(id);

ALTER TABLE responses ADD FOREIGN KEY (previous_response_id) REFERENCES responses(id);

ALTER TABLE run_events ADD FOREIGN KEY (run_id) REFERENCES runs(id);

ALTER TABLE run_groups ADD FOREIGN KEY (experiment_id) REFERENCES experiments(id);

ALTER TABLE run_item_executions ADD FOREIGN KEY (item_id) REFERENCES run_items(id) ON DELETE CASCADE;

ALTER TABLE run_items ADD FOREIGN KEY (run_id) REFERENCES runs(id);

ALTER TABLE run_trials ADD FOREIGN KEY (run_id) REFERENCES runs(id) ON DELETE CASCADE;

ALTER TABLE run_trials ADD FOREIGN KEY (item_id) REFERENCES run_items(id) ON DELETE CASCADE;

ALTER TABLE runs ADD FOREIGN KEY (experiment_id) REFERENCES experiments(id);

ALTER TABLE webhook_deliveries ADD FOREIGN KEY (endpoint_id) REFERENCES webhook_endpoints(id);

ALTER TABLE webhook_deliveries ADD FOREIGN KEY (response_id) REFERENCES responses(id) ON DELETE CASCADE;

CREATE INDEX api_credentials_tenant_history_idx ON api_credentials(tenant_id,id DESC);

CREATE INDEX api_credentials_tenant_idx
  ON api_credentials(tenant_id,id);

CREATE INDEX control_commands_experiment_idx
  ON control_commands(experiment_id, created_at);

CREATE INDEX control_events_experiment_idx
  ON control_events(experiment_id, id);

CREATE INDEX conversations_tenant_updated_idx
  ON conversations(tenant_id,updated_at DESC,id DESC);

CREATE INDEX deployments_family_idx
  ON deployments(agent_family_id,id);

CREATE UNIQUE INDEX experiments_control_session_idx
  ON experiments(control_session_id) WHERE control_session_id IS NOT NULL;

CREATE INDEX messages_experiment_idx ON messages(experiment_id, created_at);

CREATE INDEX release_decisions_history_idx ON release_decisions(deployment_id,alias,id DESC);

CREATE INDEX release_revisions_alias_idx
  ON release_revisions(deployment_alias_id,revision DESC);

CREATE INDEX release_revisions_version_idx
  ON release_revisions(agent_version_id,id);

CREATE INDEX release_tasks_run ON release_tasks((task_json ->> 'runId'));

CREATE INDEX release_tasks_status ON release_tasks(status,id);

CREATE INDEX response_events_response_idx
  ON response_events(response_id,sequence);

CREATE INDEX responses_active_admission_idx
  ON responses(credential_id,deployment_id,status,id);

CREATE INDEX responses_admission_idx
  ON responses(credential_id,deployment_id,created_at);

CREATE UNIQUE INDEX responses_conversation_active_idx
  ON responses(conversation_id)
  WHERE conversation_id IS NOT NULL AND status IN ('queued','in_progress');

CREATE INDEX responses_conversation_idx
  ON responses(conversation_id,id);

CREATE INDEX responses_deployment_status_idx
  ON responses(deployment_id,status,id);

CREATE INDEX responses_tenant_deployment_history_idx ON responses(tenant_id,deployment_id,id DESC);

CREATE INDEX responses_tenant_history_idx ON responses(tenant_id,id DESC);

CREATE INDEX responses_tenant_idx
  ON responses(tenant_id,id);

CREATE INDEX responses_tenant_status_history_idx ON responses(tenant_id,status,id DESC);

CREATE UNIQUE INDEX responses_trial_idempotency ON responses(tenant_id,idempotency_key) WHERE credential_id IS NULL;

CREATE INDEX run_events_run_idx ON run_events(run_id, id);

CREATE INDEX run_groups_experiment_idx ON run_groups(experiment_id,created_at);

CREATE INDEX run_items_run_idx ON run_items(run_id, ordinal);

CREATE INDEX run_trials_run_idx ON run_trials(run_id,ordinal);

CREATE INDEX run_trials_status_idx ON run_trials(run_id,status,ordinal);

CREATE INDEX runs_agent_version_history_idx ON runs((agent_snapshot_json ->> 'id'),created_at DESC,id DESC);

CREATE INDEX runs_experiment_idx ON runs(experiment_id, created_at);

CREATE INDEX runs_group_idx ON runs(group_id,created_at);

CREATE INDEX runs_status_idx ON runs(status, created_at);

CREATE INDEX webhook_deliveries_due_idx
  ON webhook_deliveries(status,next_attempt_at,lease_until,id);

CREATE INDEX webhook_deliveries_endpoint_idx
  ON webhook_deliveries(endpoint_id,id DESC);

CREATE INDEX webhook_endpoints_tenant_idx
  ON webhook_endpoints(tenant_id,id);
