package secret

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
)

type FileStore struct{ root string }

func NewFileStore(root string) *FileStore { return &FileStore{root: root} }

func (s *FileStore) Put(_ context.Context, ref, value string) error {
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(s.root, 0o700); err != nil {
		return err
	}
	target := s.path(ref)
	temporary, err := os.CreateTemp(s.root, ".credential-*")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	if err = temporary.Chmod(0o600); err == nil {
		_, err = temporary.WriteString(value)
	}
	closeErr := temporary.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(temporary.Name(), target)
}

func (s *FileStore) Get(_ context.Context, ref string) (string, error) {
	value, err := os.ReadFile(s.path(ref))
	if errors.Is(err, os.ErrNotExist) {
		return "", os.ErrNotExist
	}
	return string(value), err
}

func (s *FileStore) Delete(_ context.Context, ref string) error {
	err := os.Remove(s.path(ref))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *FileStore) path(ref string) string {
	hash := sha256.Sum256([]byte(ref))
	return filepath.Join(s.root, hex.EncodeToString(hash[:])+".secret")
}
