package http

import (
	"net/http"

	"github.com/charmbracelet/log"
	"github.com/ivanvc/ares/internal/config"
	"github.com/ivanvc/ares/internal/services/kubernetes"
)

type Server struct {
	*http.Server
	*kubernetes.Client
}

// New returns a new Server.
func New(config *config.Config, client *kubernetes.Client) *Server {
	stdlog := log.Default().StandardLog(log.StandardLogOptions{
		ForceLevel: log.ErrorLevel,
	})
	return &Server{&http.Server{
		Addr:     config.ListenHTTP,
		ErrorLog: stdlog,
	}, client}
}

// Starts the HTTP server.
func (s *Server) Start() error {
	log.Info("Starting HTTP server", "listen", s.Addr)
	s.registerHandlers()

	if err := s.ListenAndServe(); err != nil {
		log.Error("Error starting Web Server", "error", err)
		return err
	}

	return nil
}

func (s *Server) registerHandlers() {
	(&webhookHandler{}).registerHandler(s)
	(&statusHandler{}).registerHandler()
}
