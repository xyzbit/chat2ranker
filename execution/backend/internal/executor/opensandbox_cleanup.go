package executor

import (
	"context"
	"errors"

	sandbox "github.com/alibaba/OpenSandbox/sdks/sandbox/go"
	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/execution/backend/internal/domain"
)

// Cleanup never executes a command or creates a runtime. A lost create response
// with no matching resource stays unknown: an empty list cannot exclude a late
// server-side create. This adapter requests no sidecars or retained volumes.
func (e *OpenSandbox) Cleanup(ctx context.Context, value contract.Execution) error {
	if value.Runtime == nil {
		if contract.IsTerminal(value.Status) {
			return nil
		}
		return domain.ErrRuntimeStopUnconfirmed
	}
	if value.Runtime.Endpoint != e.config.Endpoint || value.Runtime.Scope != e.config.Scope {
		return domain.ErrRuntimeStopUnconfirmed
	}
	handles := []string{}
	if value.Runtime.Handle != "" {
		handles = append(handles, value.Runtime.Handle)
	} else {
		for page := 1; ; page++ {
			found, err := e.client.ListSandboxes(ctx, sandbox.ListOptions{Metadata: e.metadata(value), Page: page, PageSize: 100})
			if err != nil {
				return domain.ErrRuntimeStopUnconfirmed
			}
			for _, item := range found.Items {
				handles = append(handles, item.ID)
			}
			if len(found.Items) < 100 {
				break
			}
		}
		if len(handles) == 0 {
			return domain.ErrRuntimeStopUnconfirmed
		}
		// Every exact-scope match remains this attempt's cleanup duty.
		if len(handles) == 1 {
			if err := e.config.Repository.AttachRuntime(ctx, value.ID, value.Attempt, handles[0]); err != nil {
				return err
			}
		}
	}
	for _, handle := range handles {
		info, err := e.client.GetSandbox(ctx, handle)
		if isSandboxNotFound(err) {
			continue
		}
		if err != nil {
			return domain.ErrRuntimeStopUnconfirmed
		}
		for key, want := range e.metadata(value) {
			if info.Metadata[key] != want {
				return domain.ErrRuntimeStopUnconfirmed
			}
		}
		if err := e.client.DeleteSandbox(ctx, handle); err != nil && !isSandboxNotFound(err) {
			return domain.ErrRuntimeStopUnconfirmed
		}
		for {
			_, err := e.client.GetSandbox(ctx, handle)
			if isSandboxNotFound(err) {
				break
			}
			if err != nil {
				return domain.ErrRuntimeStopUnconfirmed
			}
			if err := poll(ctx, e.config.PollInterval); err != nil {
				return domain.ErrRuntimeStopUnconfirmed
			}
		}
	}
	return nil
}

func isSandboxNotFound(err error) bool {
	var api *sandbox.APIError
	return errors.As(err, &api) && api.StatusCode == 404
}
