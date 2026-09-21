package secret

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// EncryptedStore keeps model credentials in the Execution database. The master
// key belongs to deployment secrets and is never stored with the ciphertext.
type EncryptedStore struct {
	pool   *pgxpool.Pool
	cipher cipher.AEAD
}

func NewEncryptedStore(pool *pgxpool.Pool, key []byte) (*EncryptedStore, error) {
	if len(key) != 32 {
		return nil, errors.New("credential master key must contain 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &EncryptedStore{pool: pool, cipher: aead}, nil
}

func (s *EncryptedStore) Put(ctx context.Context, ref, value string) error {
	nonce := make([]byte, s.cipher.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	sealed := s.cipher.Seal(nonce, nonce, []byte(value), []byte(ref))
	_, err := s.pool.Exec(ctx, `INSERT INTO credentials(ref,ciphertext) VALUES($1,$2) ON CONFLICT(ref) DO UPDATE SET ciphertext=excluded.ciphertext`, ref, sealed)
	return err
}
func (s *EncryptedStore) Get(ctx context.Context, ref string) (string, error) {
	var sealed []byte
	err := s.pool.QueryRow(ctx, `SELECT ciphertext FROM credentials WHERE ref=$1`, ref).Scan(&sealed)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", os.ErrNotExist
	}
	if err != nil {
		return "", err
	}
	if len(sealed) < s.cipher.NonceSize() {
		return "", errors.New("invalid credential ciphertext")
	}
	plaintext, err := s.cipher.Open(nil, sealed[:s.cipher.NonceSize()], sealed[s.cipher.NonceSize():], []byte(ref))
	if err != nil {
		return "", errors.New("credential decryption failed")
	}
	return string(plaintext), nil
}
func (s *EncryptedStore) Delete(ctx context.Context, ref string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM credentials WHERE ref=$1`, ref)
	return err
}
