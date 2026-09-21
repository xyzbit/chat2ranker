package artifacts

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/url"
	"os"
	"path"
	"time"
)

// Local confines file operations to Root, including symlink traversal.
type Local struct{ Root string }

func (store Local) Put(ctx context.Context, key string, reader io.Reader, size int64) error {
	if err := validateKey(key); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := os.OpenRoot(store.Root)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.MkdirAll(path.Dir(key), 0o700); err != nil {
		return err
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	temporary := path.Join(path.Dir(key), ".upload-"+hex.EncodeToString(random[:]))
	file, err := root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer root.Remove(temporary)
	written, copyErr := io.Copy(file, io.LimitReader(reader, size+1))
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written != size {
		return io.ErrUnexpectedEOF
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return root.Rename(temporary, key)
}

func (store Local) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := validateKey(key); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return os.OpenInRoot(store.Root, key)
}

func (store Local) Delete(ctx context.Context, key string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := os.OpenRoot(store.Root)
	if err != nil {
		return err
	}
	defer root.Close()
	return root.Remove(key)
}

func (Local) SignedURL(context.Context, string, time.Duration) (*url.URL, error) {
	return nil, ErrSigningUnsupported
}
