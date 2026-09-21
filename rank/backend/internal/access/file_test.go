package access_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xyzbit/agentkiln/rank/backend/internal/access"
)

func TestFileRotationRevocationAndInvalidReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "access.json")
	oldToken, newToken := strings.Repeat("a", 64), strings.Repeat("b", 64)
	entry := func(token string) access.Entry {
		hash := sha256.Sum256([]byte(token))
		return access.Entry{Subject: "alice", TokenSHA256: hex.EncodeToString(hash[:])}
	}
	write := func(entries ...access.Entry) {
		body, err := json.Marshal(map[string]any{"version": 1, "operators": entries})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path+".next", body, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(path+".next", path); err != nil {
			t.Fatal(err)
		}
	}
	write(entry(oldToken))
	file, err := access.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	principal, err := file.Authenticate("alice", oldToken)
	if err != nil || principal.Subject != "alice" || principal.Role != "operator" {
		t.Fatalf("identity missing: %v %v", principal, err)
	}
	if _, err := file.Authenticate("bob", oldToken); !errors.Is(err, access.ErrUnauthorized) {
		t.Fatal("Basic username not checked")
	}
	write(entry(oldToken), entry(newToken))
	for _, token := range []string{oldToken, newToken} {
		if _, err := file.Authenticate("", token); err != nil {
			t.Fatal("rotation overlap rejected")
		}
	}
	write(entry(newToken))
	if _, err := file.Authenticate("", oldToken); !errors.Is(err, access.ErrUnauthorized) {
		t.Fatal("revocation cached")
	}
	expired := entry(newToken)
	past := time.Unix(1, 0)
	expired.ExpiresAt = &past
	write(expired)
	if _, err := file.Authenticate("", newToken); !errors.Is(err, access.ErrUnauthorized) {
		t.Fatal("expiry ignored")
	}
	write(entry(newToken))
	if err := os.WriteFile(path, []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Authenticate("", newToken); err == nil || errors.Is(err, access.ErrUnauthorized) {
		t.Fatal("invalid replacement did not fail closed as configuration error")
	}
	write()
	if _, err := file.Authenticate("", newToken); !errors.Is(err, access.ErrUnauthorized) {
		t.Fatal("empty operator list did not revoke all")
	}
}

func TestCredentialFileRejectsInvalidConfiguration(t *testing.T) {
	hash := strings.Repeat("a", 64)
	for _, body := range []string{
		`{"version":2,"operators":[]}`,
		`{"version":1,"operators":[],"secret":"plaintext"}`,
		`{"version":1,"operators":[]} {}`,
		`{"version":1,"operators":[{"subject":"alice","tokenSha256":"bad"}]}`,
		`{"version":1,"operators":[{"subject":"bad:name","tokenSha256":"` + hash + `"}]}`,
		`{"version":1,"operators":[{"subject":"alice","tokenSha256":"` + hash + `"},{"subject":"bob","tokenSha256":"` + hash + `"}]}`,
		`{"version":1,"operators":[]}` + strings.Repeat(" ", 65536),
	} {
		path := filepath.Join(t.TempDir(), "access.json")
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := access.Open(path); err == nil {
			t.Fatal("invalid credential configuration accepted")
		}
	}
}
