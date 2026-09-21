package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/xyzbit/agentkiln/execution/backend/artifacts"
	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/execution/backend/internal/domain"
	"github.com/xyzbit/agentkiln/execution/backend/internal/modelquota"
)

const artifactReadLimit = 1 << 20

type Error struct {
	Status  int
	Code    string
	Message string
}

func (err *Error) Error() string { return err.Message }

type Options struct {
	ModelQuotas        modelquota.Repository
	MaxModelRequests   int
	Workers            bool
	WorkerVersion      string
	ArtifactRoot       string
	ArtifactStore      artifacts.Store
	Clock              func() time.Time
	Credentials        CredentialStore
	Verifier           ConnectionVerifier
	QueueTimeout       time.Duration
	ExecutionTimeout   time.Duration
	PollInterval       time.Duration
	WorkerID           string
	PersistenceTimeout time.Duration
	CleanupGrace       time.Duration
	OnlineSlots        int
	EvaluationSlots    int
}

type Service struct {
	modelQuotas        modelquota.Repository
	maxModelRequests   int
	repo               domain.Repository
	executor           domain.Executor
	workers            bool
	workerVersion      string
	artifactStore      artifacts.Store
	now                func() time.Time
	credentials        CredentialStore
	verifier           ConnectionVerifier
	queueTimeout       time.Duration
	executionTimeout   time.Duration
	pollInterval       time.Duration
	workerID           string
	persistenceTimeout time.Duration
	cleanupGrace       time.Duration
	onlineSlots        int
	evaluationSlots    int
	local              *localWorkers
	mu                 sync.Mutex
	discoveryMu        sync.Mutex
	cancels            map[string]context.CancelFunc
	subscriptions      map[string]map[chan struct{}]struct{}
}

func NewService(repo domain.Repository, executor domain.Executor, options Options) *Service {
	if options.MaxModelRequests <= 0 {
		options.MaxModelRequests = 32
	}
	if options.Clock == nil {
		options.Clock = time.Now
	}
	if options.ArtifactStore == nil {
		options.ArtifactStore = artifacts.Local{Root: options.ArtifactRoot}
	}
	if options.WorkerVersion == "" {
		options.WorkerVersion = "dev"
	}
	if options.Verifier == nil {
		options.Verifier = HTTPConnectionVerifier{}
	}
	if options.QueueTimeout <= 0 {
		options.QueueTimeout = 5 * time.Minute
	}
	if options.ExecutionTimeout <= 0 {
		options.ExecutionTimeout = 10 * time.Minute
	}
	if options.PollInterval <= 0 {
		options.PollInterval = 250 * time.Millisecond
	}
	if options.PersistenceTimeout <= 0 {
		options.PersistenceTimeout = 5 * time.Second
	}
	if options.CleanupGrace <= 0 {
		options.CleanupGrace = 5 * time.Second
	}
	service := &Service{repo: repo, executor: executor, workers: options.Workers, workerVersion: options.WorkerVersion, artifactStore: options.ArtifactStore, now: options.Clock, credentials: options.Credentials, verifier: options.Verifier, queueTimeout: options.QueueTimeout, executionTimeout: options.ExecutionTimeout, pollInterval: options.PollInterval, workerID: options.WorkerID, persistenceTimeout: options.PersistenceTimeout, cleanupGrace: options.CleanupGrace, cancels: map[string]context.CancelFunc{}, subscriptions: map[string]map[chan struct{}]struct{}{}}
	if options.OnlineSlots == 0 && options.EvaluationSlots == 0 {
		options.OnlineSlots, options.EvaluationSlots = 2, 2
	}
	service.onlineSlots, service.evaluationSlots = options.OnlineSlots, options.EvaluationSlots
	service.maxModelRequests = options.MaxModelRequests
	service.modelQuotas = options.ModelQuotas
	if options.Workers {
		service.local = newLocalWorkers(options.OnlineSlots, options.EvaluationSlots)
	}
	return service
}

