// Package artifacts stores immutable execution evidence independently of workers.
package artifacts

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net/url"
	"strings"
	"time"
)

var ErrSigningUnsupported = errors.New("this artifact store serves reads through the authenticated Execution API")

// Store receives content-addressed keys. Callers authenticate access through the
// owning execution before reading or signing a URL; possession of a key is not
// authorization. Upload completion precedes publishing its database reference.
type Store interface {
	Put(context.Context, string, io.Reader, int64) error
	Open(context.Context, string) (io.ReadCloser, error)
	Delete(context.Context, string) error
	SignedURL(context.Context, string, time.Duration) (*url.URL, error)
}

func validateKey(key string) error {
	if key == "." || !fs.ValidPath(key) || strings.Contains(key, "\\") {
		return errors.New("artifact key must be a relative slash-separated file path")
	}
	return nil
}
