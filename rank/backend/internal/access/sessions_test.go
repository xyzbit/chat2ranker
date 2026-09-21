package access

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSessionLifetimeReplacementCapacityAndRevocation(t *testing.T) {
	token := strings.Repeat("a", 64)
	digest := sha256.Sum256([]byte(token))
	path := filepath.Join(t.TempDir(), "access.json")
	if err := os.WriteFile(path, []byte(fmt.Sprintf(`{"version":1,"operators":[{"subject":"alice","tokenSha256":"%s"}]}`, hex.EncodeToString(digest[:]))), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewSessions(time.Hour, 1)
	if err != nil {
		t.Fatal(err)
	}
	id, _, err := s.Create(t.Context(), "alice", token, "")
	if err != nil {
		t.Fatal(err)
	}
	if p, err := s.Authenticate(t.Context(), file, id); err != nil || p.Subject != "alice" {
		t.Fatal(p, err)
	}
	if _, _, err := s.Create(t.Context(), "alice", token, ""); err == nil {
		t.Fatal("capacity not enforced")
	}
	replacement, _, err := s.Create(t.Context(), "alice", token, id)
	if err != nil || replacement == id {
		t.Fatal("session did not rotate", err)
	}
	if _, err := s.Authenticate(t.Context(), file, id); err != ErrUnauthorized {
		t.Fatal("replaced session remained valid")
	}
	key := sha256.Sum256([]byte(replacement))
	entry := s.entries[key]
	entry.expires = time.Now().Add(-time.Second)
	s.entries[key] = entry
	if _, err := s.Authenticate(t.Context(), file, replacement); err != ErrUnauthorized {
		t.Fatal("expired session remained valid")
	}
	id, _, err = s.Create(t.Context(), "alice", token, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version":1,"operators":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(t.Context(), file, id); err != ErrUnauthorized {
		t.Fatal("revoked credential remained valid")
	}
	if len(s.entries) != 0 {
		t.Fatal("revoked session retained")
	}
	for _, ttl := range []time.Duration{-1, 0, time.Millisecond} {
		if _, err := NewSessions(ttl, 1); err == nil {
			t.Fatal("invalid TTL accepted")
		}
	}
	if _, err := NewSessions(time.Hour, 0); err == nil {
		t.Fatal("invalid capacity accepted")
	}
}