func (s *Service) Submit(ctx context.Context, input contract.SubmitRequest) (contract.Execution, error) {
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.IdempotencyKey == "" {
		return contract.Execution{}, problem(400, "idempotency_key_required", "idempotencyKey is required")
	}
	if err := validateSpec(input.Spec); err != nil {
		return contract.Execution{}, err
	}
	// Compare the same JSON representation that repositories persist: omitted
	// empty collections and JSON numbers must survive idempotent submission.
	encoded, err := json.Marshal(input.Spec)
	if err != nil {
		return contract.Execution{}, problem(400, "invalid_spec", "execution spec must contain JSON values")
	}
	var persisted contract.Spec
	if err := json.Unmarshal(encoded, &persisted); err != nil {
		return contract.Execution{}, err
	}
	input.Spec = persisted
	existing, err := s.repo.GetByIdempotencyKey(ctx, input.IdempotencyKey)
	if err == nil {
		return replay(existing, input.Spec)
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return contract.Execution{}, err
	}
	if len(input.Spec.Messages) > 0 && !s.executor.Probe(ctx, input.Spec.Harness).StructuredHistory {
		return contract.Execution{}, problem(400, "unsupported_history", "this harness does not support structured message history")
	}
	modelBudget := 0
	if s.executor.Probe(ctx, input.Spec.Harness).ModelRequestBudget {
		modelBudget = s.maxModelRequests
		if input.Spec.MaxModelRequests > 0 {
			modelBudget = min(modelBudget, input.Spec.MaxModelRequests)
		}
	} else if input.Spec.MaxModelRequests > 0 {
		return contract.Execution{}, problem(400, "unsupported_model_budget", "this harness cannot enforce a model request budget")
	}
	now := s.now().UTC()
	timeout := s.executionTimeout
	if input.Spec.TimeoutMs > 0 {
		timeout = min(timeout, time.Duration(input.Spec.TimeoutMs)*time.Millisecond)
	}
	queueTimeout := s.queueTimeout
	if input.Spec.QueueTimeoutMs > 0 {
		queueTimeout = min(queueTimeout, time.Duration(input.Spec.QueueTimeoutMs)*time.Millisecond)
	}
	execution := contract.Execution{QueueExpiresAt: now.Add(queueTimeout).Truncate(time.Millisecond), ID: newID("exec"), IdempotencyKey: input.IdempotencyKey, Status: contract.StatusQueued, Executor: s.executor.Name(), Attempt: 0, WorkerVersion: s.workerVersion, Spec: input.Spec, CreatedAt: now, RuntimeTimeoutMs: timeout.Milliseconds(), CleanupGraceMs: s.cleanupGrace.Milliseconds()}
	execution.MaxModelRequests = modelBudget
	if s.modelQuotas != nil && input.Spec.ModelConnectionID != "" {
		execution.ModelQuota, err = s.modelQuotas.ModelQuotaForConnection(ctx, input.Spec.ModelConnectionID)
		if err != nil {
			return contract.Execution{}, err
		}
		if execution.ModelQuota != nil && !s.executor.Probe(ctx, input.Spec.Harness).ModelQuota {
			return contract.Execution{}, problem(400, "unsupported_model_quota", "this harness cannot enforce the connection provider quota")
		}
	}
	if planner, ok := s.executor.(domain.RuntimePlanner); ok {
		plan := planner.Plan()
		execution.RuntimePlan = &plan
	}
	queued := contract.Event{ExecutionID: execution.ID, Attempt: 0, Type: "execution.queued", Status: contract.StatusQueued, At: now}
	if err = s.repo.Create(ctx, execution, queued); errors.Is(err, domain.ErrConflict) {
		existing, loadErr := s.repo.GetByIdempotencyKey(ctx, input.IdempotencyKey)
		if loadErr != nil {
			return contract.Execution{}, loadErr
		}
		return replay(existing, input.Spec)
	} else if errors.Is(err, modelquota.ErrConflict) {
		return contract.Execution{}, problem(409, "model_quota_changed", "model quota configuration changed; submit again with the same idempotency key")
	} else if errors.Is(err, domain.ErrRuntimePlan) {
		return contract.Execution{}, problem(503, "runtime_pool_unavailable", "runtime pool cannot admit this configuration")
	} else if errors.Is(err, domain.ErrOverloaded) {
		return contract.Execution{}, problem(429, "execution_overloaded", "execution queue is full; retry later")
	} else if err != nil {
		return contract.Execution{}, err
	}
	s.publish(execution.ID)
	if s.workers {
		s.dispatch(execution.ID)
	}
	return s.repo.Get(ctx, execution.ID)
}

