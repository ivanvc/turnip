package http

import (
	"net/http"

	"github.com/charmbracelet/log"
	"github.com/ivanvc/turnip/internal/common"
)

type Server struct {
	*http.Server
	*common.Common
}

// New returns a new Server.
func NewServer(common *common.Common) *Server {
	stdlog := log.Default().StandardLog(log.StandardLogOptions{
		ForceLevel: log.ErrorLevel,
	})
	return &Server{&http.Server{
		Addr:     common.Config.ListenHTTP,
		ErrorLog: stdlog,
	}, common}
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
