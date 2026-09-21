package sqlstore_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xyzbit/agentkiln/rank/backend/internal/access"
	"github.com/xyzbit/agentkiln/rank/backend/internal/sqlstore"
)

func TestSharedSessionsSurviveReopenAndRevokeAcrossProcesses(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			var first, second *sqlstore.Store
			if driver == "postgres" {
				first, second = postgresPair(t, true)
			} else {
				var path string
				first, path = openStore(t)
				var err error
				second, err = sqlstore.OpenSQLite(t.Context(), path)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { second.Close() })
			}
			token := strings.Repeat("s", 64)
			digest := sha256.Sum256([]byte(token))
			hash := hex.EncodeToString(digest[:])
			path := filepath.Join(t.TempDir(), "access.json")
			contents := fmt.Sprintf(`{"version":1,"operators":[{"subject":"alice","tokenSha256":"%s"}]}`, hash)
			if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
				t.Fatal(err)
			}
			file, err := access.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			a, err := access.NewStoredSessions(time.Hour, 1, first)
			if err != nil {
				t.Fatal(err)
			}
			b, err := access.NewStoredSessions(time.Hour, 1, second)
			if err != nil {
				t.Fatal(err)
			}
			id, _, err := a.Create(t.Context(), "alice", token, "")
			if err != nil {
				t.Fatal(err)
			}
			if principal, err := b.Authenticate(t.Context(), file, id); err != nil || principal.Subject != "alice" {
				t.Fatal(principal, err)
			}
			var storedHash, storedDigest string
			if err := second.DB().QueryRow(`SELECT id_hash,credential_digest FROM management_sessions`).Scan(&storedHash, &storedDigest); err != nil {
				t.Fatal(err)
			}
			if storedHash == id || storedDigest == token || storedDigest != hash {
				t.Fatal("session store contains bearer secrets")
			}
			if _, _, err := b.Create(t.Context(), "alice", token, ""); err == nil {
				t.Fatal("shared capacity exceeded")
			}
			replacement, _, err := b.Create(t.Context(), "alice", token, id)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := a.Authenticate(t.Context(), file, id); !errors.Is(err, access.ErrUnauthorized) {
				t.Fatal(err)
			}
			if err := a.Revoke(t.Context(), replacement); err != nil {
				t.Fatal(err)
			}
			if _, err := b.Authenticate(t.Context(), file, replacement); !errors.Is(err, access.ErrUnauthorized) {
				t.Fatal(err)
			}
			id, _, err = a.Create(t.Context(), "alice", token, "")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(`{"version":1,"operators":[]}`), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := b.Authenticate(t.Context(), file, id); !errors.Is(err, access.ErrUnauthorized) {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
				t.Fatal(err)
			}
			id, _, err = a.Create(t.Context(), "alice", token, "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := second.DB().Exec(`UPDATE management_sessions SET expires_at=0`); err != nil {
				t.Fatal(err)
			}
			if _, err := b.Authenticate(t.Context(), file, id); !errors.Is(err, access.ErrUnauthorized) {
				t.Fatal(err)
			}
			if driver == "postgres" {
				var wg sync.WaitGroup
				results := make(chan error, 8)
				for i := 0; i < 8; i++ {
					wg.Add(1)
					go func() { defer wg.Done(); _, _, err := b.Create(t.Context(), "alice", token, ""); results <- err }()
				}
				wg.Wait()
				close(results)
				successes := 0
				for err := range results {
					if err == nil {
						successes++
					}
				}
				if successes != 1 {
					t.Fatalf("concurrent shared capacity admitted %d", successes)
				}
			}
		})
	}
}