func (s *Service) Get(ctx context.Context, id string) (contract.Execution, error) {
	value, err := s.repo.Get(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		return value, problem(404, "execution_not_found", "execution not found")
	}
	return value, err
}

func (s *Service) Probe(ctx context.Context, harness string) contract.Availability {
	return s.executor.Probe(ctx, harness)
}

// Events returns durable lifecycle events after one per-execution sequence.
func (s *Service) Events(ctx context.Context, id string, afterSequence int64) ([]contract.Event, error) {
	if _, err := s.Get(ctx, id); err != nil {
		return nil, err
	}
	return s.repo.ListEvents(ctx, id, afterSequence)
}

// WaitEvents blocks until a lifecycle event is available, the execution is
// terminal, the heartbeat interval expires, or the caller cancels.
func (s *Service) WaitEvents(ctx context.Context, id string, afterSequence int64, heartbeat time.Duration) ([]contract.Event, bool, error) {
	if heartbeat <= 0 {
		heartbeat = 15 * time.Second
	}
	wakeup := make(chan struct{}, 1)
	deadline := time.Now().Add(heartbeat)
	s.subscribe(id, wakeup)
	defer s.unsubscribe(id, wakeup)
	for {
		events, err := s.Events(ctx, id, afterSequence)
		if err != nil || len(events) > 0 {
			return events, false, err
		}
		execution, err := s.Get(ctx, id)
		if err != nil {
			return nil, false, err
		}
		if contract.IsTerminal(execution.Status) {
			// Completion can commit between the empty event read and status
			// read. Drain its events before acknowledging the end of the stream.
			events, err = s.Events(ctx, id, afterSequence)
			return events, len(events) == 0, err
		}
		timer := time.NewTimer(min(s.pollInterval, time.Until(deadline)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, false, ctx.Err()
		case <-wakeup:
			timer.Stop()
		case <-timer.C:
			if !time.Now().Before(deadline) {
				return nil, false, nil
			}
		}
	}
}

func (s *Service) Cancel(ctx context.Context, id string) (contract.Execution, error) {
	now := s.now().UTC()
	execution, err := s.Get(ctx, id)
	if err != nil {
		return contract.Execution{}, err
	}
	if contract.IsTerminal(execution.Status) {
		return execution, nil
	}
	if err := s.repo.RequestCancel(ctx, id, now); err != nil && !errors.Is(err, domain.ErrConflict) {
		return contract.Execution{}, err
	}
	s.publish(id)
	s.mu.Lock()
	if cancel := s.cancels[id]; cancel != nil {
		cancel()
	}
	s.mu.Unlock()
	return s.Get(ctx, id)
}

func (s *Service) ResumeActive(ctx context.Context) error {
	if err := s.ExpireQueued(ctx); err != nil {
		return err
	}
	executions, err := s.repo.ListActive(ctx)
	if err != nil {
		return err
	}
	for _, execution := range executions {
		if execution.Status != contract.StatusQueued {
			event := contract.Event{ExecutionID: execution.ID, Attempt: execution.Attempt, Type: "execution.failed", Status: contract.StatusFailed, At: s.now().UTC()}
			if err := s.repo.Fail(ctx, execution.ID, "execution_outcome_unknown: local owner was lost; invocation was not replayed", event.At, event); err != nil && !errors.Is(err, domain.ErrConflict) {
				return err
			}
			continue
		}
		if s.workers {
			s.dispatch(execution.ID)
		}
	}
	return nil
}

// Execute handles one delivery. Runtime failures become durable terminal states;
// persistence failures return to the queue without authorizing a runtime replay.
func (s *Service) Execute(ctx context.Context, id string) error {
	execution, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if contract.IsTerminal(execution.Status) {
		return nil
	}
	if execution.Status != contract.StatusQueued {
		if execution.StartedAt != nil && s.now().Before(execution.StartedAt.Add(time.Duration(execution.RuntimeTimeoutMs+execution.CleanupGraceMs)*time.Millisecond)) {
			return domain.ErrInProgress
		}
		event := contract.Event{ExecutionID: id, Attempt: execution.Attempt, Type: "execution.failed", Status: contract.StatusFailed, At: s.now().UTC()}
		return s.repo.Fail(ctx, id, "execution_outcome_unknown: previous delivery lost its owner; invocation was not replayed", event.At, event)
	}
	if execution.QueueExpiresAt.IsZero() || !s.now().Before(execution.QueueExpiresAt) {
		return s.expireDelivery(ctx, id)
	}
	if execution.Executor != s.executor.Name() {
		return domain.ErrExecutorUnavailable
	}
	if planner, ok := s.executor.(domain.RuntimePlanner); ok {
		if execution.RuntimePlan == nil {
			event := contract.Event{ExecutionID: id, Attempt: execution.Attempt, Type: "execution.failed", Status: contract.StatusFailed, At: s.now().UTC()}
			return s.repo.Fail(ctx, id, "execution_runtime_plan_missing: submit a new execution with resolved resources", event.At, event)
		}
		if !planner.SupportsPlan(execution.RuntimePlan) {
			return domain.ErrExecutorUnavailable
		}
	}
	if execution.RuntimeTimeoutMs <= 0 || execution.CleanupGraceMs <= 0 {
		event := contract.Event{ExecutionID: id, Attempt: execution.Attempt, Type: "execution.failed", Status: contract.StatusFailed, At: s.now().UTC()}
		return s.repo.Fail(ctx, id, "execution_limits_missing: submit a new execution with resolved runtime limits", event.At, event)
	}
	if execution.ModelQuota != nil && !s.executor.Probe(ctx, execution.Spec.Harness).ModelQuota {
		return domain.ErrExecutorUnavailable
	}
	budgetSupported := s.executor.Probe(ctx, execution.Spec.Harness).ModelRequestBudget
	if execution.MaxModelRequests > 0 && !budgetSupported {
		return domain.ErrExecutorUnavailable
	}
	if budgetSupported && execution.MaxModelRequests <= 0 {
		event := contract.Event{ExecutionID: id, Attempt: execution.Attempt, Type: "execution.failed", Status: contract.StatusFailed, At: s.now().UTC()}
		return s.repo.Fail(ctx, id, "execution_model_budget_missing: submit a new execution with a resolved model budget", event.At, event)
	}
	if err := contract.ValidateModelSelection(execution.Spec); err != nil {
		event := contract.Event{ExecutionID: id, Attempt: execution.Attempt, Type: "execution.failed", Status: contract.StatusFailed, At: s.now().UTC()}
		return s.repo.Fail(ctx, id, "execution_model_unresolved: "+err.Error(), event.At, event)
	}
	started := s.now().UTC()
	attempt := execution.Attempt + 1
	running := contract.Event{ExecutionID: id, Attempt: attempt, Type: "execution.running", Status: contract.StatusRunning, At: started}
	if err = s.repo.MarkRunning(ctx, id, attempt, s.executor.Name(), s.workerID, s.workerVersion, started, running); err != nil {
		if errors.Is(err, domain.ErrQueueExpired) {
			return s.expireDelivery(ctx, id)
		}
		if errors.Is(err, domain.ErrConflict) {
			return domain.ErrInProgress
		}
		return err
	}
	s.publish(id)
	timeout := min(s.executionTimeout, time.Duration(execution.RuntimeTimeoutMs)*time.Millisecond)
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	monitorDone := make(chan struct{})
	stopMonitor := make(chan struct{})
	go func() {
		defer close(monitorDone)
		ticker := time.NewTicker(s.pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stopMonitor:
				return
			case <-runCtx.Done():
				return
			case <-ticker.C:
				current, err := s.repo.Get(runCtx, id)
				if err != nil || current.Status != contract.StatusRunning || current.Attempt != attempt {
					cancel()
					return
				}
			}
		}
	}()
	emit := func(event domain.ExecutorEvent) error {
		at := event.At
		if at.IsZero() {
			at = s.now().UTC()
		}
		progress := contract.Event{ExecutionID: id, Attempt: attempt, Type: event.Type, Status: contract.StatusRunning, Message: event.Message, Data: event.Data, At: at}
		if err := s.repo.AppendEvent(runCtx, progress); err != nil {
			cancel()
			return err
		}
		s.publish(id)
		return nil
	}
	execution.Attempt, execution.StartedAt = attempt, &started
	execution.Status, execution.Executor, execution.ExternalHandle = contract.StatusRunning, s.executor.Name(), s.workerID
	execution.RuntimeCleanupPending = true
	result, runErr := s.executor.Run(runCtx, execution, emit)
	close(stopMonitor)
	<-monitorDone
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), s.persistenceTimeout)
	defer finishCancel()
	if !errors.Is(runErr, domain.ErrRuntimeStopUnconfirmed) {
		if err := s.repo.ConfirmRuntimeStopped(finishCtx, id, attempt); err != nil {
			return err
		}
	}
	current, err := s.repo.Get(finishCtx, id)
	if err != nil {
		return err
	}
	if current.Attempt != attempt || contract.IsTerminal(current.Status) {
		return nil
	}
	completed := s.now().UTC()
	if current.Status == contract.StatusCancelling && !errors.Is(runErr, domain.ErrRuntimeStopUnconfirmed) {
		event := contract.Event{ExecutionID: id, Attempt: attempt, Type: "execution.cancelled", Status: contract.StatusCancelled, At: completed}
		err = s.repo.Cancel(finishCtx, id, completed, event)
		s.publish(id)
		return err
	}
	if runErr == nil && runCtx.Err() != nil {
		runErr = runCtx.Err()
	}
	if runErr != nil {
		failed := contract.Event{ExecutionID: id, Attempt: attempt, Type: "execution.failed", Status: contract.StatusFailed, Message: runErr.Error(), At: completed}
		err = s.repo.Fail(finishCtx, id, runErr.Error(), completed, failed)
		s.publish(id)
		return err
	}
	complete := contract.Event{ExecutionID: id, Attempt: attempt, Type: "execution.completed", Status: contract.StatusCompleted, At: completed}
	if err = s.repo.Complete(finishCtx, id, result, completed, complete); errors.Is(err, domain.ErrConflict) {
		current, loadErr := s.repo.Get(finishCtx, id)
		if loadErr != nil {
			return loadErr
		}
		if current.Status == contract.StatusCancelling && current.Attempt == attempt {
			complete.Type, complete.Status = "execution.cancelled", contract.StatusCancelled
			return s.repo.Cancel(finishCtx, id, completed, complete)
		}
		return nil
	}
	s.publish(id)
	return err
}

