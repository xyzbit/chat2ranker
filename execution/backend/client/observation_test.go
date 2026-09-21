package client_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xyzbit/agentkiln/execution/backend/client"
	"github.com/xyzbit/agentkiln/execution/backend/contract"
)

func TestObserverErrorsDoNotCancelRemoteInvocation(t *testing.T) {
	for _, callback := range []bool{false, true} {
		t.Run(map[bool]string{false: "transport", true: "callback"}[callback], func(t *testing.T) {
			var cancellations atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/executions":
					json.NewEncoder(w).Encode(contract.Execution{ID: "accepted", Status: contract.StatusRunning})
				case "/v1/executions/accepted/events":
					if !callback {
						http.Error(w, "temporary outage", http.StatusServiceUnavailable)
						return
					}
					w.Header().Set("Content-Type", "text/event-stream")
					w.Write([]byte("data: {\"sequence\":1,\"type\":\"execution.running\"}\n\n"))
				case "/v1/executions/accepted/cancel":
					cancellations.Add(1)
					json.NewEncoder(w).Encode(contract.Execution{ID: "accepted", Status: contract.StatusCancelled})
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			sdk := client.New(server.URL, time.Second)
			_, err := sdk.RunWithEvents(t.Context(), contract.SubmitRequest{IdempotencyKey: "original"}, func(contract.Event) error { return errors.New("projection unavailable") })
			if err == nil {
				t.Fatal("observer failure was hidden")
			}
			if cancellations.Load() != 0 {
				t.Fatal("observation failure cancelled accepted work")
			}
			if _, err := sdk.Cancel(context.Background(), "accepted"); err != nil {
				t.Fatal(err)
			}
			if cancellations.Load() != 1 {
				t.Fatal("explicit cancellation not delivered")
			}
		})
	}
}

func TestNamedStreamFailureIsNotSuccessfulObservation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("event: stream.error\ndata: {\"error\":\"repository unavailable\"}\n\n"))
	}))
	defer server.Close()
	cursor, err := client.New(server.URL, time.Second).Watch(t.Context(), "accepted", 7, nil)
	if err == nil || cursor != 7 {
		t.Fatalf("stream error discarded: cursor=%d err=%v", cursor, err)
	}
}
