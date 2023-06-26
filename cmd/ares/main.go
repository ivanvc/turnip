package main

import "github.com/ivanvc/ares/internal/server"

func main() {
	s := server.New()
	s.Start()
}
