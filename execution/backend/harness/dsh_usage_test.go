package harness

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestReadDSHAccountingAggregatesNativeUsageAndOfficialCost(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "workspace", "session-one", "session.jsonl.zstd")
	writeDSHSession(t, path,
		map[string]any{"type": "session", "version": 0},
		map[string]any{"type": "assistant/message", "data": map[string]any{
			"message": map[string]any{"source": map[string]any{"provider": "deepseek-official", "model": "deepseek-v4-flash"}},
			"usage":   map[string]any{"inputTokens": 1_000_000, "outputTokens": 100_000, "cacheReadTokens": 500_000, "reasoningTokens": 10_000},
		}},
	)
	usage, cost, known, err := readDSHAccounting(home, "")
	if err != nil {
		t.Fatal(err)
	}
	if usage.InputTokens != 1_000_000 || usage.OutputTokens != 100_000 || usage.CacheReadTokens != 500_000 || usage.ReasoningTokens != 10_000 {
		t.Fatalf("unexpected usage: %#v", usage)
	}
	if !known || math.Abs(cost-0.1694) > 0.0000001 {
		t.Fatalf("unexpected official cost: known=%v cost=%f", known, cost)
	}
}

func TestReadDSHAccountingKeepsUsageButRejectsCustomEndpointPricing(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "workspace", "session-one", "session.jsonl.zstd")
	writeDSHSession(t, path, map[string]any{"type": "assistant/message", "data": map[string]any{
		"message": map[string]any{"source": map[string]any{"provider": "deepseek-official", "model": "deepseek-v4-flash"}},
		"usage":   map[string]any{"inputTokens": 20, "outputTokens": 5},
	}})
	usage, _, known, err := readDSHAccounting(home, "https://gateway.example.test/v1")
	if err != nil || usage.InputTokens != 20 || known {
		t.Fatalf("custom endpoint must retain usage with unknown cost: usage=%#v known=%v err=%v", usage, known, err)
	}
}

func writeDSHSession(t *testing.T, path string, events ...map[string]any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := zstd.NewWriter(file)
	if err != nil {
		t.Fatal(err)
	}
	encoder := json.NewEncoder(writer)
	for _, event := range events {
		if err := encoder.Encode(event); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
