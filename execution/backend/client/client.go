// Package client provides the Go client for the generic Execution Service.
package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/xyzbit/chat2ranker/execution/backend/contract"
)

type Client struct {
	baseURL string
	http    *http.Client
	poll    time.Duration
}

func New(baseURL string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 15 * time.Minute
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: &http.Client{Timeout: timeout}, poll: 40 * time.Millisecond}
}

func (client *Client) Probe(ctx context.Context, harness string) (contract.Availability, error) {
	var result contract.Availability
	err := client.call(ctx, http.MethodGet, "/v1/runtimes/"+url.PathEscape(harness), nil, &result)
	return result, err
}

func (client *Client) ListModelConnections(ctx context.Context) ([]contract.ModelConnection, error) {
	var result []contract.ModelConnection
	err := client.call(ctx, http.MethodGet, "/v1/model-connections", nil, &result)
	return result, err
}
func (client *Client) ListModelCatalog(ctx context.Context) ([]contract.ModelProvider, error) {
	var result []contract.ModelProvider
	err := client.call(ctx, http.MethodGet, "/v1/model-catalog", nil, &result)
	return result, err
}
func (client *Client) GetModelConnection(ctx context.Context, id string) (contract.ModelConnection, error) {
	var result contract.ModelConnection
	err := client.call(ctx, http.MethodGet, "/v1/model-connections/"+url.PathEscape(id), nil, &result)
	return result, err
}
func (client *Client) CreateModelConnection(ctx context.Context, input contract.ModelConnectionInput) (contract.ModelConnection, error) {
	var result contract.ModelConnection
	err := client.call(ctx, http.MethodPost, "/v1/model-connections", input, &result)
	return result, err
}
func (client *Client) UpdateModelConnection(ctx context.Context, id string, input contract.ModelConnectionInput) (contract.ModelConnection, error) {
	var result contract.ModelConnection
	err := client.call(ctx, http.MethodPatch, "/v1/model-connections/"+url.PathEscape(id), input, &result)
	return result, err
}
func (client *Client) VerifyModelConnection(ctx context.Context, id string) (contract.ModelConnection, error) {
	var result contract.ModelConnection
	err := client.call(ctx, http.MethodPost, "/v1/model-connections/"+url.PathEscape(id)+"/verify", map[string]any{}, &result)
	return result, err
}
func (client *Client) DeleteModelConnection(ctx context.Context, id string) error {
	return client.call(ctx, http.MethodDelete, "/v1/model-connections/"+url.PathEscape(id), nil, nil)
}
func (client *Client) ListSystemModelBindings(ctx context.Context) ([]contract.SystemModelBinding, error) {
	var result []contract.SystemModelBinding
	err := client.call(ctx, http.MethodGet, "/v1/system-model-bindings", nil, &result)
	return result, err
}
func (client *Client) GetSystemModelBinding(ctx context.Context, role string) (contract.SystemModelBinding, error) {
	var result contract.SystemModelBinding
	err := client.call(ctx, http.MethodGet, "/v1/system-model-bindings/"+url.PathEscape(role), nil, &result)
	return result, err
}
func (client *Client) SaveSystemModelBinding(ctx context.Context, role string, input contract.SystemModelBindingInput) (contract.SystemModelBinding, error) {
	var result contract.SystemModelBinding
	err := client.call(ctx, http.MethodPut, "/v1/system-model-bindings/"+url.PathEscape(role), input, &result)
	return result, err
}

func (client *Client) Submit(ctx context.Context, request contract.SubmitRequest) (contract.Execution, error) {
	var result contract.Execution
	err := client.call(ctx, http.MethodPost, "/v1/executions", request, &result)
	return result, err
}

func (client *Client) Get(ctx context.Context, id string) (contract.Execution, error) {
	var result contract.Execution
	err := client.call(ctx, http.MethodGet, "/v1/executions/"+url.PathEscape(id), nil, &result)
	return result, err
}

func (client *Client) Cancel(ctx context.Context, id string) (contract.Execution, error) {
	var result contract.Execution
	err := client.call(ctx, http.MethodPost, "/v1/executions/"+url.PathEscape(id)+"/cancel", map[string]any{}, &result)
	return result, err
}

func (client *Client) Run(ctx context.Context, request contract.SubmitRequest) (contract.Execution, error) {
	return client.RunWithEvents(ctx, request, nil)
}

// RunWithEvents submits one invocation, consumes its durable event stream,
// and returns the terminal execution.
func (client *Client) RunWithEvents(ctx context.Context, request contract.SubmitRequest, handle func(contract.Event) error) (contract.Execution, error) {
	execution, err := client.Submit(ctx, request)
	if err != nil {
		return execution, err
	}
	// Always replay from sequence zero, including when a fast worker reached a
	// terminal state before Submit returned. Durable events are the source of
	// progress callbacks; terminal status alone must not skip them.
	if _, err = client.Watch(ctx, execution.ID, 0, handle); err != nil {
		_, _ = client.Cancel(context.Background(), execution.ID)
		return execution, err
	}
	execution, err = client.Get(ctx, execution.ID)
	if err != nil {
		return execution, err
	}
	if execution.Status != contract.StatusCompleted || execution.Result == nil {
		return execution, fmt.Errorf("execution %s ended as %s: %s", execution.ID, execution.Status, execution.Error)
	}
	return execution, nil
}

// Watch consumes ordered execution events until the terminal event closes the
// stream. It returns the last received per-execution sequence.
func (client *Client) Watch(ctx context.Context, id string, after int64, handle func(contract.Event) error) (int64, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, client.baseURL+"/v1/executions/"+url.PathEscape(id)+"/events?after="+fmt.Sprint(after), nil)
	if err != nil {
		return after, err
	}
	request.Header.Set("Accept", "text/event-stream")
	response, err := client.http.Do(request)
	if err != nil {
		return after, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		payload, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		return after, fmt.Errorf("execution event stream returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(payload)))
	}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 64<<10), 2<<20)
	var data string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			data = strings.TrimPrefix(line, "data: ")
			continue
		}
		if line != "" || data == "" {
			continue
		}
		var event contract.Event
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return after, fmt.Errorf("decode execution event: %w", err)
		}
		data = ""
		if event.Sequence <= after {
			continue
		}
		after = event.Sequence
		if handle != nil {
			if err := handle(event); err != nil {
				return after, err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return after, err
	}
	return after, nil
}

func (client *Client) ReadArtifact(ctx context.Context, executionID, path string) (contract.ArtifactContent, error) {
	var result contract.ArtifactContent
	err := client.call(ctx, http.MethodGet, "/v1/executions/"+url.PathEscape(executionID)+"/artifacts?path="+url.QueryEscape(path), nil, &result)
	return result, err
}

func (client *Client) call(ctx context.Context, method, path string, body, target any) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	request, err := http.NewRequestWithContext(ctx, method, client.baseURL+path, reader)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var problem struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(payload, &problem)
		if problem.Error.Message == "" {
			problem.Error.Message = strings.TrimSpace(string(payload))
		}
		return fmt.Errorf("execution service returned HTTP %d: %s", response.StatusCode, problem.Error.Message)
	}
	if target == nil {
		return nil
	}
	return json.Unmarshal(payload, target)
}
