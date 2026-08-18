package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/xyzbit/chat2ranker/rank/backend/internal/runnerprotocol"
	"github.com/xyzbit/chat2ranker/rank/backend/internal/workerruntime"
)

func main() {
	request, err := workerruntime.ParseRequest(os.Stdin)
	if err != nil {
		response := runnerprotocol.Response{ProtocolVersion: runnerprotocol.Version, Status: "failed", Error: fmt.Sprintf("decode request: %v", err)}
		_ = workerruntime.WriteResponse(os.Stdout, response)
		os.Exit(2)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	response := workerruntime.Execute(ctx, request)
	if err := workerruntime.WriteResponse(os.Stdout, response); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Exit(workerruntime.ExitCode(response))
}
