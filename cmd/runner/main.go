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

	os.Exit(runner.Run(context.Background(), cfg))
}
