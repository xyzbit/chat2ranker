package webhooksecret_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/xyzbit/chat2ranker/rank/backend/internal/webhooksecret"
)

func TestFileStoreUsesPrivateCreateOnlyFiles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "secrets")
	store, err := webhooksecret.NewFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := store.Save(ctx, "wh_test", []byte("whsec_test")); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, "wh_test", []byte("replacement")); err == nil {
		t.Fatal("existing secret was overwritten")
	}
	value, err := store.Load(ctx, "wh_test")
	if err != nil || string(value) != "whsec_test" {
		t.Fatalf("load secret: %q %v", value, err)
	}
	info, err := os.Stat(filepath.Join(root, "wh_test.secret"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("secret permission = %o", info.Mode().Perm())
	}
	if err := store.Delete(ctx, "wh_test"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(ctx, "wh_test"); !os.IsNotExist(err) {
		t.Fatalf("deleted secret remained readable: %v", err)
	}
}

func TestFileStoreRejectsPathTraversal(t *testing.T) {
	store, err := webhooksecret.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), "../escape", []byte("secret")); err == nil {
		t.Fatal("path traversal reference was accepted")
	}
}
