package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/ivanvc/ares/internal/config"
	"github.com/ivanvc/ares/internal/http"
	"github.com/ivanvc/ares/internal/rpc"
	"github.com/ivanvc/ares/internal/services/kubernetes"
)

func main() {
	c := config.Load()

	cl := kubernetes.LoadClient("default")
	s := http.New(c, cl)
	gs := rpc.New(c)

	done := make(chan os.Signal, 1)

	go s.Start()
	go gs.Start()

	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	<-done
}
