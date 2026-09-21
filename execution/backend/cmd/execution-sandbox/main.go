package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/xyzbit/agentkiln/execution/backend/internal/sandboxruntime"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	mode := flag.String("mode", "", "network-init, init or artifact")
	name := flag.String("path", "", "relative artifact path")
	limit := flag.Int64("limit", 0, "artifact byte limit")
	flag.Parse()
	switch *mode {
	case "init", "network-init":
		expires, err := time.Parse(time.RFC3339Nano, os.Getenv("RANK_RUNTIME_EXPIRES_AT"))
		if err != nil {
			return errors.New("invalid sandbox deadline")
		}
		maximum, err := time.ParseDuration(os.Getenv("RANK_RUNTIME_MAX_LIFETIME"))
		if err != nil || maximum <= 0 {
			return errors.New("invalid sandbox maximum lifetime")
		}
		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
		defer cancel()
		if *mode == "network-init" {
			return sandboxruntime.NetworkInit(ctx, expires, maximum)
		}
		return sandboxruntime.Init(ctx, expires, maximum)
	case "artifact":
		return sandboxruntime.StreamArtifact(os.Stdout, sandboxruntime.Root+"/artifacts", *name, *limit)
	default:
		return errors.New("sandbox mode must be network-init, init or artifact")
	}
}
