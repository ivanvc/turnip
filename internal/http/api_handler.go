package http

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/charmbracelet/log"

	"github.com/ivanvc/turnip/internal/adapters/api/handlers"
	"github.com/ivanvc/turnip/internal/adapters/api/objects"
)

// apiHandler holds the HTTP endpoint to handle API calls.
type apiHandler struct{}

// Registers the handler to be used by an HTTP server.
func (h *apiHandler) registerHandler(s *Server) {
	http.HandleFunc("/api/lift", h.handleLift(s))
}

// Handles the HTTP request.
func (h *apiHandler) handleLift(s *Server) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		if s.Config.APIToken == "" {
			w.WriteHeader(http.StatusNotImplemented)
			return
		}

		if req.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		if req.Header.Get("Authentication") != "Bearer "+s.Config.APIToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		decoder := json.NewDecoder(req.Body)
		var lift objects.LiftRequest
		if err := decoder.Decode(&lift); err != nil {
			log.Error("Error unmarshalling", "error", err)
			fmt.Fprintf(w, "{error:%q}", err.Error())
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		res, err := handlers.HandleLift(s.Common, &lift)
		if err != nil {
			fmt.Fprintf(w, "{error:%q}", err.Error())
			log.Error("Error unmarshalling", "error", err)
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		output, err := json.Marshal(res)
		if err != nil {
			fmt.Fprintf(w, "{error:%q}", err.Error())
			log.Error("Error marshalling", "error", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		fmt.Fprintf(w, string(output))
		w.WriteHeader(http.StatusCreated)
	}
}
