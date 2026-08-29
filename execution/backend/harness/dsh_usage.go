package harness

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/klauspost/compress/zstd"
	"github.com/xyzbit/chat2ranker/execution/backend/contract"
)

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

// readDSHAccounting treats DSH assistant/message events as the usage and
// resolved-model source of truth.
func readDSHAccounting(harnessHome string) (contract.Usage, string, string, error) {
	paths, err := dshSessionPaths(harnessHome)
	if err != nil {
		return contract.Usage{}, "", "", err
	}
	if len(paths) == 0 {
		return contract.Usage{}, "", "", errors.New("DSH session history was not produced")
	}
	usage := contract.Usage{}
	provider, model, calls := "", "", 0
	for _, path := range paths {
		events, readErr := readDSHUsageEvents(path)
		if readErr != nil {
			return contract.Usage{}, "", "", fmt.Errorf("read DSH session history %s: %w", filepath.Base(filepath.Dir(path)), readErr)
		}
		for _, event := range events {
			calls++
			usage.InputTokens += event.Data.Usage.InputTokens
			usage.OutputTokens += event.Data.Usage.OutputTokens
			usage.CacheReadTokens += event.Data.Usage.CacheReadTokens
			usage.CacheWriteTokens += event.Data.Usage.CacheWriteTokens
			usage.ReasoningTokens += event.Data.Usage.ReasoningTokens
			currentProvider, currentModel := event.Data.Message.Source.Provider, event.Data.Message.Source.Model
			if provider == "" {
				provider, model = currentProvider, currentModel
			} else if provider != currentProvider || model != currentModel {
				provider, model = "", ""
			}
		}
	}
	if calls == 0 {
		return contract.Usage{}, "", "", errors.New("DSH session history contains no model usage")
	}
	return usage, provider, model, nil
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