func (s *Service) subscribe(id string, wakeup chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.subscriptions[id] == nil {
		s.subscriptions[id] = map[chan struct{}]struct{}{}
	}
	s.subscriptions[id][wakeup] = struct{}{}
}

func (s *Service) unsubscribe(id string, wakeup chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.subscriptions[id], wakeup)
	if len(s.subscriptions[id]) == 0 {
		delete(s.subscriptions, id)
	}
}

func (s *Service) publish(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for wakeup := range s.subscriptions[id] {
		select {
		case wakeup <- struct{}{}:
		default:
		}
	}
}

func (s *Service) ReadArtifact(ctx context.Context, executionID, artifactPath string) (contract.ArtifactContent, error) {
	execution, err := s.Get(ctx, executionID)
	if err != nil {
		return contract.ArtifactContent{}, err
	}
	if execution.Result == nil {
		return contract.ArtifactContent{}, problem(404, "artifact_not_found", "artifact not found")
	}
	authorized := false
	for _, artifact := range execution.Result.Artifacts {
		if artifact.Path == artifactPath && artifact.ExecutionID == executionID {
			authorized = true
			break
		}
	}
	if !authorized {
		return contract.ArtifactContent{}, problem(404, "artifact_not_found", "artifact not found")
	}
	file, err := s.artifactStore.Open(ctx, artifactPath)
	if err != nil {
		return contract.ArtifactContent{}, err
	}
	defer file.Close()
	buffer, err := io.ReadAll(io.LimitReader(file, artifactReadLimit+1))
	if err != nil {
		return contract.ArtifactContent{}, err
	}
	truncated := len(buffer) > artifactReadLimit
	if truncated {
		buffer = buffer[:artifactReadLimit]
	}
	content, encoding := string(buffer), "utf-8"
	if !utf8.Valid(buffer) {
		content, encoding = base64.StdEncoding.EncodeToString(buffer), "base64"
	}
	return contract.ArtifactContent{Path: artifactPath, Content: content, Encoding: encoding, Truncated: truncated}, nil
}

