package main

import (
	"context"
	"log/slog"
	"os"

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

	os.Exit(runner.Run(ctx, cfg))
}
