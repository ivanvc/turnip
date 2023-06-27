package server

import (
	"net/http"

	"github.com/charmbracelet/log"
	"github.com/ivanvc/ares/internal/config"
)

type Server struct {
	*http.Server
}

const addr = ":8080"

// New returns a new Server.
func New(config *config.Config) *Server {
	stdlog := log.Default().StandardLog(log.StandardLogOptions{
		ForceLevel: log.ErrorLevel,
	})
	return &Server{&http.Server{
		Addr:     config.Listen,
		ErrorLog: stdlog,
	}}
}

// Starts the HTTP server.
func (s *Server) Start() error {
	log.Info("Starting HTTP server", "listen", addr)
	s.registerHandlers()

	if err := s.ListenAndServe(); err != nil {
		log.Error("Error starting Web Server", "error", err)
		return err
	}

	return nil
}

func (s *Server) registerHandlers() {
	(&webhookHandler{}).registerHandler()
}
