package sandboxruntime

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestArtifactTransferRejectsEscapeAndBoundsBytes(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "private")
	if err := os.WriteFile(outside, []byte("control credential"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	content := bytes.Repeat([]byte("safe data\n"), 12000)
	if err := os.WriteFile(filepath.Join(root, "stdout"), content, 0600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		limit int64
	}{{"escape", 1 << 20}, {"../private", 1 << 20}, {"stdout", 5}, {".", 1 << 20}} {
		var out bytes.Buffer
		if err := StreamArtifact(&out, root, test.name, test.limit); err == nil {
			t.Fatalf("unsafe transfer accepted: %s", test.name)
		}
		if bytes.Contains(out.Bytes(), []byte("control credential")) {
			t.Fatal("private content escaped")
		}
	}
	var out bytes.Buffer
	if err := StreamArtifact(&out, root, "stdout", int64(len(content))); err != nil {
		t.Fatal(err)
	}
	reader := json.NewDecoder(&out)
	var got bytes.Buffer
	complete := false
	for {
		var chunk ArtifactChunk
		err := reader.Decode(&chunk)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if chunk.Complete {
			sum := sha256.Sum256(got.Bytes())
			if chunk.Size != int64(len(content)) || chunk.SHA256 != hex.EncodeToString(sum[:]) {
				t.Fatal("digest mismatch")
			}
			complete = true
		} else {
			if complete {
				t.Fatal("data after completion")
			}
			got.Write(chunk.Data)
		}
	}
	if !complete || !bytes.Equal(content, got.Bytes()) {
		t.Fatal("artifact was truncated or changed")
	}
}
