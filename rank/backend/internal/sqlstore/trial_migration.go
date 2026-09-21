package sqlstore

import (
	"context"
	"fmt"
	"strings"
)

// Trial requests use the operator identity rather than a serving API key.
// Rebuilding retains IDs so response events, releases, and webhooks keep their references.
func (s *Store) migrateTrials(ctx context.Context) error {
	var done int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version=20").Scan(&done); err != nil || done != 0 {
		return err
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		return err
	}
	defer conn.ExecContext(context.Background(), "PRAGMA foreign_keys=ON")
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, table := range []struct{ name, schema string }{
		{"conversations", trialConversationSchema},
		{"responses", trialResponseSchema},
	} {
		rows, err := tx.QueryContext(ctx, "SELECT sql FROM sqlite_master WHERE type='index' AND tbl_name=? AND sql IS NOT NULL", table.name)
		if err != nil {
			return err
		}
		var indexes []string
		for rows.Next() {
			var statement string
			if err := rows.Scan(&statement); err != nil {
				rows.Close()
				return err
			}
			indexes = append(indexes, statement)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		rows, err = tx.QueryContext(ctx, "PRAGMA table_info("+table.name+")")
		if err != nil {
			return err
		}
		var columns []string
		for rows.Next() {
			var id, required, primary int
			var name, kind string
			var fallback any
			if err := rows.Scan(&id, &name, &kind, &required, &fallback, &primary); err != nil {
				rows.Close()
				return err
			}
			columns = append(columns, name)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		if _, err := tx.ExecContext(ctx, table.schema); err != nil {
			return err
		}
		fields := strings.Join(columns, ",")
		if _, err := tx.ExecContext(ctx, "INSERT INTO "+table.name+"_trial_new("+fields+") SELECT "+fields+" FROM "+table.name); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DROP TABLE "+table.name+"; ALTER TABLE "+table.name+"_trial_new RENAME TO "+table.name); err != nil {
			return err
		}
		for _, statement := range indexes {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `
		CREATE UNIQUE INDEX responses_trial_idempotency ON responses(tenant_id,idempotency_key) WHERE credential_id IS NULL;
		INSERT INTO schema_migrations(version,applied_at) VALUES(20,strftime('%Y-%m-%dT%H:%M:%fZ','now'));
	`); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	invalid := rows.Next()
	checkErr := rows.Err()
	rows.Close()
	if checkErr != nil {
		return checkErr
	}
	if invalid {
		return fmt.Errorf("trial migration violates a foreign key")
	}
	return tx.Commit()
}

const trialConversationSchema = `CREATE TABLE conversations_trial_new(
 id INTEGER PRIMARY KEY, public_id TEXT NOT NULL UNIQUE, tenant_id TEXT NOT NULL,
 deployment_id INTEGER REFERENCES deployments(id), agent_version_id TEXT NOT NULL REFERENCES agent_versions(id),
 release_revision_id INTEGER REFERENCES release_revisions(id), last_response_id INTEGER REFERENCES responses(id),
 created_at TEXT NOT NULL, updated_at TEXT NOT NULL, trial INTEGER NOT NULL DEFAULT 0
)`

const trialResponseSchema = `CREATE TABLE responses_trial_new(
 id INTEGER PRIMARY KEY, public_id TEXT NOT NULL UNIQUE, tenant_id TEXT NOT NULL,
 credential_id INTEGER REFERENCES api_credentials(id), deployment_id INTEGER REFERENCES deployments(id),
 deployment_name TEXT NOT NULL, requested_reference TEXT NOT NULL, resolution_kind TEXT NOT NULL,
 release_revision_id INTEGER REFERENCES release_revisions(id), agent_version_id TEXT NOT NULL REFERENCES agent_versions(id),
 idempotency_key TEXT NOT NULL, request_hash TEXT NOT NULL, input_text TEXT NOT NULL, instructions TEXT NOT NULL,
 metadata_json TEXT NOT NULL, background INTEGER NOT NULL, status TEXT NOT NULL, execution_id TEXT,
 output_item_id TEXT NOT NULL, output_text TEXT NOT NULL, usage_json TEXT NOT NULL, cost REAL NOT NULL,
 cost_known INTEGER NOT NULL, error_code TEXT NOT NULL, error_message TEXT NOT NULL, created_at TEXT NOT NULL,
 started_at TEXT, completed_at TEXT, conversation_id INTEGER REFERENCES conversations(id),
 previous_response_id INTEGER REFERENCES responses(id), caller_tools_json TEXT NOT NULL DEFAULT '[]',
 tool_call_json TEXT NOT NULL DEFAULT 'null', input_items_json TEXT NOT NULL DEFAULT '[]',
 text_format_json TEXT NOT NULL DEFAULT '{"type":"text"}', UNIQUE(credential_id,idempotency_key)
)`
