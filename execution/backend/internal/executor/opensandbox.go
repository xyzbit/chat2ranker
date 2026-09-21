package executor

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	sandbox "github.com/alibaba/OpenSandbox/sdks/sandbox/go"
	"github.com/xyzbit/agentkiln/execution/backend/artifacts"
	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/execution/backend/internal/domain"
	"github.com/xyzbit/agentkiln/execution/backend/internal/modelquota"
	"github.com/xyzbit/agentkiln/execution/backend/internal/sandboxruntime"
	"github.com/xyzbit/agentkiln/execution/backend/internal/workerprotocol"
)

// OpenSandboxConfig selects one managed runtime, with no local fallback. Image
// must implement the protected execution-sandbox init and include the DSH worker.
// The server deployment must not add host mounts or unsolicited sidecars.
type OpenSandboxConfig struct {
	ModelQuotaAuthority                        *modelquota.Authority
	Endpoint, APIKey, Scope, Image, Pool       string
	CPU, Memory                                string
	Egress                                     contract.EgressPolicy
	RequestTimeout, PollInterval, CleanupGrace time.Duration
	MaxArtifactBytes                           int64
	MaxArtifacts                               int
	Repository                                 domain.Repository
	ArtifactStore                              artifacts.Store
	ResolveModelConnection                     func(context.Context, string) (contract.ModelConnection, string, error)
}

type OpenSandbox struct {
	config    OpenSandboxConfig
	client    *sandbox.LifecycleClient
	resources contract.RuntimeResources
}

func NewOpenSandbox(config OpenSandboxConfig) (*OpenSandbox, error) {
	endpoint, err := url.Parse(config.Endpoint)
	if err != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, errors.New("sandbox endpoint must be an HTTP(S) service URL without userinfo, query or fragment")
	}
	config.Endpoint = strings.TrimRight(config.Endpoint, "/")
	if !strings.HasSuffix(config.Endpoint, "/v1") || config.APIKey == "" || config.Scope == "" || !regexp.MustCompile(`^(?:[^\s@]+@)?sha256:[a-f0-9]{64}$`).MatchString(config.Image) {
		return nil, errors.New("sandbox requires a /v1 endpoint, private key, scope and immutable image digest")
	}
	if config.RequestTimeout <= 0 || config.PollInterval <= 0 || config.CleanupGrace <= 0 || config.MaxArtifactBytes < 1 || config.MaxArtifacts < 1 || config.Repository == nil || config.ArtifactStore == nil || config.CPU == "" || config.Memory == "" {
		return nil, errors.New("sandbox requires persistence, artifacts, resource limits and positive timeouts/byte bounds")
	}
	resources, err := contract.ParseRuntimeResources(config.CPU, config.Memory)
	if err != nil {
		return nil, err
	}
	config.Egress, err = config.Egress.Normalize()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(config.Pool) == "" {
		return nil, errors.New("sandbox requires a configured resource pool")
	}
	client := &http.Client{Timeout: config.RequestTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &OpenSandbox{config: config, resources: resources, client: sandbox.NewLifecycleClient(config.Endpoint, config.APIKey, sandbox.WithHTTPClient(client), sandbox.WithRetry(sandbox.RetryConfig{}))}, nil
}

func (e *OpenSandbox) Plan() contract.RuntimePlan {
	policy := contract.EgressPolicy{Allow: slices.Clone(e.config.Egress.Allow)}
	return contract.RuntimePlan{Pool: e.config.Pool, Endpoint: e.config.Endpoint, Scope: e.config.Scope, Image: e.config.Image, Resources: e.resources, Egress: policy}
}
func (e *OpenSandbox) SupportsPlan(plan *contract.RuntimePlan) bool {
	return plan != nil && plan.Pool == e.config.Pool && plan.Endpoint == e.config.Endpoint && plan.Scope == e.config.Scope && plan.Image == e.config.Image && plan.Resources.CPUMillis > 0 && plan.Resources.MemoryBytes > 0 && slices.Equal(plan.Egress.Allow, e.config.Egress.Allow)
}

func (*OpenSandbox) Name() string { return "opensandbox" }
func (e *OpenSandbox) Probe(_ context.Context, name string) contract.Availability {
	supported := name == "dsh" || name == "mock"
	return contract.Availability{ModelQuota: name == "dsh" && e.config.ModelQuotaAuthority != nil, ModelRequestBudget: name == "dsh", Available: supported, Installed: supported, Configured: supported, StructuredHistory: supported, Label: "OpenSandbox " + name, Reason: map[bool]string{false: "this sandbox image supports DSH and the deterministic fixture only"}[supported]}
}

