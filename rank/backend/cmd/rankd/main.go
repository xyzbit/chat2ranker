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

	executionclient "github.com/xyzbit/chat2ranker/execution/backend/client"
	"github.com/xyzbit/chat2ranker/rank/backend/internal/app"
	"github.com/xyzbit/chat2ranker/rank/backend/internal/httpapi"
	"github.com/xyzbit/chat2ranker/rank/backend/internal/sqlite"
)

func main() {
	address := flag.String("addr", "127.0.0.1:8787", "HTTP listen address")
	databasePath := flag.String("db", "../var/rank.db", "SQLite database path")
	executionURL := flag.String("execution-url", envOr("RANK_EXECUTION_URL", "http://127.0.0.1:8790"), "Execution Service base URL")
	executionTimeout := flag.Duration("execution-timeout", 15*time.Minute, "candidate or judge Execution Service timeout")
	flag.Parse()
	actionSecret := os.Getenv("RANK_ACTION_SECRET")
	if actionSecret == "" {
		actionSecret = "rank-local-action-secret"
	}
	controlToken := os.Getenv("RANK_CONTROL_TOKEN")
	if controlToken == "" {
		controlToken = "rank-local-control-token"
	}
	if directory := filepath.Dir(*databasePath); directory != "." {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			slog.Error("create database directory", "error", err)
			os.Exit(1)
		}
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	store, err := sqlite.Open(ctx, *databasePath)
	if err != nil {
		slog.Error("open repository", "error", err)
		os.Exit(1)
	}
	defer store.Close()
	execution := executionclient.New(*executionURL, *executionTimeout)
	runners := app.ExecutionRunners(app.ExecutionRunnerConfig{Client: execution, JudgeHarness: os.Getenv("RANK_JUDGE_RUNNER"), JudgeModel: os.Getenv("RANK_JUDGE_MODEL"), Timeout: *executionTimeout})
	service := app.NewService(store, app.Options{Workers: true, ActionSecret: actionSecret, Runners: runners, Artifacts: app.ExecutionArtifactReader{Client: execution}, Connections: execution, JudgeHarness: os.Getenv("RANK_JUDGE_RUNNER"), JudgeModel: os.Getenv("RANK_JUDGE_MODEL")})
	if err := service.EnsureSeed(ctx); err != nil {
		slog.Error("seed database", "error", err)
		os.Exit(1)
	}
	if err := service.ResumeActiveRuns(ctx); err != nil {
		slog.Error("recover active runs", "error", err)
		os.Exit(1)
	}
	server := &http.Server{Addr: *address, Handler: httpapi.New(service, httpapi.Options{ControlToken: controlToken, ControlURL: envOr("RANK_CONTROL_URL", "http://127.0.0.1:8788")}), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		slog.Info("rankd listening", "address", *address, "database", *databasePath)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("rankd stopped", "error", err)
			cancel()
		}
	}()
	<-ctx.Done()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = server.Shutdown(shutdownCtx)
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
