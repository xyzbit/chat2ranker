package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/xyzbit/chat2ranker/execution/backend/harness"
	"github.com/xyzbit/chat2ranker/execution/backend/internal/app"
	"github.com/xyzbit/chat2ranker/execution/backend/internal/executor"
	"github.com/xyzbit/chat2ranker/execution/backend/internal/httpapi"
	"github.com/xyzbit/chat2ranker/execution/backend/internal/sqlite"
)

func main() {
	address := flag.String("addr", "127.0.0.1:8790", "HTTP listen address")
	databasePath := flag.String("db", "../../rank/var/execution.db", "SQLite database path")
	workerBinary := flag.String("worker", os.Getenv("EXECUTION_WORKER_BIN"), "execution-worker executable path")
	repositoryRoot := flag.String("repo-root", os.Getenv("EXECUTION_REPO_ROOT"), "chat2ranker repository root")
	artifactRoot := flag.String("artifacts", "../../rank/var/artifacts", "artifact store root")
	sandboxRoot := flag.String("sandboxes", "../../rank/var/sandboxes", "local process sandbox root")
	workerTimeout := flag.Duration("worker-timeout", 10*time.Minute, "per harness invocation timeout")
	workerVersion := flag.String("worker-version", os.Getenv("EXECUTION_WORKER_VERSION"), "immutable execution-worker release identifier")
	flag.Parse()
	for _, path := range []string{*databasePath, filepath.Join(*artifactRoot, ".keep"), filepath.Join(*sandboxRoot, ".keep")} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			slog.Error("create runtime directory", "error", err)
			os.Exit(1)
		}
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	store, err := sqlite.Open(ctx, *databasePath)
	if err != nil {
		slog.Error("open execution repository", "error", err)
		os.Exit(1)
	}
	defer store.Close()
	harnesses, err := harness.DefaultRegistry(*repositoryRoot)
	if err != nil {
		slog.Error("configure harness adapters", "error", err)
		os.Exit(1)
	}
	local := executor.NewLocal(executor.LocalConfig{WorkerBinary: *workerBinary, RepositoryRoot: *repositoryRoot, ArtifactRoot: *artifactRoot, SandboxRoot: *sandboxRoot, Timeout: *workerTimeout, Harnesses: harnesses})
	service := app.NewService(store, local, app.Options{Workers: true, WorkerVersion: *workerVersion, ArtifactRoot: *artifactRoot})
	if err := service.ResumeActive(ctx); err != nil {
		slog.Error("resume active executions", "error", err)
		os.Exit(1)
	}
	server := &http.Server{Addr: *address, Handler: httpapi.New(service), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		slog.Info("executiond listening", "address", *address, "database", *databasePath, "executor", local.Name())
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("executiond stopped", "error", err)
			cancel()
		}
	}()
	<-ctx.Done()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = server.Shutdown(shutdownCtx)
}
