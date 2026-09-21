package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/xyzbit/agentkiln/rank/backend/internal/access"
	"github.com/xyzbit/agentkiln/rank/backend/internal/domain"
)

const sessionSchema = `CREATE TABLE IF NOT EXISTS management_sessions(
 id_hash TEXT PRIMARY KEY, username TEXT NOT NULL, credential_digest TEXT NOT NULL,
 expires_at BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS management_sessions_expiry ON management_sessions(expires_at);`

// CreateSession rotates a cookie and enforces installation-wide capacity in one transaction.
func (s *Store) CreateSession(ctx context.Context, value access.StoredSession, previous string, capacity int) error {
	return s.WithinTx(ctx, func(ctx context.Context, repo domain.Repository) error {
		tx := repo.(*Store)
		if _, err := tx.q.ExecContext(ctx, `DELETE FROM management_sessions WHERE expires_at <= $1 OR id_hash = $2`, time.Now().UnixNano(), previous); err != nil {
			return err
		}
		var count int
		if err := tx.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM management_sessions`).Scan(&count); err != nil {
			return err
		}
		if count >= capacity {
			return errors.New("browser session capacity reached")
		}
		_, err := tx.q.ExecContext(ctx, `INSERT INTO management_sessions(id_hash,username,credential_digest,expires_at) VALUES($1,$2,$3,$4)`, value.Hash, value.Username, value.CredentialDigest, value.ExpiresAt.UnixNano())
		return err
	})
}

// GetSession never returns expired cookies; database failures remain distinguishable from invalid credentials.
func (s *Store) GetSession(ctx context.Context, hash string) (access.StoredSession, error) {
	value := access.StoredSession{Hash: hash}
	var expires int64
	err := s.q.QueryRowContext(ctx, `SELECT username,credential_digest,expires_at FROM management_sessions WHERE id_hash=$1`, hash).Scan(&value.Username, &value.CredentialDigest, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return value, access.ErrUnauthorized
	}
	if err != nil {
		return value, err
	}
	value.ExpiresAt = time.Unix(0, expires)
	if !time.Now().Before(value.ExpiresAt) {
		return value, access.ErrUnauthorized
	}
	return value, nil
}

// DeleteSession revokes the cookie across API processes; unknown cookies are already revoked.
func (s *Store) DeleteSession(ctx context.Context, hash string) error {
	_, err := s.q.ExecContext(ctx, `DELETE FROM management_sessions WHERE id_hash=$1`, hash)
	return err
}
