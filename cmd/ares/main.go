package main

import (
	"github.com/ivanvc/ares/internal/config"
	"github.com/ivanvc/ares/internal/server"
)

func main() {
	c := config.Load()

	s := server.New(c)
	s.Start()
}
