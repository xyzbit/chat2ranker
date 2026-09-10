// Package webhooksecret stores Webhook signing secrets outside the Rank
// database so public and persistence projections never contain key material.
package webhooksecret

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

var referencePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// FileStore is a local development secret provider. Production deployments can
// replace the application port with a managed credential store.
type FileStore struct{ root string }

// NewFileStore prepares a private directory for Webhook signing secrets.
//
// @param root directory owned by the Rank process.
// @returns a filesystem-backed secret provider.
func NewFileStore(root string) (*FileStore, error) {
	if root == "" {
		return nil, errors.New("webhook secret directory is required")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create webhook secret directory: %w", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return nil, fmt.Errorf("protect webhook secret directory: %w", err)
	}
	return &FileStore{root: root}, nil
}

func (s *FileStore) path(reference string) (string, error) {
	if !referencePattern.MatchString(reference) {
		return "", errors.New("invalid webhook secret reference")
	}
	return filepath.Join(s.root, reference+".secret"), nil
}

// Save writes one new secret without replacing an existing value.
//
// @param _ call context reserved for managed providers.
// @param reference opaque repository reference.
// @param secret serialized signing secret.
// @returns an error when the reference already exists or cannot be written.
func (s *FileStore) Save(_ context.Context, reference string, secret []byte) error {
	path, err := s.path(reference)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err = file.Write(secret); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	return file.Close()
}

// Load returns the exact serialized signing secret.
//
// @param _ call context reserved for managed providers.
// @param reference opaque repository reference.
// @returns secret bytes and any storage error.
func (s *FileStore) Load(_ context.Context, reference string) ([]byte, error) {
	path, err := s.path(reference)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

// Delete removes a secret after its endpoint transaction fails.
//
// @param _ call context reserved for managed providers.
// @param reference opaque repository reference.
// @returns an error other than an absent secret.
func (s *FileStore) Delete(_ context.Context, reference string) error {
	path, err := s.path(reference)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
