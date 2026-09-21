package executor

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestSandboxArtifactRedactionCoversEscapedAndCompressedCredentials(t *testing.T) {
	secret := "private-key-\"\\\n-value"
	content, err := json.Marshal(map[string]string{"credential": secret})
	if err != nil {
		t.Fatal(err)
	}
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer encoder.Close()
	decoder, err := zstd.NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer decoder.Close()
	remote := OpenSandbox{config: OpenSandboxConfig{MaxArtifactBytes: 1 << 20}}
	for _, compressed := range []bool{false, true} {
		input, name := content, "request.json"
		if compressed {
			input, name = encoder.EncodeAll(content, nil), "history.jsonl.zstd"
		}
		output, err := remote.redactArtifact(input, name, []string{secret})
		if err != nil {
			t.Fatal(err)
		}
		if compressed {
			output, err = decoder.DecodeAll(output, nil)
			if err != nil {
				t.Fatal(err)
			}
		}
		var record map[string]string
		if err := json.Unmarshal(output, &record); err != nil {
			t.Fatal(err)
		}
		if record["credential"] != "[REDACTED]" {
			t.Fatal("credential survived artifact publication")
		}
	}
	large := encoder.EncodeAll(bytes.Repeat([]byte("x"), 2<<20), nil)
	if _, err := remote.redactArtifact(large, "history.zstd", nil); err == nil {
		t.Fatal("compressed content exceeded decoded byte limit")
	}
	if _, err := remote.redactArtifact([]byte("invalid"), "history.zstd", nil); err == nil {
		t.Fatal("invalid compressed history was published")
	}
}
