package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestHTTPDrainCompletesSlowRequestAndBoundsUnfinishedStream(t *testing.T) {
	for _, complete := range []bool{true, false} {
		name := "deadline"
		if complete {
			name = "slow-completion"
		}
		t.Run(name, func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(200)
				w.(http.Flusher).Flush()
				close(started)
				select {
				case <-release:
					_, _ = w.Write([]byte("completed"))
				case <-r.Context().Done():
				}
			})}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { server.Close() })
			go server.Serve(listener)
			response, err := http.Get("http://" + listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			<-started
			result := make(chan error, 1)
			timeout := 100 * time.Millisecond
			if complete {
				timeout = 30 * time.Second
			}
			go func() { result <- shutdownHTTP(server, timeout) }()
			if complete {
				select {
				case err := <-result:
					t.Fatalf("drain ended before slow request: %v", err)
				case <-time.After(6 * time.Second):
				}
				close(release)
				output, err := io.ReadAll(response.Body)
				if err != nil || string(output) != "completed" {
					t.Fatal(string(output), err)
				}
				if err := <-result; err != nil {
					t.Fatal(err)
				}
			} else {
				if err := <-result; err != context.DeadlineExceeded {
					t.Fatal(err)
				}
				if _, err := io.ReadAll(response.Body); err == nil {
					t.Fatal("unfinished stream was reported as complete")
				}
			}
		})
	}
}
