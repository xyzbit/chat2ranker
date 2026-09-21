package dispatch_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/xyzbit/agentkiln/execution/backend/client"
	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/execution/backend/internal/postgres"
)

type childProcess struct {
	command *exec.Cmd
	done    chan error
	logPath string
}

func TestPackagedAPIAndWorkersShareDurableExecutions(t *testing.T) {
	url := os.Getenv("EXECUTION_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("EXECUTION_TEST_DATABASE_URL is required")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	temporary := t.TempDir()
	binary := filepath.Join(temporary, "executiond")
	workerBinary := filepath.Join(temporary, "execution-worker")
	for name, path := range map[string]string{"executiond": binary, "execution-worker": workerBinary} {
		command := exec.Command("go", "build", "-o", path, "./cmd/"+name)
		command.Dir = root
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", name, err, output)
		}
	}
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	schema := "execution_process_" + hex.EncodeToString(random[:8])
	store, err := postgres.Open(t.Context(), postgres.Config{OperationTimeout: 5 * time.Second, URL: url, Schema: schema, MaxQueued: 50, MaxAttempts: 3})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := store.Pool().Exec(ctx, "DROP SCHEMA IF EXISTS "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Error(err)
		}
		store.Close()
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	token := hex.EncodeToString(random[:])
	argv, _ := json.Marshal([]string{"node", filepath.Join(root, "../../rank/examples/execution-lifecycle/harness.mjs"), "{prompt}", "{workspace}"})
	environment := []string{}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(name, "EXECUTION_") && !strings.Contains(name, "KEY") && !strings.Contains(name, "SECRET") && !strings.Contains(name, "TOKEN") && !strings.Contains(name, "PASSWORD") {
			environment = append(environment, entry)
		}
	}
	environment = append(environment, "EXECUTION_DATABASE_URL="+url, "EXECUTION_SERVICE_TOKEN="+token, "EXECUTION_CREDENTIAL_KEY="+base64.StdEncoding.EncodeToString(random[:]), "EXECUTION_HARNESS_DSH_ARGV="+string(argv))
	arguments := []string{"-addr", address, "-postgres-schema", schema, "-worker", workerBinary, "-worker-version", "process-test", "-artifacts", filepath.Join(temporary, "artifacts"), "-sandboxes", filepath.Join(temporary, "sandboxes"), "-worker-timeout", "3s", "-cleanup-grace", "1s", "-persistence-timeout", "2s", "-rescue-after", "7s", "-poll-interval", "30ms", "-shutdown-grace", "5s", "-online-slots", "1", "-evaluation-slots", "1"}
	migrate := exec.Command(binary, append(append([]string{}, arguments...), "-migrate")...)
	migrate.Env = environment
	if output, err := migrate.CombinedOutput(); err != nil {
		t.Fatalf("migration: %v\n%s", err, output)
	}
	start := func(role, id string) *childProcess {
		t.Helper()
		logPath := filepath.Join(temporary, id+".log")
		logFile, err := os.Create(logPath)
		if err != nil {
			t.Fatal(err)
		}
		command := exec.Command(binary, append(append([]string{}, arguments...), "-role", role, "-worker-id", id)...)
		command.Env = environment
		command.Stdout = logFile
		command.Stderr = logFile
		if err := command.Start(); err != nil {
			logFile.Close()
			t.Fatal(err)
		}
		child := &childProcess{command: command, done: make(chan error, 1), logPath: logPath}
		go func() { child.done <- command.Wait(); logFile.Close(); close(child.done) }()
		t.Cleanup(func() {
			_ = command.Process.Signal(syscall.SIGTERM)
			select {
			case <-child.done:
			case <-time.After(8 * time.Second):
				_ = command.Process.Kill()
				<-child.done
			}
			if t.Failed() {
				data, _ := os.ReadFile(logPath)
				t.Logf("%s log:\n%s", id, data)
			}
		})
		waitFor(t, 5*time.Second, func() bool {
			data, _ := os.ReadFile(logPath)
			return strings.Contains(string(data), "executiond started")
		})
		return child
	}
	start("api", "api")
	baseURL := "http://" + address
	api := client.New(baseURL, 10*time.Second).WithToken(token)
	response, err := http.Get(baseURL + "/v1/executions/anything")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated access: %d", response.StatusCode)
	}
	request := func(key, prompt string, online bool) contract.SubmitRequest {
		metadata := map[string]string{}
		if online {
			metadata["responseId"] = key
		}
		return contract.SubmitRequest{IdempotencyKey: key, Spec: contract.Spec{ProtocolVersion: contract.ProtocolVersion, Kind: contract.KindAgent, Harness: "dsh", Prompt: prompt, Metadata: metadata}}
	}
	queued, err := api.Submit(t.Context(), request("queued-cancel", "quick", true))
	if err != nil {
		t.Fatal(err)
	}
	if cancelled, err := api.Cancel(t.Context(), queued.ID); err != nil || cancelled.Status != contract.StatusCancelled || cancelled.Attempt != 0 {
		t.Fatalf("queued cancel: %#v %v", cancelled, err)
	}
	workers := map[string]*childProcess{"worker-a": start("worker", "worker-a"), "worker-b": start("worker", "worker-b")}
	ids := []string{}
	for i := range 8 {
		value, err := api.Submit(t.Context(), request(fmt.Sprintf("quick-%d", i), "quick", i%2 == 0))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, value.ID)
	}
	owners := map[string]bool{}
	for _, id := range ids {
		value := waitExecution(t, api, id, contract.StatusCompleted, 10*time.Second)
		if value.Attempt != 1 || value.Result == nil || !strings.Contains(value.Result.Output, "fixture completed") {
			t.Fatalf("unexpected result: %#v", value)
		}
		owners[value.ExternalHandle] = true
		var state string
		if err := store.Pool().QueryRow(t.Context(), `SELECT j.state::text FROM executions e JOIN river_job j ON j.id=e.job_id WHERE e.id=$1`, id).Scan(&state); err != nil || state != "completed" {
			t.Fatalf("result published without job acknowledgement: %s %v", state, err)
		}
	}
	if len(owners) != 2 {
		t.Fatalf("work did not reach both workers: %v", owners)
	}
	duplicate, err := api.Submit(t.Context(), request("quick-0", "quick", true))
	if err != nil || duplicate.ID != ids[0] {
		t.Fatalf("duplicate submission: %#v %v", duplicate, err)
	}
	if _, err := api.Submit(t.Context(), request("quick-0", "different", true)); err == nil {
		t.Fatal("changed idempotent payload was accepted")
	}
	active, err := api.Submit(t.Context(), request("running-cancel", "hold", true))
	if err != nil {
		t.Fatal(err)
	}
	pidPath := filepath.Join(temporary, "sandboxes", active.ID, "fixture-pids.json")
	var pids []int
	waitFor(t, 5*time.Second, func() bool {
		data, err := os.ReadFile(pidPath)
		return err == nil && json.Unmarshal(data, &pids) == nil && len(pids) == 2
	})
	if _, err := api.Cancel(t.Context(), active.ID); err != nil {
		t.Fatal(err)
	}
	waitExecution(t, api, active.ID, contract.StatusCancelled, 5*time.Second)
	for _, pid := range pids {
		waitFor(t, 3*time.Second, func() bool { return syscall.Kill(pid, 0) != nil })
	}

	lost, err := api.Submit(t.Context(), request("owner-loss", "hold", true))
	if err != nil {
		t.Fatal(err)
	}
	owner := waitExecution(t, api, lost.ID, contract.StatusRunning, 5*time.Second)
	if err := workers[owner.ExternalHandle].command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	failed := waitExecution(t, api, lost.ID, contract.StatusFailed, 50*time.Second)
	if failed.Attempt != 1 || !strings.Contains(failed.Error, "execution_outcome_unknown") {
		t.Fatalf("owner loss was replayed or misreported: %#v", failed)
	}
	events, err := store.ListEvents(t.Context(), lost.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	starts := 0
	for _, event := range events {
		if event.Type == "execution.running" {
			starts++
		}
	}
	if starts != 1 {
		t.Fatalf("owner loss launched %d attempts", starts)
	}
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("condition did not become true before timeout")
}
func waitExecution(t *testing.T, api *client.Client, id, status string, timeout time.Duration) contract.Execution {
	t.Helper()
	var current contract.Execution
	waitFor(t, timeout, func() bool {
		value, err := api.Get(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		current = value
		if contract.IsTerminal(value.Status) && value.Status != status {
			t.Fatalf("unexpected execution status: %#v", value)
		}
		return value.Status == status
	})
	return current
}
