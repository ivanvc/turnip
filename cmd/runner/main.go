package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/ivanvc/turnip/internal/logging"
	"github.com/ivanvc/turnip/internal/runner"
)

func main() {
	slog.SetDefault(logging.New(os.Stderr, logging.ParseLevel(os.Getenv("TURNIP_LOG_LEVEL"))))

	cfg, err := runner.ConfigFromEnv(os.Getenv)
	if err != nil {
		slog.Error("loading config", "error", err)
		os.Exit(1)
	}

	ctx := context.Background()

	// The clone runs in an initContainer, before the container that
	// executes the tool exists — and, under the run-in-image strategy,
	// in a different image from it. Selecting the mode by argument keeps
	// both in one binary, so the clone's merge semantics and token
	// redaction have a single implementation. See runner.RunClone.
	if len(os.Args) > 1 && os.Args[1] == "clone" {
		os.Exit(runner.RunClone(ctx, cfg))
	}

	// git invokes this as `<runner> credential get` while cloning, so the
	// credential never has to exist anywhere git could persist it. Same
	// single-binary dispatch as the clone above.
	if len(os.Args) > 2 && os.Args[1] == "credential" {
		os.Exit(runner.RunCredential(ctx, cfg, os.Args[2], os.Stdin, os.Stdout, os.Stderr))
	}

	// The kubelet sends SIGTERM at the Job's deadline, on an eviction, or
	// when the Pod is deleted. Caught here, it stops the tool rather than
	// the Runner, which then still reports the result within the Pod's
	// grace period. Only this mode catches it: a clone told to stop simply
	// ends, and the sweep finalizes its Job.
	stop := make(chan struct{})
	terms := make(chan os.Signal, 1)
	signal.Notify(terms, syscall.SIGTERM)
	go func() {
		<-terms
		close(stop)
	}()

	os.Exit(runner.RunWithStop(ctx, cfg, stop))
}