func (e *OpenSandbox) Run(ctx context.Context, value contract.Execution, emit domain.EventSink) (result contract.Result, runErr error) {
	if !e.SupportsPlan(value.RuntimePlan) || value.StartedAt == nil || value.Attempt < 1 || value.RuntimeTimeoutMs <= 0 || value.CleanupGraceMs <= 0 || !e.Probe(ctx, value.Spec.Harness).Available {
		return result, errors.New("sandbox execution requires a started attempt, frozen limits and a supported harness")
	}
	request := workerprotocol.Request{MaxModelRequests: value.MaxModelRequests, ProtocolVersion: workerprotocol.Version, ExecutionID: value.ID, Spec: value.Spec, WorkspaceDir: sandboxruntime.Root + "/workspace", ArtifactDir: sandboxruntime.Root + "/artifacts/" + value.ID, HarnessHome: sandboxruntime.Root + "/artifacts/" + value.ID + "/harness-home", Metadata: value.Spec.Metadata}
	if value.Spec.ModelConnectionID != "" {
		if e.config.ResolveModelConnection == nil {
			return result, errors.New("sandbox model resolver is required")
		}
		connection, credential, err := e.config.ResolveModelConnection(ctx, value.Spec.ModelConnectionID)
		if err != nil {
			return result, err
		}
		request.ModelConnection, request.Credential = &connection, credential
	}
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return result, err
	}
	daemonToken := hex.EncodeToString(token[:])
	expires := value.StartedAt.Add(time.Duration(value.RuntimeTimeoutMs+value.CleanupGraceMs) * time.Millisecond)
	allocation := contract.RuntimeAllocation{Endpoint: e.config.Endpoint, Scope: e.config.Scope, Image: e.config.Image, ExpiresAt: expires}
	if err := e.config.Repository.BeginRuntime(ctx, value.ID, value.Attempt, allocation); err != nil {
		return result, err
	}
	value.Runtime = &allocation
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), min(e.config.CleanupGrace, time.Duration(value.CleanupGraceMs)*time.Millisecond))
		defer cancel()
		if err := e.Cleanup(cleanupCtx, value); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("%w: sandbox cleanup remains pending", domain.ErrRuntimeStopUnconfirmed))
		}
	}()
	remaining := time.Until(expires)
	if remaining <= 0 {
		return result, context.DeadlineExceeded
	}
	// The lifecycle API requires at least 60 seconds; protected PID 1 enforces
	// the execution's earlier absolute deadline independently of this fallback.
	lifetime := max(60, int((remaining+time.Second-1)/time.Second))
	egressJSON, err := json.Marshal(value.RuntimePlan.Egress)
	if err != nil {
		return result, err
	}
	info, err := e.client.CreateSandbox(ctx, sandbox.CreateSandboxRequest{
		Image: &sandbox.ImageSpec{URI: allocation.Image}, Timeout: &lifetime, ResourceLimits: sandbox.ResourceLimits{"cpu": strconv.FormatInt(value.RuntimePlan.Resources.CPUMillis, 10) + "m", "memory": strconv.FormatInt(value.RuntimePlan.Resources.MemoryBytes, 10)},
		Metadata: e.metadata(value), Entrypoint: []string{"/bin/sleep", "infinity"},
		Env: map[string]string{"EXECD_ACCESS_TOKEN": daemonToken, "EXECD_BOOTSTRAP_PRE_SCRIPT": "/usr/local/lib/rank-sandbox-init.sh", "RANK_RUNTIME_EGRESS_POLICY": string(egressJSON), "RANK_RUNTIME_EXPIRES_AT": expires.Format(time.RFC3339Nano), "RANK_RUNTIME_MAX_LIFETIME": (time.Duration(value.RuntimeTimeoutMs+value.CleanupGraceMs) * time.Millisecond).String()},
	})
	if err != nil {
		return result, errors.New("sandbox creation outcome is unknown; invocation was not replayed")
	}
	allocation.Handle = info.ID
	if err := e.config.Repository.AttachRuntime(ctx, value.ID, value.Attempt, info.ID); err != nil {
		return result, err
	}
	endpoint, err := e.client.GetEndpoint(ctx, info.ID, 44772, boolPointer(true))
	if err != nil {
		return result, errors.New("sandbox endpoint resolution failed")
	}
	endpointURL := endpoint.Endpoint
	if !strings.Contains(endpointURL, "://") {
		origin, _ := url.Parse(e.config.Endpoint)
		endpointURL = origin.Scheme + "://" + endpointURL
	}
	origin, _ := url.Parse(e.config.Endpoint)
	proxy, parseErr := url.Parse(endpointURL)
	if parseErr != nil || proxy.Scheme != origin.Scheme || !strings.EqualFold(proxy.Host, origin.Host) || proxy.User != nil || proxy.RawQuery != "" || proxy.Fragment != "" {
		return result, errors.New("sandbox proxy endpoint must share the lifecycle service origin")
	}
	transport := &http.Client{Timeout: e.config.RequestTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	commands := sandbox.NewExecdClient(endpointURL, daemonToken, sandbox.WithHTTPClient(transport), sandbox.WithHeaders(map[string]string{"OPEN-SANDBOX-API-KEY": e.config.APIKey}), sandbox.WithRetry(sandbox.RetryConfig{}))
	for {
		if err := commands.Ping(ctx); err == nil {
			break
		}
		if err := poll(ctx, e.config.PollInterval); err != nil {
			return result, err
		}
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		return result, errors.New("sandbox execution requires a command deadline")
	}
	quota, quotaErr := e.config.ModelQuotaAuthority.Issue(value)
	if quotaErr != nil {
		return result, quotaErr
	}
	request.ModelQuota = quota
	request.Spec.TimeoutMs = time.Until(deadline).Milliseconds()
	if request.Spec.TimeoutMs <= 0 {
		return result, context.DeadlineExceeded
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return result, err
	}
	if len(payload) > 32<<20 {
		return result, errors.New("sandbox worker input exceeds protocol limit")
	}
	// Execd's wire format parses the decimal digits as octal permissions.
	if err = commands.UploadFile(ctx, bytes.NewReader(payload), sandbox.UploadFileOptions{FileName: "request.json", Metadata: sandbox.FileMetadata{Path: sandboxruntime.Root + "/input/request.json", Owner: "node", Group: "node", Mode: 600}}); err != nil {
		return result, errors.New("sandbox private input upload failed")
	}
	var output, stderr bytes.Buffer
	secrets := []string{daemonToken, request.Credential}
	if request.ModelQuota != nil {
		secrets = append(secrets, request.ModelQuota.Token)
	}
	onOutput := func(kind, text string) error {
		if kind == "stdout" {
			if output.Len()+len(text) > 32<<20 {
				return errors.New("sandbox worker response exceeds protocol limit")
			}
			output.WriteString(text)
			return nil
		}
		if kind != "stderr" {
			return nil
		}
		stderr.WriteString(text)
		if stderr.Len() > 2<<20 {
			return errors.New("sandbox worker event exceeds protocol limit")
		}
		for {
			line, err := stderr.ReadString('\n')
			if errors.Is(err, io.EOF) {
				stderr.WriteString(line)
				break
			}
			if err != nil {
				return err
			}
			event, matched, decodeErr := workerprotocol.DecodeEvent(strings.TrimSuffix(line, "\n"))
			if decodeErr != nil {
				return decodeErr
			}
			if matched && emit != nil {
				event.Message = redact(event.Message, secrets)
				event.Data = []byte(redactJSON(string(event.Data), secrets))
				if err := emit(domain.ExecutorEvent{Type: event.Type, Message: event.Message, Data: event.Data, At: event.At}); err != nil {
					return err
				}
			}
		}
		return nil
	}
	commandErr := e.command(ctx, commands, "exec /usr/local/bin/execution-worker < /run/rank/input/request.json", onOutput)
	if commandErr != nil && !errors.Is(commandErr, sandboxCommandExit(1)) {
		return result, errors.New("execution_outcome_unknown: sandbox worker command failed or stream was interrupted; invocation was not replayed")
	}
	var response workerprotocol.Response
	if err = json.Unmarshal([]byte(redactJSON(output.String(), secrets)), &response); err != nil {
		return result, errors.New("execution_outcome_unknown: sandbox worker response is invalid; invocation was not replayed")
	}
	if err := workerprotocol.ValidateResponse(response, value.ID); err != nil {
		return result, fmt.Errorf("execution_outcome_unknown: %w; invocation was not replayed", err)
	}
	if (response.Status == contract.StatusFailed) != (commandErr != nil) {
		return result, errors.New("execution_outcome_unknown: sandbox worker exit and response disagree; invocation was not replayed")
	}
	var responseErr error
	if response.Status == contract.StatusFailed {
		responseErr = errors.New(response.Error)
	}
	if len(response.Result.Artifacts) > e.config.MaxArtifacts {
		return result, errors.New("sandbox artifact count exceeds limit")
	}
	err = e.publishRemoteArtifacts(ctx, commands, value, &response.Result, secrets)
	return response.Result, errors.Join(responseErr, err)
}

func (e *OpenSandbox) metadata(value contract.Execution) map[string]string {
	return map[string]string{"rank.scope": e.config.Scope, "rank.execution": value.ID, "rank.attempt": strconv.Itoa(value.Attempt)}
}
func boolPointer(value bool) *bool { return &value }
func poll(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
func redact(value string, secrets []string) string {
	for _, secret := range secrets {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "[REDACTED]")
		}
	}
	return value
}
func redactJSON(value string, secrets []string) string {
	for _, secret := range secrets {
		if secret != "" {
			encoded, _ := json.Marshal(secret)
			value = strings.ReplaceAll(value, string(encoded[1:len(encoded)-1]), "[REDACTED]")
		}
	}
	return value
}
