package executor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/execution/backend/internal/domain"
	"github.com/xyzbit/agentkiln/execution/backend/internal/sqlite"
)

func TestWorkerEnvironmentCarriesPackagedDSHPath(t *testing.T) {
	t.Setenv("RANK_DSH_BIN", "/runtime/dsh.js")
	if environment := workerEnvironment("/runtime"); !slices.Contains(environment, "RANK_DSH_BIN=/runtime/dsh.js") {
		t.Fatalf("worker environment omitted packaged DSH path: %#v", environment)
	}
}

func TestUnconfirmedForcedStopRetainsTheRuntimeWorkspace(t *testing.T) {
	root := t.TempDir()
	worker := filepath.Join(root, "unresponsive-worker.mjs")
	source := `#!/usr/bin/env node
import { mkdir, writeFile } from 'node:fs/promises';
let raw = '';
for await (const chunk of process.stdin) raw += chunk;
const request = JSON.parse(raw);
await mkdir(request.workspaceDir, { recursive: true });
process.on('SIGTERM', () => {});
await writeFile(request.workspaceDir + '/started', 'owned workspace');
setInterval(() => {}, 1000);
`
	if err := os.WriteFile(worker, []byte(source), 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(t.Context(), filepath.Join(root, "execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	local := NewLocal(LocalConfig{Repository: store, WorkerBinary: worker, SandboxRoot: filepath.Join(root, "sandboxes"), ArtifactRoot: filepath.Join(root, "artifacts"), Timeout: 5 * time.Second, TerminationGrace: 20 * time.Millisecond})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		defer close(done)
		_, err := local.Run(ctx, contract.Execution{ID: "unresponsive", Spec: contract.Spec{ProtocolVersion: contract.ProtocolVersion, Kind: contract.KindAgent, Harness: "mock", Prompt: "test"}}, nil)
		done <- err
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("runtime cleanup deadline elapsed")
		}
	})
	marker := filepath.Join(root, "sandboxes/unresponsive/started")
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("unresponsive worker did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, domain.ErrRuntimeStopUnconfirmed) {
			t.Fatalf("forced stop reported %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("worker process was not reaped")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("unconfirmed workspace was removed: %v", err)
	}
}
