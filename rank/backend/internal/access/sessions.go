package access

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

// Sessions issues bounded opaque cookies and rechecks their credential on every request.
// A SessionStore shares revocation and lifetime across API processes.
type Sessions struct {
	store    SessionStore
	mu       sync.Mutex
	entries  map[[32]byte]session
	ttl      time.Duration
	capacity int
}

type session struct {
	username, digest string
	expires          time.Time
}

// NewSessions requires a positive lifetime and capacity.
func NewSessions(ttl time.Duration, capacity int) (*Sessions, error) {
	if ttl < time.Second || capacity < 1 {
		return nil, errors.New("session lifetime must be at least one second and capacity positive")
	}
	return &Sessions{entries: make(map[[32]byte]session), ttl: ttl, capacity: capacity}, nil
}

// Create issues an opaque ID after credentials have been verified by the caller.
// An existing session is replaced atomically; full capacity leaves it intact.
func (s *Sessions) Create(ctx context.Context, username, token, previous string) (string, time.Time, error) {
	if s.store != nil {
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			return "", time.Time{}, err
		}
		id := hex.EncodeToString(raw)
		expires := time.Now().Add(s.ttl)
		err := s.store.CreateSession(ctx, StoredSession{Hash: sessionHash(id), Username: username, CredentialDigest: sessionHash(token), ExpiresAt: expires}, sessionHash(previous), s.capacity)
		return id, expires, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for key, entry := range s.entries {
		if !now.Before(entry.expires) {
			delete(s.entries, key)
		}
	}
	previousKey := sha256.Sum256([]byte(previous))
	_, replacing := s.entries[previousKey]
	if len(s.entries) >= s.capacity && !replacing {
		return "", time.Time{}, errors.New("browser session capacity reached")
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	id := hex.EncodeToString(raw)
	expires := now.Add(s.ttl)
	delete(s.entries, previousKey)
	s.entries[sha256.Sum256([]byte(id))] = session{username, sessionHash(token), expires}
	return id, expires, nil
}

// Authenticate rechecks the credential file so revocation also invalidates cookies.
func (s *Sessions) Authenticate(ctx context.Context, file *File, id string) (Principal, error) {
	if s.store != nil {
		entry, err := s.store.GetSession(ctx, sessionHash(id))
		if err != nil {
			return Principal{}, err
		}
		if !time.Now().Before(entry.ExpiresAt) {
			return Principal{}, ErrUnauthorized
		}
		principal, err := file.authenticateDigest(entry.Username, entry.CredentialDigest)
		if errors.Is(err, ErrUnauthorized) {
			if revokeErr := s.Revoke(ctx, id); revokeErr != nil {
				return Principal{}, revokeErr
			}
		}
		return principal, err
	}
	key := sha256.Sum256([]byte(id))
	s.mu.Lock()
	entry, ok := s.entries[key]
	if ok && !time.Now().Before(entry.expires) {
		delete(s.entries, key)
		ok = false
	}
	s.mu.Unlock()
	if !ok {
		return Principal{}, ErrUnauthorized
	}
	principal, err := file.authenticateDigest(entry.username, entry.digest)
	if errors.Is(err, ErrUnauthorized) {
		if err := s.Revoke(ctx, id); err != nil {
			return Principal{}, err
		}
	}
	return principal, err
}

// Revoke invalidates a session without affecting other browsers.
func (s *Sessions) Revoke(ctx context.Context, id string) error {
	if s.store != nil {
		return s.store.DeleteSession(ctx, sessionHash(id))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, sha256.Sum256([]byte(id)))
	return nil
}

// StoredSession contains digests, never the cookie or operator credential.
type StoredSession struct {
	Hash             string
	Username         string
	CredentialDigest string
	ExpiresAt        time.Time
}

// SessionStore atomically rotates cookies under a shared capacity limit.
// GetSession returns ErrUnauthorized for unknown cookies; storage failures remain errors.
type SessionStore interface {
	CreateSession(context.Context, StoredSession, string, int) error
	GetSession(context.Context, string) (StoredSession, error)
	DeleteSession(context.Context, string) error
}

// NewStoredSessions uses persistent storage for process-independent login and logout.
func NewStoredSessions(ttl time.Duration, capacity int, store SessionStore) (*Sessions, error) {
	s, err := NewSessions(ttl, capacity)
	if err != nil {
		return nil, err
	}
	if store == nil {
		return nil, errors.New("session store is required")
	}
	s.store = store
	return s, nil
}

func sessionHash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}
