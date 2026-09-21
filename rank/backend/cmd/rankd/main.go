package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	executionclient "github.com/xyzbit/agentkiln/execution/backend/client"
	"github.com/xyzbit/agentkiln/rank/backend/internal/access"
	"github.com/xyzbit/agentkiln/rank/backend/internal/app"
	"github.com/xyzbit/agentkiln/rank/backend/internal/domain"
	"github.com/xyzbit/agentkiln/rank/backend/internal/httpapi"
	"github.com/xyzbit/agentkiln/rank/backend/internal/sqlstore"
	"github.com/xyzbit/agentkiln/rank/backend/internal/webhooksecret"
)

func main() {
	mode := flag.String("mode", "all", "all, api, or single-owner dispatcher; split modes require PostgreSQL")
	shutdownTimeout := flag.Duration("shutdown-timeout", 30*time.Second, "maximum time to finish accepted HTTP requests after SIGTERM")
	eventPollInterval := flag.Duration("event-poll-interval", time.Second, "cross-process response event polling interval")
	address := flag.String("addr", "127.0.0.1:8787", "HTTP listen address")
	accessPath := flag.String("access-file", os.Getenv("RANK_ACCESS_FILE"), "operator token digest file; required on non-loopback listeners")
	managementOrigin := flag.String("management-origin", os.Getenv("RANK_MANAGEMENT_ORIGIN"), "public browser origin behind a TLS gateway")
	sessionTTL := flag.Duration("session-ttl", 8*time.Hour, "fixed browser login lifetime")
	sessionCapacity := flag.Int("session-capacity", 1024, "maximum active browser sessions per installation")
	databasePath := flag.String("db", "../var/rank.db", "SQLite database path")
	postgresSchema := flag.String("postgres-schema", envOr("RANK_POSTGRES_SCHEMA", "rank"), "private Rank PostgreSQL schema")
	databaseConnections := flag.Int("database-connections", 8, "maximum Rank PostgreSQL connections")
	databaseTimeout := flag.Duration("database-timeout", 5*time.Second, "PostgreSQL acquisition, query and business transaction deadline; increase for explicit migrations")
	migrate := flag.Bool("migrate", false, "migrate Rank PostgreSQL and exit before accepting traffic")
	executionURL := flag.String("execution-url", envOr("RANK_EXECUTION_URL", "http://127.0.0.1:8790"), "Execution Service base URL")
	executionTimeout := flag.Duration("execution-timeout", 15*time.Minute, "candidate or judge Execution Service timeout")
	responseRetry := flag.Duration("dispatch-retry-interval", time.Second, "serving and evaluation dispatch reconciliation retry interval")
	responseObserve := flag.Duration("execution-observe-timeout", 15*time.Second, "maximum Execution event observation window")
	trialTimeout := flag.Duration("trial-timeout", 2*time.Minute, "Agent playground execution timeout")
	trialRPM := flag.Int("trial-requests-per-minute", 60, "Agent playground requests per minute per target")
	trialConcurrency := flag.Int("trial-concurrency", 4, "Agent playground concurrent responses per target")
	webhookSecretDirectory := flag.String("webhook-secret-dir", "", "private Webhook signing secret directory")
	webhookPollInterval := flag.Duration("webhook-poll-interval", time.Second, "durable Webhook outbox poll interval")
	releasePollInterval := flag.Duration("release-poll-interval", time.Second, "durable release task poll interval")
	webhookAttemptTimeout := flag.Duration("webhook-attempt-timeout", 20*time.Second, "one Webhook HTTP attempt timeout")
	webhookRetryDelays := flag.String("webhook-retry-delays", "5s,5m,30m,2h,5h,10h,14h,20h,24h", "comma-separated Webhook retry delays")
	flag.Parse()
	if (*mode != "all" && *mode != "api" && *mode != "dispatcher") || *shutdownTimeout <= 0 || *eventPollInterval <= 0 || *sessionTTL < time.Second || *sessionCapacity < 1 {
		slog.Error("mode must be all, api or dispatcher; shutdown and event polling durations must be positive; session TTL must be at least 1s and capacity positive")
		os.Exit(1)
	}
	var accessFile *access.File
	if !*migrate {
		host, _, addressErr := net.SplitHostPort(*address)
		ip := net.ParseIP(host)
		if *accessPath == "" && (addressErr != nil || ip == nil || !ip.IsLoopback()) {
			slog.Error("non-loopback management requires -access-file")
			os.Exit(1)
		}
		if *accessPath != "" {
			var err error
			accessFile, err = access.Open(*accessPath)
			if err != nil {
				slog.Error("management authentication configuration is invalid")
				os.Exit(1)
			}
		}
		if *managementOrigin != "" {
			origin, err := url.Parse(*managementOrigin)
			if err != nil || origin.Host == "" || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" || origin.Scheme != "http" && origin.Scheme != "https" || *managementOrigin != origin.Scheme+"://"+origin.Host {
				slog.Error("management-origin must be an HTTP(S) origin without path or credentials")
				os.Exit(1)
			}
		}
	}
	databaseURL := os.Getenv("RANK_DATABASE_URL")
	if *mode != "all" && databaseURL == "" {
		slog.Error("split Rank modes require PostgreSQL")
		os.Exit(1)
	}
	if *migrate && databaseURL == "" {
		slog.Error("explicit migration requires RANK_DATABASE_URL; SQLite migrates on open")
		os.Exit(1)
	}
	if *responseRetry <= 0 || *responseObserve <= 0 || *trialTimeout <= 0 || *trialRPM < 1 || *trialConcurrency < 1 {
		slog.Error("trial timeout and limits must be positive")
		os.Exit(1)
	}
	if *releasePollInterval <= 0 {
		slog.Error("release poll interval must be positive")
		os.Exit(1)
	}
	actionSecret := os.Getenv("RANK_ACTION_SECRET")
	if actionSecret == "" {
		actionSecret = "rank-local-action-secret"
	}
	controlToken := os.Getenv("RANK_CONTROL_TOKEN")
	if controlToken == "" {
		controlToken = "rank-local-control-token"
	}
	if (databaseURL != "" || accessFile != nil) && !*migrate {
		for _, name := range []string{"RANK_ACTION_SECRET", "RANK_CONTROL_TOKEN", "EXECUTION_SERVICE_TOKEN"} {
			value := os.Getenv(name)
			if len(value) < 24 || value == "rank-local-control-token" || value == "rank-local-action-secret" {
				slog.Error("Authenticated server requires an explicit private secret of at least 24 characters", "setting", name)
				os.Exit(1)
			}
		}
	}
	if databaseURL != "" && !*migrate {
		if *webhookSecretDirectory == "" {
			slog.Error("PostgreSQL server requires an explicit persistent webhook-secret-dir")
			os.Exit(1)
		}
	}
	if directory := filepath.Dir(*databasePath); databaseURL == "" && directory != "." {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			slog.Error("create database directory", "error", err)
			os.Exit(1)
		}
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	var store *sqlstore.Store
	var err error
	if databaseURL == "" {
		store, err = sqlstore.OpenSQLite(ctx, *databasePath)
	} else {
		store, err = sqlstore.OpenPostgres(ctx, sqlstore.PostgresConfig{OperationTimeout: *databaseTimeout, URL: databaseURL, Schema: *postgresSchema, MaxConnections: *databaseConnections})
	}
	if err != nil {
		slog.Error("open repository", "error", err)
		os.Exit(1)
	}
	defer store.Close()
	if databaseURL != "" {
		if *migrate {
			if err := store.MigratePostgres(ctx); err != nil {
				slog.Error("migrate Rank PostgreSQL", "error", err)
				os.Exit(1)
			}
			slog.Info("Rank PostgreSQL migration complete")
			return
		}
		if err := store.CheckPostgresSchema(ctx); err != nil {
			slog.Error("check Rank PostgreSQL schema", "error", err)
			os.Exit(1)
		}
	}
	sessions, sessionErr := access.NewStoredSessions(*sessionTTL, *sessionCapacity, store)
	if sessionErr != nil {
		slog.Error(sessionErr.Error())
		os.Exit(1)
	}
	if *webhookSecretDirectory == "" {
		*webhookSecretDirectory = filepath.Join(filepath.Dir(*databasePath), "webhook-secrets")
	}
	secretStore, err := webhooksecret.NewFileStore(*webhookSecretDirectory)
	if err != nil {
		slog.Error("open Webhook secret store", "error", err)
		os.Exit(1)
	}
	retrySchedule, err := parseDurations(*webhookRetryDelays)
	if err != nil {
		slog.Error("parse Webhook retry delays", "error", err)
		os.Exit(1)
	}
	execution := executionclient.New(*executionURL, *executionTimeout).WithToken(os.Getenv("EXECUTION_SERVICE_TOKEN"))
	runners := app.ExecutionRunners(app.ExecutionRunnerConfig{Client: execution, TenantID: envOr("RANK_TENANT_ID", "local"), JudgeHarness: os.Getenv("RANK_JUDGE_RUNNER"), JudgeModel: os.Getenv("RANK_JUDGE_MODEL"), Timeout: *executionTimeout})
	service := app.NewService(store, app.Options{DispatchRetryInterval: *responseRetry, ExecutionObserveTimeout: *responseObserve, Workers: *mode != "api", ExternalDispatch: *mode == "api", EventPollInterval: *eventPollInterval, ActionSecret: actionSecret, Runners: runners, Artifacts: app.ExecutionArtifactReader{Client: execution}, Connections: execution, Execution: execution, TrialPolicy: domain.DeploymentPolicy{RequestsPerMinute: *trialRPM, MaxConcurrentResponses: *trialConcurrency, ExecutionTimeoutMs: trialTimeout.Milliseconds()}, JudgeHarness: os.Getenv("RANK_JUDGE_RUNNER"), JudgeModel: os.Getenv("RANK_JUDGE_MODEL"), TenantID: envOr("RANK_TENANT_ID", "local"), WebhookSecrets: secretStore, WebhookPollInterval: *webhookPollInterval, WebhookAttemptTimeout: *webhookAttemptTimeout, WebhookRetrySchedule: retrySchedule})
	defer service.StopServingResponses()
	defer service.StopRunWorkers()
	if err := service.EnsureSeed(ctx); err != nil {
		slog.Error("seed database", "error", err)
		os.Exit(1)
	}
	var dispatchers sync.WaitGroup
	if *mode != "api" {
		if err := service.ResumeActiveRuns(ctx); err != nil {
			slog.Error("recover active runs", "error", err)
			os.Exit(1)
		}
		if err := service.ResumeActiveResponses(ctx); err != nil {
			slog.Error("recover active Responses", "error", err)
			os.Exit(1)
		}
		for _, run := range []func(context.Context) error{
			service.RunWebhookDispatcher,
			func(ctx context.Context) error { return service.RunReleaseDispatcher(ctx, *releasePollInterval) },
			service.RunPendingDispatcher,
		} {
			dispatchers.Add(1)
			go func() {
				defer dispatchers.Done()
				if err := run(ctx); err != nil && ctx.Err() == nil {
					slog.Error("Rank dispatcher stopped", "error", err)
					cancel()
				}
			}()
		}
	}
	server := &http.Server{Addr: *address, Handler: httpapi.New(service, httpapi.Options{Sessions: sessions, AccessFile: accessFile, ManagementOrigin: *managementOrigin, ControlToken: controlToken, ControlURL: envOr("RANK_CONTROL_URL", "http://127.0.0.1:8788"), Storage: store.Driver()}), ReadHeaderTimeout: 5 * time.Second}
	if *mode == "dispatcher" {
		health := http.NewServeMux()
		health.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"runtime":"rank-dispatcher"}`))
		})
		server.Handler = health
	}
	go func() {
		slog.Info("rankd listening", "address", *address, "storage", store.Driver())
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("rankd stopped", "error", err)
			cancel()
		}
	}()
	<-ctx.Done()
	if err := shutdownHTTP(server, *shutdownTimeout); err != nil {
		slog.Warn("HTTP drain deadline reached; remote executions remain durable", "error", err)
	}
	dispatchers.Wait()
}

func parseDurations(value string) ([]time.Duration, error) {
	parts := strings.Split(value, ",")
	result := make([]time.Duration, 0, len(parts))
	for _, part := range parts {
		delay, err := time.ParseDuration(strings.TrimSpace(part))
		if err != nil || delay <= 0 {
			return nil, errors.New("Webhook retry delays must be positive Go durations")
		}
		result = append(result, delay)
	}
	if len(result) == 0 {
		return nil, errors.New("at least one Webhook retry delay is required")
	}
	return result, nil
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

// shutdownHTTP bounds draining and closes remaining connections when the deadline expires.
func shutdownHTTP(server *http.Server, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	err := server.Shutdown(ctx)
	if err != nil {
		_ = server.Close()
	}
	return err
}
