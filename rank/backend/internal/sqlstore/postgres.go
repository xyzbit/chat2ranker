package sqlstore

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/xyzbit/agentkiln/execution/backend/pgtimeout"
)

//go:embed postgres_v1.sql
var postgresV1 string

const postgresVersion = 4

var schemaName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

// PostgresConfig selects an owned schema and bounds the Rank connection pool.
type PostgresConfig struct {
	OperationTimeout time.Duration
	URL              string
	Schema           string
	MaxConnections   int
}

// OpenPostgres opens a Rank database without changing its schema. Deployment
// runs MigratePostgres explicitly before serving traffic with CheckPostgresSchema.
func OpenPostgres(ctx context.Context, config PostgresConfig) (*Store, error) {
	if !schemaName.MatchString(config.Schema) || strings.HasPrefix(config.Schema, "pg_") || config.Schema == "public" || config.Schema == "information_schema" || config.OperationTimeout <= 0 || config.MaxConnections < 1 || config.MaxConnections > 2147483647 || config.URL == "" {
		return nil, errors.New("Rank PostgreSQL requires a private schema, connection URL and positive operation timeout and pool size")
	}
	connection, err := pgxpool.ParseConfig(config.URL)
	if err != nil {
		return nil, errors.New("invalid Rank PostgreSQL connection settings")
	}
	connection.ConnConfig.RuntimeParams["search_path"] = pgx.Identifier{config.Schema}.Sanitize() + ",pg_catalog"
	connection.MaxConns = int32(config.MaxConnections)
	if err := pgtimeout.Configure(connection, config.OperationTimeout); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, config.OperationTimeout)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, connection)
	if err != nil {
		return nil, err
	}
	// pgx owns both the connection limit and acquisition deadline; a second
	// finite database/sql pool would add a wait outside the pgx deadline.
	db := stdlib.OpenDBFromPool(pool)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		pool.Close()
		return nil, fmt.Errorf("connect Rank PostgreSQL: %w", err)
	}
	return &Store{db: db, q: db, postgresPool: pool, postgresSchema: config.Schema, operationTimeout: config.OperationTimeout}, nil
}

// MigratePostgres atomically initializes a fresh Rank schema under a database
// migration lock. Repeated calls preserve data; unknown versions are rejected.
func (s *Store) MigratePostgres(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, s.operationTimeout)
	defer cancel()
	if s.postgresSchema == "" {
		return errors.New("PostgreSQL migration requires a PostgreSQL store")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, s.postgresSchema+"/rank-migrations"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "CREATE SCHEMA IF NOT EXISTS "+pgx.Identifier{s.postgresSchema}.Sanitize()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations(version BIGINT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return err
	}
	var version int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&version); err != nil {
		return err
	}
	if version < 0 || version > postgresVersion {
		return fmt.Errorf("unsupported Rank PostgreSQL schema version %d", version)
	}
	if version == 0 {
		if _, err := tx.ExecContext(ctx, postgresV1); err != nil {
			return fmt.Errorf("migrate Rank PostgreSQL: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version,applied_at) VALUES($1,$2)`, 1, stamp(time.Now())); err != nil {
			return err
		}
	}
	if version < 2 {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE responses ADD COLUMN execution_request_json TEXT NOT NULL DEFAULT 'null';
        ALTER TABLE responses ADD COLUMN cancel_requested INTEGER NOT NULL DEFAULT 0;
        ALTER TABLE responses ADD COLUMN execution_sequence BIGINT NOT NULL DEFAULT 0;`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version,applied_at) VALUES($1,$2)`, 2, stamp(time.Now())); err != nil {
			return err
		}
	}
	if version < 3 {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE runs ADD COLUMN cancel_requested INTEGER NOT NULL DEFAULT 0;
        ALTER TABLE runs ADD COLUMN execution_format INTEGER NOT NULL DEFAULT 0;
 ALTER TABLE runs ADD COLUMN execution_settings_json TEXT NOT NULL DEFAULT '{}';
        ALTER TABLE run_events ADD COLUMN event_key TEXT;
        CREATE UNIQUE INDEX run_events_key ON run_events(run_id,event_key);`+evaluationInvocationSchema); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version,applied_at) VALUES(3,$1)`, stamp(time.Now())); err != nil {
			return err
		}
	}
	if version < 4 {
		if _, err := tx.ExecContext(ctx, sessionSchema); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version,applied_at) VALUES(4,$1)`, stamp(time.Now())); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// CheckPostgresSchema refuses traffic before explicit migration or with an
// incompatible database; it never changes persistent state.
func (s *Store) CheckPostgresSchema(ctx context.Context) error {
	if s.postgresSchema == "" {
		return errors.New("PostgreSQL schema check requires a PostgreSQL store")
	}
	var version int
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&version); err != nil {
		return fmt.Errorf("Rank PostgreSQL schema unavailable; run rankd -migrate: %w", err)
	}
	if version != postgresVersion {
		return fmt.Errorf("unsupported Rank PostgreSQL schema version %d; expected %d", version, postgresVersion)
	}
	return nil
}
