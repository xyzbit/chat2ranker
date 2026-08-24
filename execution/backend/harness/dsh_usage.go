package harness

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
	"github.com/xyzbit/chat2ranker/execution/backend/contract"
)

type dshPrice struct {
	input     float64
	cacheRead float64
	output    float64
}

type dshSessionEvent struct {
	Type string `json:"type"`
	Data struct {
		Message struct {
			Source struct {
				Provider string `json:"provider"`
				Model    string `json:"model"`
			} `json:"source"`
		} `json:"message"`
		Usage contract.Usage `json:"usage"`
	} `json:"data"`
}

// readDSHAccounting treats DSH assistant/message events as the usage source of
// truth. Prices are applied only for the official endpoint and known models.
func readDSHAccounting(harnessHome, baseURL string) (contract.Usage, float64, bool, error) {
	paths, err := dshSessionPaths(harnessHome)
	if err != nil {
		return contract.Usage{}, 0, false, err
	}
	if len(paths) == 0 {
		return contract.Usage{}, 0, false, errors.New("DSH session history was not produced")
	}
	usage := contract.Usage{}
	cost, costKnown, calls := 0.0, officialDeepSeekEndpoint(baseURL), 0
	for _, path := range paths {
		events, readErr := readDSHUsageEvents(path)
		if readErr != nil {
			return contract.Usage{}, 0, false, fmt.Errorf("read DSH session history %s: %w", filepath.Base(filepath.Dir(path)), readErr)
		}
		for _, event := range events {
			calls++
			usage.InputTokens += event.Data.Usage.InputTokens
			usage.OutputTokens += event.Data.Usage.OutputTokens
			usage.CacheReadTokens += event.Data.Usage.CacheReadTokens
			usage.CacheWriteTokens += event.Data.Usage.CacheWriteTokens
			usage.ReasoningTokens += event.Data.Usage.ReasoningTokens
			price, known := officialDeepSeekPrice(event.Data.Message.Source.Provider, event.Data.Message.Source.Model)
			if !known {
				costKnown = false
				continue
			}
			cost += (float64(event.Data.Usage.InputTokens)*price.input +
				float64(event.Data.Usage.CacheReadTokens)*price.cacheRead +
				float64(event.Data.Usage.OutputTokens)*price.output) / 1_000_000
		}
	}
	if calls == 0 {
		return contract.Usage{}, 0, false, errors.New("DSH session history contains no model usage")
	}
	return usage, cost, costKnown, nil
}

func dshSessionPaths(harnessHome string) ([]string, error) {
	root := filepath.Join(harnessHome, "sessions")
	paths := []string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if !entry.IsDir() && entry.Name() == "session.jsonl.zstd" {
			paths = append(paths, path)
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return paths, nil
	}
	return paths, err
}

func readDSHUsageEvents(path string) ([]dshSessionEvent, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader, err := zstd.NewReader(file)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	decoder := json.NewDecoder(reader)
	events := []dshSessionEvent{}
	for {
		var event dshSessionEvent
		if err := decoder.Decode(&event); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		if event.Type == "assistant/message" && event.Data.Usage.InputTokens >= 0 && event.Data.Usage.OutputTokens >= 0 {
			events = append(events, event)
		}
	}
	return events, nil
}

func officialDeepSeekEndpoint(baseURL string) bool {
	value := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	return value == "" || value == "https://api.deepseek.com"
}

// Prices are USD per million tokens from DeepSeek's official pricing page,
// verified 2026-08-24: https://api-docs.deepseek.com/quick_start/pricing
func officialDeepSeekPrice(provider, model string) (dshPrice, bool) {
	if provider != "deepseek-official" {
		return dshPrice{}, false
	}
	switch model {
	case "deepseek-v4-flash", "deepseek-chat", "deepseek-reasoner":
		return dshPrice{input: 0.14, cacheRead: 0.0028, output: 0.28}, true
	case "deepseek-v4-pro":
		return dshPrice{input: 0.435, cacheRead: 0.003625, output: 0.87}, true
	default:
		return dshPrice{}, false
	}
}
