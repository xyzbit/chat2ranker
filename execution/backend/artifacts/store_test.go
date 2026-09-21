package artifacts

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestArtifactStoreReadDeleteAndSignedAccess(t *testing.T) {
	t.Run("local", func(t *testing.T) { exerciseStore(t, Local{Root: t.TempDir()}, false) })
	t.Run("s3", func(t *testing.T) {
		if os.Getenv("EXECUTION_TEST_S3_ENDPOINT") == "" {
			t.Skip("EXECUTION_TEST_S3_ENDPOINT is required")
		}
		store, err := NewS3(t.Context(), S3Config{Endpoint: os.Getenv("EXECUTION_TEST_S3_ENDPOINT"), Bucket: os.Getenv("EXECUTION_TEST_S3_BUCKET"), Region: "us-east-1", AccessKey: os.Getenv("EXECUTION_TEST_S3_ACCESS_KEY"), SecretKey: os.Getenv("EXECUTION_TEST_S3_SECRET_KEY"), Secure: false})
		if err != nil {
			t.Fatal(err)
		}
		exerciseStore(t, store, true)
	})
}

func exerciseStore(t *testing.T, store Store, signed bool) {
	t.Helper()
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	key := "test/" + hex.EncodeToString(random[:]) + "/artifact.txt"
	data := []byte("immutable execution evidence\n")
	if err := store.Put(t.Context(), key, bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Delete(context.Background(), key) })
	reader, err := store.Open(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := io.ReadAll(reader)
	reader.Close()
	if err != nil || !bytes.Equal(actual, data) {
		t.Fatalf("artifact bytes differ: %v", err)
	}
	url, err := store.SignedURL(t.Context(), key, time.Minute)
	if signed {
		if err != nil {
			t.Fatal(err)
		}
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url.String(), nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal("signed artifact download failed")
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != 200 || !bytes.Equal(body, data) {
			t.Fatal("signed artifact did not return stored bytes")
		}
	} else if err != ErrSigningUnsupported {
		t.Fatal("local store promised a signed URL")
	}
	if err := store.Delete(t.Context(), key); err != nil {
		t.Fatal(err)
	}
	if reader, err := store.Open(t.Context(), key); err == nil {
		reader.Close()
		t.Fatal("deleted artifact remained readable")
	}
}

func TestLocalArtifactAccessRejectsTraversalAndSymlinkEscape(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "private"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	store := Local{Root: root}
	for _, key := range []string{"../private", "/private", "escape/private", "dir\\private"} {
		if reader, err := store.Open(t.Context(), key); err == nil {
			reader.Close()
			t.Fatalf("allowed path %q", key)
		}
	}
}