func validateSpec(spec contract.Spec) error {
	if spec.MaxModelRequests < 0 || spec.MaxModelRequests > contract.MaxModelRequestBudget {
		return problem(400, "invalid_model_budget", "maxModelRequests must be a non-negative signed 32-bit integer")
	}
	if spec.QueueTimeoutMs < 0 || spec.QueueTimeoutMs > int64((time.Duration(1<<63-1))/time.Millisecond) {
		return problem(400, "invalid_queue_timeout", "queueTimeoutMs must be a non-negative duration in milliseconds")
	}
	if spec.TimeoutMs < 0 || spec.TimeoutMs > int64((time.Duration(1<<63-1))/time.Millisecond) {
		return problem(400, "invalid_timeout", "timeoutMs must be a non-negative duration in milliseconds")
	}
	if spec.ProtocolVersion != contract.ProtocolVersion {
		return problem(400, "unsupported_protocol", fmt.Sprintf("protocolVersion must be %d", contract.ProtocolVersion))
	}
	if spec.Kind != contract.KindAgent && spec.Kind != contract.KindJudge {
		return problem(400, "invalid_kind", "kind must be agent or judge")
	}
	if strings.TrimSpace(spec.Harness) == "" {
		return problem(400, "invalid_spec", "harness is required")
	}
	if err := contract.ValidateInput(spec); err != nil {
		return problem(400, "invalid_input", err.Error())
	}
	if spec.Kind == contract.KindJudge && strings.TrimSpace(spec.CandidateOutput) == "" {
		return problem(400, "invalid_judge_spec", "candidateOutput is required for judge executions")
	}
	if spec.Kind == contract.KindJudge && spec.StructuredOutput != nil {
		return problem(400, "invalid_judge_spec", "structuredOutput is available only for agent executions")
	}
	if err := contract.ValidateModelSelection(spec); err != nil {
		return problem(400, "invalid_model_selection", err.Error())
	}
	if err := contract.ValidateStructuredOutput(spec.StructuredOutput); err != nil {
		return problem(400, "invalid_structured_output", err.Error())
	}
	if err := contract.ValidateInputAttachments(spec.InputAttachments); err != nil {
		return problem(400, "invalid_input_attachments", err.Error())
	}
	return nil
}

func newID(prefix string) string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		panic(err)
	}
	return prefix + "-" + hex.EncodeToString(value)
}

func problem(status int, code, message string) error {
	return &Error{Status: status, Code: code, Message: message}
}
