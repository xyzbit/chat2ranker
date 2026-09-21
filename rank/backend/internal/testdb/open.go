// Package testdb runs application tests against SQLite or an explicitly
// configured PostgreSQL instance, retaining database identity across reopen.
package testdb

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/xyzbit/agentkiln/rank/backend/internal/sqlstore"
)

// Open uses path as the SQLite file or a unique PostgreSQL test-schema identity.
// RANK_TEST_DATABASE_URL opts into real PostgreSQL; cleanup only drops that schema.
func Open(t testing.TB, path string) (*sqlstore.Store, error) {
	t.Helper()
	url := os.Getenv("RANK_TEST_DATABASE_URL")
	if url == "" {
		return sqlstore.OpenSQLite(context.Background(), path)
	}
	schema := fmt.Sprintf("rank_test_%x", sha256.Sum256([]byte(path)))[:58]
	config := sqlstore.PostgresConfig{OperationTimeout: 5 * time.Second, URL: url, Schema: schema, MaxConnections: 8}
	store, err := sqlstore.OpenPostgres(context.Background(), config)
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() {
		store.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cleanup, err := sqlstore.OpenPostgres(ctx, config)
		if err != nil {
			t.Error(err)
			return
		}
		defer cleanup.Close()
		if _, err := cleanup.DB().ExecContext(ctx, "DROP SCHEMA IF EXISTS "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	if err := store.MigratePostgres(context.Background()); err != nil {
		return nil, err
	}
	if err := store.CheckPostgresSchema(context.Background()); err != nil {
		return nil, err
	}
	return store, nil
}
