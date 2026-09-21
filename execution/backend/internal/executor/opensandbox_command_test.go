package executor

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sandbox "github.com/alibaba/OpenSandbox/sdks/sandbox/go"
)

func TestSandboxCommandSeparatesProcessExitFromIncompleteStreams(t *testing.T) {
	for _, test := range []struct {
		name, stream string
		code         int
		success      bool
	}{
		{"completed", `{"type":"stdout","text":"output"}` + "\n" + `{"type":"execution_complete"}`, 0, true},
		{"failed worker", `{"type":"error","error":{"ename":"CommandExecError","evalue":"1"}}`, 1, false},
		{"failed worker with keepalive", `{"type":"error","error":{"ename":"CommandExecError","evalue":"1"}}` + "\n" + `{"type":"ping","text":"ping"}`, 1, false},
		{"completed with keepalive", `{"type":"execution_complete"}` + "\n" + `{"type":"ping","text":"ping"}`, 0, true},
		{"keepalive without completion", `{"type":"ping","text":"ping"}`, 0, false},
		{"conflicting completion", `{"type":"error","error":{"ename":"CommandExecError","evalue":"1"}}` + "\n" + `{"type":"execution_complete"}`, 0, false},
		{"other exit", `{"type":"error","error":{"ename":"CommandExecError","evalue":"2"}}`, 2, false},
		{"missing completion", `{"type":"stdout","text":"output"}`, 0, false},
		{"abnormal failure", `{"type":"error","error":{"ename":"CommandExecError","evalue":"-1"}}`, 0, false},
		{"timeout", `{"type":"error","error":{"ename":"TimeoutError","evalue":"1"}}`, 0, false},
		{"trailing output", `{"type":"error","error":{"ename":"CommandExecError","evalue":"1"}}` + "\n" + `{"type":"stdout","text":"late"}`, 0, false},
		{"malformed tail", `{"type":"error","error":{"ename":"CommandExecError","evalue":"1"}}` + "\n" + `{invalid`, 0, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				for _, frame := range strings.Split(test.stream, "\n") {
					fmt.Fprintf(w, "%s\n\n", frame)
				}
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			err := (&OpenSandbox{}).command(ctx, sandbox.NewExecdClient(server.URL, "test"), "exit", func(string, string) error { return nil })
			if (err == nil) != test.success {
				t.Fatalf("success=%v: %v", test.success, err)
			}
			var code sandboxCommandExit
			if errors.As(err, &code) != (test.code != 0) || int(code) != test.code {
				t.Fatalf("exit code=%d: %v", code, err)
			}
		})
	}
}

func TestSandboxReportedExitDoesNotHideTransportInterruption(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Content-Length", "1000")
		fmt.Fprintf(w, "%s\n\n", `{"type":"error","error":{"ename":"CommandExecError","evalue":"1"}}`)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	err := (&OpenSandbox{}).command(ctx, sandbox.NewExecdClient(server.URL, "test"), "exit", func(string, string) error { return nil })
	var code sandboxCommandExit
	if err == nil || errors.As(err, &code) {
		t.Fatalf("truncated transport became a normal exit: %v", err)
	}
}
