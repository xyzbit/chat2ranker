// Package harness defines the plug-in seam between execution-worker and an
// agent harness such as DeepSeek Harness, Codex, Pi, or Claude Code.
package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/xyzbit/chat2ranker/execution/backend/contract"
)

// ProgressEvent is emitted while an adapter is still running.
type ProgressEvent struct {
	Type    string
	Message string
	Data    json.RawMessage
	At      time.Time
}

// Invocation contains the immutable execution specification and the private
// directories materialized for one worker attempt.
type Invocation struct {
	ExecutionID string
	Spec        contract.Spec
	Workspace   string
	ArtifactDir string
	HarnessHome string
	Metadata    map[string]string
	Emit        func(ProgressEvent) error
}

// Adapter executes one invocation using a specific agent harness.
type Adapter interface {
	ID() string
	Probe(context.Context) contract.Availability
	Run(context.Context, Invocation) (contract.Result, error)
}

// Registry resolves adapters by their stable harness identifier.
type Registry struct{ adapters map[string]Adapter }

// NewRegistry creates a registry and rejects duplicate or empty identifiers.
func NewRegistry(adapters ...Adapter) (*Registry, error) {
	registry := &Registry{adapters: make(map[string]Adapter, len(adapters))}
	for _, adapter := range adapters {
		if adapter == nil || strings.TrimSpace(adapter.ID()) == "" {
			return nil, fmt.Errorf("harness adapter id is required")
		}
		if _, exists := registry.adapters[adapter.ID()]; exists {
			return nil, fmt.Errorf("duplicate harness adapter %q", adapter.ID())
		}
		registry.adapters[adapter.ID()] = adapter
	}
	return registry, nil
}

// Probe reports whether an adapter can run in the current deployment.
func (registry *Registry) Probe(ctx context.Context, id string) contract.Availability {
	adapter := registry.adapters[id]
	if adapter == nil {
		return contract.Availability{Reason: "未知 Harness：" + id}
	}
	return adapter.Probe(ctx)
}

// Run resolves and invokes one adapter.
func (registry *Registry) Run(ctx context.Context, invocation Invocation) (contract.Result, error) {
	adapter := registry.adapters[invocation.Spec.Harness]
	if adapter == nil {
		return contract.Result{}, fmt.Errorf("unknown harness %q", invocation.Spec.Harness)
	}
	availability := adapter.Probe(ctx)
	if !availability.Available {
		return contract.Result{}, fmt.Errorf("harness %q unavailable: %s", invocation.Spec.Harness, availability.Reason)
	}
	return adapter.Run(ctx, invocation)
}

// DefaultRegistry assembles the first-party adapters from deployment
// configuration. External CLI templates remain shell-free JSON argv arrays.
func DefaultRegistry(repositoryRoot string) (*Registry, error) {
	definitions := []struct {
		id    string
		label string
	}{
		{"dsh", "DeepSeek Harness"},
		{"pi", "Pi"},
		{"claude-code", "Claude Code"},
		{"codex", "Codex"},
		{"hermes", "Hermes"},
	}
	adapters := []Adapter{NewMock()}
	for _, definition := range definitions {
		key := ArgvEnvironmentKey(definition.id)
		configured := strings.TrimSpace(os.Getenv(key))
		if configured != "" {
			argv, err := ParseArgv(key, configured)
			if err != nil {
				return nil, err
			}
			adapters = append(adapters, NewCommand(CommandConfig{ID: definition.id, Label: definition.label, Argv: argv}))
			continue
		}
		if definition.id == "dsh" {
			adapters = append(adapters, NewDSH(repositoryRoot))
			continue
		}
		adapters = append(adapters, NewCommand(CommandConfig{ID: definition.id, Label: definition.label, MissingReason: "未配置 " + key}))
	}
	return NewRegistry(adapters...)
}

// ArgvEnvironmentKey returns the deployment variable for an adapter argv.
func ArgvEnvironmentKey(id string) string {
	return "EXECUTION_HARNESS_" + strings.ToUpper(strings.ReplaceAll(id, "-", "_")) + "_ARGV"
}
