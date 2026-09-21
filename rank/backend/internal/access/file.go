// Package access authenticates installation operators from an atomically replaced
// credential file. Public serving credentials retain their database owner.
package access

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"time"
)

var ErrUnauthorized = errors.New("invalid management credential")

// Principal identifies an installation operator, not a serving tenant.
type Principal struct {
	Subject string `json:"subject"`
	Role    string `json:"role"`
}

// Entry stores only a digest of a randomly generated access token. Several
// active tokens may share a subject during rotation.
type Entry struct {
	Subject     string     `json:"subject"`
	TokenSHA256 string     `json:"tokenSha256"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
}

// File rereads credentials on each authorization; invalid replacement files fail
// closed instead of retaining revoked entries in a process cache.
type File struct{ path string }

func Open(path string) (*File, error) {
	f := &File{path: path}
	_, err := f.entries()
	return f, err
}

func (f *File) entries() ([]Entry, error) {
	file, err := os.Open(f.path)
	if err != nil {
		return nil, errors.New("management credential file unavailable")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 65536 {
		return nil, errors.New("invalid management credential file")
	}
	var config struct {
		Version   int     `json:"version"`
		Operators []Entry `json:"operators"`
	}
	decoder := json.NewDecoder(io.LimitReader(file, 65537))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil || decoder.InputOffset() > 65536 || config.Version != 1 {
		return nil, errors.New("invalid management credential file")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, errors.New("invalid management credential file")
	}
	seen := map[string]bool{}
	for _, entry := range config.Operators {
		validSubject := entry.Subject != "" && len(entry.Subject) <= 128
		for _, c := range entry.Subject {
			validSubject = validSubject && (c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("@._-", c))
		}
		hash, err := hex.DecodeString(entry.TokenSHA256)
		if !validSubject || err != nil || len(hash) != sha256.Size || entry.TokenSHA256 != strings.ToLower(entry.TokenSHA256) || seen[entry.TokenSHA256] {
			return nil, errors.New("invalid management credential entry")
		}
		seen[entry.TokenSHA256] = true
	}
	return config.Operators, nil
}

// Authenticate checks the current file and expiry. A nonempty Basic username
// must match the token subject; Bearer authentication omits the username.
func (f *File) Authenticate(username, token string) (Principal, error) {
	if len(token) < 32 || len(token) > 512 {
		return Principal{}, ErrUnauthorized
	}
	digest := sha256.Sum256([]byte(token))
	return f.authenticateDigest(username, hex.EncodeToString(digest[:]))
}

// authenticateDigest revalidates a server-stored credential digest against the live file.
func (f *File) authenticateDigest(username, hash string) (Principal, error) {
	entries, err := f.entries()
	if err != nil {
		return Principal{}, err
	}
	for _, entry := range entries {
		if subtle.ConstantTimeCompare([]byte(hash), []byte(entry.TokenSHA256)) != 1 {
			continue
		}
		if username != "" && username != entry.Subject || entry.ExpiresAt != nil && !time.Now().Before(*entry.ExpiresAt) {
			return Principal{}, ErrUnauthorized
		}
		return Principal{Subject: entry.Subject, Role: "operator"}, nil
	}
	return Principal{}, ErrUnauthorized
}
