package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/charmbracelet/log"

	"github.com/ivanvc/turnip/internal/adapters/github"
	"github.com/ivanvc/turnip/internal/common"
	"github.com/ivanvc/turnip/internal/config"
	"github.com/ivanvc/turnip/internal/http"
	"github.com/ivanvc/turnip/internal/rpc"
	"github.com/ivanvc/turnip/internal/services/kubernetes"
)

func main() {
	log.Default().SetReportCaller(true)
	log.Default().SetLevel(log.DebugLevel)
	cfg := config.Load()
	common := &common.Common{
		Config:           cfg,
		KubernetesClient: kubernetes.LoadClient(cfg),
		GitHubClient:     github.NewClient(cfg),
	}

	s := http.NewServer(common)
	gs := rpc.NewServer(common)

	done := make(chan os.Signal, 1)

	go s.Start()
	go gs.Start()

	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	<-done
}
