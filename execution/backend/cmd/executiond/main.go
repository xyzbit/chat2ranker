package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/xyzbit/agentkiln/execution/backend/artifacts"
	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/execution/backend/harness"
	"github.com/xyzbit/agentkiln/execution/backend/internal/app"
	"github.com/xyzbit/agentkiln/execution/backend/internal/dispatch"
	"github.com/xyzbit/agentkiln/execution/backend/internal/domain"
	"github.com/xyzbit/agentkiln/execution/backend/internal/executor"
	"github.com/xyzbit/agentkiln/execution/backend/internal/httpapi"
	"github.com/xyzbit/agentkiln/execution/backend/internal/modelquota"
	"github.com/xyzbit/agentkiln/execution/backend/internal/postgres"
	"github.com/xyzbit/agentkiln/execution/backend/internal/secret"
	"github.com/xyzbit/agentkiln/execution/backend/internal/sqlite"
)

func main() {
	if err := run(); err != nil {
		slog.Error("executiond stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	address := flag.String("addr", "127.0.0.1:8790", "HTTP listen address")
	databasePath := flag.String("db", "../../rank/var/execution.db", "SQLite path when EXECUTION_DATABASE_URL is unset")
	schema := flag.String("postgres-schema", "execution", "PostgreSQL schema owned by Execution and River")
	role := flag.String("role", "all", "process role: api, worker, or all; split roles require PostgreSQL")
	databaseTimeout := flag.Duration("database-timeout", 5*time.Second, "PostgreSQL acquisition, query and business transaction deadline; increase for explicit migrations")
	migrate := flag.Bool("migrate", false, "apply PostgreSQL/River migrations and exit")
	executorName := flag.String("executor", "local-process", "runtime backend: local-process or opensandbox")
	sandboxEndpoint := flag.String("sandbox-endpoint", os.Getenv("EXECUTION_SANDBOX_ENDPOINT"), "OpenSandbox lifecycle URL including /v1")
	sandboxScope := flag.String("sandbox-scope", "", "stable OpenSandbox ownership scope for this deployment")
	sandboxImage := flag.String("sandbox-image", "", "immutable image digest implementing the sandbox runtime")
	sandboxPool := flag.String("sandbox-pool", "", "shared durable capacity pool ID for this sandbox service")
	sandboxCapacityCPU := flag.String("sandbox-capacity-cpu", "", "exclusive CPU budget assigned to this Execution database on the sandbox host")
	sandboxCapacityMemory := flag.String("sandbox-capacity-memory", "", "exclusive memory budget assigned to this Execution database on the sandbox host")
	configurePool := flag.Bool("configure-sandbox-pool", false, "explicitly update the sandbox pool budget and exit; target changes require a drained pool")
	sandboxCPU := flag.String("sandbox-cpu", "500m", "CPU limit per sandbox")
	sandboxMemory := flag.String("sandbox-memory", "512Mi", "memory limit per sandbox")
	sandboxEgress := flag.String("sandbox-egress-policy", os.Getenv("EXECUTION_SANDBOX_EGRESS_POLICY"), "JSON transport allow list for sandbox egress; empty denies all new outbound connections")
	sandboxRequestTimeout := flag.Duration("sandbox-request-timeout", 10*time.Second, "sandbox lifecycle and file request timeout")
	maxArtifacts := flag.Int("max-artifacts", 128, "maximum retained files per remote invocation")
	workerBinary := flag.String("worker", os.Getenv("EXECUTION_WORKER_BIN"), "execution-worker executable path")
	repositoryRoot := flag.String("repo-root", os.Getenv("EXECUTION_REPO_ROOT"), "agentkiln repository root")
	artifactRoot := flag.String("artifacts", "../../rank/var/artifacts", "artifact store root")
	s3Endpoint := flag.String("s3-endpoint", os.Getenv("EXECUTION_S3_ENDPOINT"), "S3 host:port; unset keeps local artifacts")
	s3Bucket := flag.String("s3-bucket", os.Getenv("EXECUTION_S3_BUCKET"), "existing artifact bucket")
	s3Region := flag.String("s3-region", "us-east-1", "S3 signing region")
	s3TLS := flag.Bool("s3-tls", true, "use HTTPS for S3 requests")
	maxArtifactBytes := flag.Int64("max-artifact-bytes", 64<<20, "maximum size of one uploaded artifact")
	sandboxRoot := flag.String("sandboxes", "../../rank/var/sandboxes", "local process workspace root")
	credentialRoot := flag.String("credentials", "../../rank/var/credentials", "local development credential directory")
	workerTimeout := flag.Duration("worker-timeout", 10*time.Minute, "maximum harness invocation duration")
	cleanupGrace := flag.Duration("cleanup-grace", 5*time.Second, "runtime termination grace")
	persistenceTimeout := flag.Duration("persistence-timeout", 5*time.Second, "terminal state transaction deadline")
	poll := flag.Duration("poll-interval", 250*time.Millisecond, "queue, cancellation and event polling interval")
	rescueAfter := flag.Duration("rescue-after", 15*time.Minute, "River lost-worker recovery threshold")
	shutdownGrace := flag.Duration("shutdown-grace", 30*time.Second, "worker drain timeout before cancellation")
	queueTimeout := flag.Duration("queue-timeout", 5*time.Minute, "maximum wait before an execution starts")
	maxQueued := flag.Int("max-queued", 1000, "maximum queued executions")
	modelQuotaURL := flag.String("model-quota-url", os.Getenv("EXECUTION_MODEL_QUOTA_URL"), "runtime-reachable Execution /v1/model-permits URL")
	modelQuotaTimeout := flag.Duration("model-quota-timeout", 5*time.Second, "timeout for each runtime quota callback, within 1ms..1m")
	modelPermitValidity := flag.Duration("model-permit-validity", time.Second, "maximum delay between a quota grant and model dispatch, at most one second")
	configureModelQuota := flag.String("configure-model-quota", "", "JSON quota definition file to apply and exit; requires PostgreSQL")
	maxModelRequests := flag.Int("max-model-requests", 32, "maximum total DSH model calls per execution, including retries")
	maxModelConcurrent := flag.Int("max-model-concurrent", 8, "global concurrent invocations per model connection and model")
	maxTenantConcurrent := flag.Int("max-tenant-concurrent", 16, "global concurrent invocations per tenant")
	reconcileInterval := flag.Duration("reconcile-interval", time.Second, "terminal River delivery reconciliation interval")
	maxAttempts := flag.Int("max-attempts", 3, "maximum deliveries; does not authorize runtime replay")
	onlineSlots := flag.Int("online-slots", 2, "online invocation slots per worker")
	evaluationSlots := flag.Int("evaluation-slots", 2, "evaluation invocation slots per worker")
	workerVersion := flag.String("worker-version", os.Getenv("EXECUTION_WORKER_VERSION"), "immutable worker release identifier")
	hostname, _ := os.Hostname()
	workerID := flag.String("worker-id", fmt.Sprintf("%s-%d", hostname, os.Getpid()), "unique worker process identity")
	flag.Parse()
	if *executorName != "local-process" && *executorName != "opensandbox" {
		return errors.New("executor must be local-process or opensandbox")
	}
	if *role != "api" && *role != "worker" && *role != "all" {
		return errors.New("role must be api, worker, or all")
	}
	if *maxModelRequests < 1 || *maxModelRequests > contract.MaxModelRequestBudget || *queueTimeout < time.Millisecond || *workerTimeout <= 0 || *cleanupGrace <= 0 || *persistenceTimeout <= 0 || *poll <= 0 || *shutdownGrace <= 0 || *maxQueued < 1 || *maxAttempts < 1 || *maxArtifactBytes < 1 || *maxModelConcurrent < 1 || *maxTenantConcurrent < 1 || *reconcileInterval <= 0 || *onlineSlots < 0 || *evaluationSlots < 0 || *onlineSlots+*evaluationSlots == 0 {
		return errors.New("timeouts and queue bounds must be positive; slots must be non-negative with at least one queue")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	var store domain.Repository
	var pg *postgres.Store
	var credentials app.CredentialStore
	var err error
	token := os.Getenv("EXECUTION_SERVICE_TOKEN")
	if databaseURL := os.Getenv("EXECUTION_DATABASE_URL"); databaseURL != "" {
		pg, err = postgres.Open(ctx, postgres.Config{OperationTimeout: *databaseTimeout, URL: databaseURL, Schema: *schema, MaxQueued: *maxQueued, MaxAttempts: *maxAttempts, MaxModelConcurrent: *maxModelConcurrent, MaxTenantConcurrent: *maxTenantConcurrent})
		if err != nil {
			return err
		}
		defer pg.Close()
		if *migrate {
			return pg.Migrate(ctx)
		}
		if err = pg.CheckSchema(ctx); err != nil {
			return fmt.Errorf("execution migrations are required: %w", err)
		}
		if len(token) < 24 {
			return errors.New("PostgreSQL deployment requires EXECUTION_SERVICE_TOKEN with at least 24 characters")
		}
		key, decodeErr := base64.StdEncoding.DecodeString(os.Getenv("EXECUTION_CREDENTIAL_KEY"))
		if decodeErr != nil {
			return errors.New("EXECUTION_CREDENTIAL_KEY must be a base64-encoded 32-byte key")
		}
		credentials, err = secret.NewEncryptedStore(pg.Pool(), key)
		if err != nil {
			return err
		}
		store = pg
	} else {
		if *role != "all" || *migrate {
			return errors.New("split roles and migrations require EXECUTION_DATABASE_URL")
		}
		if err = os.MkdirAll(filepath.Dir(*databasePath), 0o700); err != nil {
			return err
		}
		store, err = sqlite.OpenWithQueueLimit(ctx, *databasePath, *maxQueued)
		if err != nil {
			return err
		}
		defer store.Close()
		credentials = secret.NewFileStore(*credentialRoot)
	}
	if *configureModelQuota != "" {
		if pg == nil {
			return errors.New("shared model quotas require PostgreSQL")
		}
		data, readErr := os.ReadFile(*configureModelQuota)
		if readErr != nil {
			return readErr
		}
		if len(data) > 65536 {
			return errors.New("model quota configuration is too large")
		}
		var quota contract.ModelQuota
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&quota) != nil || decoder.Decode(new(any)) != io.EOF {
			return errors.New("invalid model quota JSON")
		}
		return pg.ConfigureModelQuota(ctx, quota)
	}
	quotaAuthority, quotaErr := modelquota.NewAuthority(*modelQuotaURL, os.Getenv("EXECUTION_MODEL_QUOTA_KEY"), *modelPermitValidity, *modelQuotaTimeout)
	if quotaErr != nil {
		return quotaErr
	}
	if quotaAuthority != nil && pg == nil {
		return errors.New("shared model quotas require PostgreSQL")
	}
	if *executorName == "opensandbox" {
		capacity, parseErr := contract.ParseRuntimeResources(*sandboxCapacityCPU, *sandboxCapacityMemory)
		if parseErr != nil {
			return fmt.Errorf("sandbox host capacity: %w", parseErr)
		}
		resources, parseErr := contract.ParseRuntimeResources(*sandboxCPU, *sandboxMemory)
		if parseErr != nil {
			return parseErr
		}
		if !domain.FitsRuntime(capacity, contract.RuntimeResources{}, resources) {
			return errors.New("sandbox resource limits exceed its pool budget")
		}
		pool := domain.RuntimePool{ID: *sandboxPool, Endpoint: strings.TrimRight(*sandboxEndpoint, "/"), Scope: *sandboxScope, Capacity: capacity}
		if err = store.ConfigureRuntimePool(ctx, pool, *configurePool); err != nil {
			return err
		}
		if *configurePool {
			return nil
		}
	} else if *configurePool {
		return errors.New("sandbox pool configuration requires the opensandbox executor")
	}
	*artifactRoot, err = filepath.Abs(*artifactRoot)
	if err != nil {
		return err
	}
	*sandboxRoot, err = filepath.Abs(*sandboxRoot)
	if err != nil {
		return err
	}
	for _, path := range []string{*artifactRoot, *sandboxRoot} {
		if err = os.MkdirAll(path, 0o700); err != nil {
			return err
		}
	}
	harnesses, err := harness.DefaultRegistry(*repositoryRoot)
	if err != nil {
		return err
	}
	resolveConnection := func(ctx context.Context, id string) (contract.ModelConnection, string, error) {
		connection, err := store.GetModelConnection(ctx, id)
		if err != nil {
			return connection, "", err
		}
		if connection.Status != "verified" {
			return connection, "", errors.New("model connection is not verified")
		}
		credential, err := credentials.Get(ctx, connection.CredentialRef)
		return connection, credential, err
	}
	var artifactStore artifacts.Store
	if *s3Endpoint != "" {
		artifactCtx, done := context.WithTimeout(ctx, *persistenceTimeout)
		artifactStore, err = artifacts.NewS3(artifactCtx, artifacts.S3Config{Endpoint: *s3Endpoint, Bucket: *s3Bucket, Region: *s3Region, AccessKey: os.Getenv("EXECUTION_S3_ACCESS_KEY"), SecretKey: os.Getenv("EXECUTION_S3_SECRET_KEY"), Secure: *s3TLS})
		done()
		if err != nil {
			return err
		}
	}
	var runtimeExecutor domain.Executor
	runtimeExecutor = executor.NewLocal(executor.LocalConfig{ModelQuotaAuthority: quotaAuthority, Repository: store, WorkerBinary: *workerBinary, RepositoryRoot: *repositoryRoot, ArtifactRoot: *artifactRoot, SandboxRoot: *sandboxRoot, Timeout: *workerTimeout, TerminationGrace: *cleanupGrace, Harnesses: harnesses, ResolveModelConnection: resolveConnection, ArtifactStore: artifactStore, MaxArtifactBytes: *maxArtifactBytes})
	if *executorName == "opensandbox" {
		remoteStore := artifactStore
		if remoteStore == nil {
			remoteStore = artifacts.Local{Root: *artifactRoot}
		}
		egress, parseErr := contract.ParseEgressPolicy(*sandboxEgress)
		if parseErr != nil {
			return parseErr
		}
		runtimeExecutor, err = executor.NewOpenSandbox(executor.OpenSandboxConfig{ModelQuotaAuthority: quotaAuthority, Pool: *sandboxPool, Endpoint: *sandboxEndpoint, APIKey: os.Getenv("EXECUTION_SANDBOX_API_KEY"), Scope: *sandboxScope, Image: *sandboxImage, CPU: *sandboxCPU, Memory: *sandboxMemory, Egress: egress, RequestTimeout: *sandboxRequestTimeout, PollInterval: *poll, CleanupGrace: *cleanupGrace, MaxArtifactBytes: *maxArtifactBytes, MaxArtifacts: *maxArtifacts, Repository: store, ArtifactStore: remoteStore, ResolveModelConnection: resolveConnection})
		if err != nil {
			return err
		}
	}
	options := app.Options{MaxModelRequests: *maxModelRequests, Workers: pg == nil, WorkerVersion: *workerVersion, ArtifactRoot: *artifactRoot, ArtifactStore: artifactStore, Credentials: credentials, QueueTimeout: *queueTimeout, ExecutionTimeout: *workerTimeout, PollInterval: *poll, WorkerID: *workerID, PersistenceTimeout: *persistenceTimeout, CleanupGrace: *cleanupGrace, OnlineSlots: *onlineSlots, EvaluationSlots: *evaluationSlots}
	if pg != nil {
		options.ModelQuotas = pg
	}
	service := app.NewService(store, runtimeExecutor, options)
	var queue *river.Client[pgx.Tx]
	if pg != nil && *role != "api" {
		queue, err = dispatch.NewRiver(pg, runtimeExecutor, options, dispatch.Config{ID: *workerID, OnlineSlots: *onlineSlots, EvaluationSlots: *evaluationSlots, PollInterval: *poll, RescueAfter: *rescueAfter})
		if err != nil {
			return err
		}
		if err = queue.Start(context.Background()); err != nil {
			return err
		}
	} else if pg == nil {
		if err = service.ResumeActive(ctx); err != nil {
			return err
		}
	}
	reconcileDone := make(chan struct{})
	go func() {
		defer close(reconcileDone)
		if *role == "worker" {
			return
		}
		ticker := time.NewTicker(*reconcileInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pollCtx, done := context.WithTimeout(ctx, *persistenceTimeout)
				reconcileErr := service.ExpireQueued(pollCtx)
				if pg != nil {
					reconcileErr = errors.Join(reconcileErr, pg.Reconcile(pollCtx, time.Now().UTC()), pg.PruneModelPermits(pollCtx))
				}
				done()
				cleanupCtx, cleanupDone := context.WithTimeout(ctx, *cleanupGrace+*persistenceTimeout)
				reconcileErr = errors.Join(reconcileErr, service.ReconcileCleanup(cleanupCtx))
				cleanupDone()
				if reconcileErr != nil && ctx.Err() == nil {
					slog.Error("execution reconciliation failed", "error", reconcileErr)
				}
			}
		}
	}()
	var server *http.Server
	serverErrors := make(chan error, 1)
	if *role != "worker" {
		handler := httpapi.New(service, httpapi.Options{ControlToken: os.Getenv("RANK_CONTROL_TOKEN"), ServiceToken: token, CapacityToken: os.Getenv("EXECUTION_CAPACITY_TOKEN")})
		if quotaAuthority != nil {
			permits := quotaAuthority.Handler(pg)
			base := handler
			handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/model-permits" {
					permits.ServeHTTP(w, r)
				} else {
					base.ServeHTTP(w, r)
				}
			})
		}
		server = &http.Server{Addr: *address, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
		go func() { serverErrors <- server.ListenAndServe() }()
	}
	slog.Info("executiond started", "role", *role, "worker", *workerID, "postgres", pg != nil)
	select {
	case <-ctx.Done():
	case err = <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
	}
	cancel()
	<-reconcileDone
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), *shutdownGrace)
	defer shutdownCancel()
	serverStopped := make(chan struct{})
	go func() {
		defer close(serverStopped)
		if server != nil {
			_ = server.Shutdown(shutdownCtx)
		}
	}()
	if queue != nil {
		if stopErr := queue.Stop(shutdownCtx); stopErr != nil {
			forcedCtx, forcedCancel := context.WithTimeout(context.Background(), *cleanupGrace+*persistenceTimeout)
			defer forcedCancel()
			if cancelErr := queue.StopAndCancel(forcedCtx); cancelErr != nil {
				return cancelErr
			}
		}
	}
	if pg == nil {
		if stopErr := service.StopLocal(shutdownCtx); stopErr != nil {
			err = errors.Join(err, stopErr)
		}
	}
	<-serverStopped
	return err
}
