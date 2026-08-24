package main

import (
	"context"
	"fmt"
	"os"

	"github.com/ivanvc/turnip/internal/runner"
)

func main() {
	cfg, err := runner.ConfigFromEnv(os.Getenv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "runner: %v\n", err)
		os.Exit(1)
	}

	os.Exit(runner.Run(context.Background(), cfg))
}
