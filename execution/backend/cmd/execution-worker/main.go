package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/xyzbit/chat2ranker/execution/backend/contract"
	"github.com/xyzbit/chat2ranker/execution/backend/internal/workerprotocol"
	"github.com/xyzbit/chat2ranker/execution/backend/internal/workerruntime"
)

func main() {
	request, err := workerruntime.ParseRequest(os.Stdin)
	if err != nil {
		response := workerprotocol.Response{ProtocolVersion: workerprotocol.Version, Status: contract.StatusFailed, Error: fmt.Sprintf("decode request: %v", err)}
		_ = workerruntime.WriteResponse(os.Stdout, response)
		os.Exit(2)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	var eventMu sync.Mutex
	emit := func(event workerprotocol.Event) error {
		payload, err := workerprotocol.EncodeEvent(event)
		if err != nil {
			return err
		}
		eventMu.Lock()
		defer eventMu.Unlock()
		_, err = fmt.Fprintln(os.Stderr, string(payload))
		return err
	}
	response := workerruntime.Execute(ctx, request, emit)
	if err := workerruntime.WriteResponse(os.Stdout, response); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Exit(workerruntime.ExitCode(response))
}
