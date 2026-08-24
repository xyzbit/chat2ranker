package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/xyzbit/chat2ranker/execution/backend/contract"
	"github.com/xyzbit/chat2ranker/execution/backend/internal/domain"
)

const artifactReadLimit = 1 << 20

type Error struct {
	Status  int
	Code    string
	Message string
}

func (err *Error) Error() string { return err.Message }

type Options struct {
	Workers       bool
	WorkerVersion string
	ArtifactRoot  string
	Clock         func() time.Time
}

type Service struct {
	repo          domain.Repository
	executor      domain.Executor
	workers       bool
	workerVersion string
	artifactRoot  string
	now           func() time.Time
	mu            sync.Mutex
	cancels       map[string]context.CancelFunc
	subscriptions map[string]map[chan struct{}]struct{}
}

func NewService(repo domain.Repository, executor domain.Executor, options Options) *Service {
	if options.Clock == nil {
		options.Clock = time.Now
	}
	if options.WorkerVersion == "" {
		options.WorkerVersion = "dev"
	}
	return &Service{repo: repo, executor: executor, workers: options.Workers, workerVersion: options.WorkerVersion, artifactRoot: options.ArtifactRoot, now: options.Clock, cancels: map[string]context.CancelFunc{}, subscriptions: map[string]map[chan struct{}]struct{}{}}
}

func (s *Service) Submit(ctx context.Context, input contract.SubmitRequest) (contract.Execution, error) {
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.IdempotencyKey == "" {
		return contract.Execution{}, problem(400, "idempotency_key_required", "idempotencyKey is required")
	}
	if err := validateSpec(input.Spec); err != nil {
		return contract.Execution{}, err
	}
	existing, err := s.repo.GetByIdempotencyKey(ctx, input.IdempotencyKey)
	if err == nil {
		if !reflect.DeepEqual(existing.Spec, input.Spec) {
			return contract.Execution{}, problem(409, "idempotency_conflict", "idempotencyKey was already used with another spec")
		}
		return existing, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return contract.Execution{}, err
	}
	now := s.now().UTC()
	execution := contract.Execution{ID: newID("exec"), IdempotencyKey: input.IdempotencyKey, Status: contract.StatusQueued, Executor: s.executor.Name(), Attempt: 0, WorkerVersion: s.workerVersion, Spec: input.Spec, CreatedAt: now}
	queued := contract.Event{ExecutionID: execution.ID, Attempt: 0, Type: "execution.queued", Status: contract.StatusQueued, At: now}
	if err = s.repo.Create(ctx, execution, queued); errors.Is(err, domain.ErrConflict) {
		existing, loadErr := s.repo.GetByIdempotencyKey(ctx, input.IdempotencyKey)
		if loadErr != nil {
			return contract.Execution{}, loadErr
		}
		if !reflect.DeepEqual(existing.Spec, input.Spec) {
			return contract.Execution{}, problem(409, "idempotency_conflict", "idempotencyKey was already used with another spec")
		}
		return existing, nil
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
			return nil, true, nil
		}
		timer := time.NewTimer(heartbeat)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, false, ctx.Err()
		case <-wakeup:
			timer.Stop()
		case <-timer.C:
			return nil, false, nil
		}
	}
}

func (s *Service) Cancel(ctx context.Context, id string) (contract.Execution, error) {
	now := s.now().UTC()
	execution, err := s.Get(ctx, id)
	if err != nil {
		return contract.Execution{}, err
	}
	event := contract.Event{ExecutionID: id, Attempt: execution.Attempt, Type: "execution.cancelled", Status: contract.StatusCancelled, At: now}
	if err := s.repo.Cancel(ctx, id, now, event); err != nil && !errors.Is(err, domain.ErrConflict) {
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
	executions, err := s.repo.ListActive(ctx)
	if err != nil {
		return err
	}
	for _, execution := range executions {
		event := contract.Event{ExecutionID: execution.ID, Attempt: execution.Attempt, Type: "execution.requeued", Status: contract.StatusQueued, Message: "executiond resumed an unfinished local execution", At: s.now().UTC()}
		if err = s.repo.Requeue(ctx, execution.ID, event); err != nil {
			return err
		}
		s.publish(execution.ID)
		if s.workers {
			s.dispatch(execution.ID)
		}
	}
	return nil
}

func (s *Service) dispatch(id string) {
	s.mu.Lock()
	if _, exists := s.cancels[id]; exists {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancels[id] = cancel
	s.mu.Unlock()
	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.cancels, id)
			s.mu.Unlock()
		}()
		_ = s.execute(ctx, id)
	}()
}

func (s *Service) execute(ctx context.Context, id string) error {
	execution, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	started := s.now().UTC()
	attempt := execution.Attempt + 1
	running := contract.Event{ExecutionID: id, Attempt: attempt, Type: "execution.running", Status: contract.StatusRunning, At: started}
	if err = s.repo.MarkRunning(ctx, id, attempt, s.executor.Name(), "", started, running); err != nil {
		return err
	}
	s.publish(id)
	emit := func(event domain.ExecutorEvent) error {
		at := event.At
		if at.IsZero() {
			at = s.now().UTC()
		}
		progress := contract.Event{ExecutionID: id, Attempt: attempt, Type: event.Type, Status: contract.StatusRunning, Message: event.Message, Data: event.Data, At: at}
		if err := s.repo.AppendEvent(ctx, progress); err != nil {
			return err
		}
		s.publish(id)
		return nil
	}
	result, runErr := s.executor.Run(ctx, id, execution.Spec, emit)
	completed := s.now().UTC()
	if runErr != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil
		}
		failed := contract.Event{ExecutionID: id, Attempt: attempt, Type: "execution.failed", Status: contract.StatusFailed, Message: runErr.Error(), At: completed}
		err = s.repo.Fail(context.Background(), id, runErr.Error(), completed, failed)
		s.publish(id)
		return err
	}
	complete := contract.Event{ExecutionID: id, Attempt: attempt, Type: "execution.completed", Status: contract.StatusCompleted, At: completed}
	if err = s.repo.Complete(context.Background(), id, result, completed, complete); errors.Is(err, domain.ErrConflict) {
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
	root, err := filepath.EvalSymlinks(s.artifactRoot)
	if err != nil {
		return contract.ArtifactContent{}, err
	}
	target, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(artifactPath)))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return contract.ArtifactContent{}, problem(404, "artifact_not_found", "artifact not found")
		}
		return contract.ArtifactContent{}, err
	}
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return contract.ArtifactContent{}, problem(403, "artifact_path_rejected", "artifact path escaped its store")
	}
	file, err := os.Open(target)
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
	return contract.ArtifactContent{Path: artifactPath, Content: string(buffer), Truncated: truncated}, nil
}

func validateSpec(spec contract.Spec) error {
	if spec.ProtocolVersion != contract.ProtocolVersion {
		return problem(400, "unsupported_protocol", fmt.Sprintf("protocolVersion must be %d", contract.ProtocolVersion))
	}
	if spec.Kind != contract.KindAgent && spec.Kind != contract.KindJudge {
		return problem(400, "invalid_kind", "kind must be agent or judge")
	}
	if strings.TrimSpace(spec.Harness) == "" || strings.TrimSpace(spec.Prompt) == "" {
		return problem(400, "invalid_spec", "harness and prompt are required")
	}
	if spec.Kind == contract.KindJudge && strings.TrimSpace(spec.CandidateOutput) == "" {
		return problem(400, "invalid_judge_spec", "candidateOutput is required for judge executions")
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
