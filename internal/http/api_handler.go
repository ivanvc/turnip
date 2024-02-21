package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/charmbracelet/log"

	"github.com/ivanvc/turnip/internal/adapters/api/handlers"
	"github.com/ivanvc/turnip/internal/adapters/api/objects"
)

var (
	// ErrAPITokenNotSet is the error message when the API token is not set.
	ErrAPITokenNotSet = errors.New("API Token not set, API capabilities disabled. Please set the API token in to enable it")
	// ErrMethodNotAllowed is the error message when the HTTP method is not allowed.
	ErrMethodNotAllowed = errors.New("Method not allowed")
	// ErrUnauthorized is the error message when the request is not authorized.
	ErrUnauthorized = errors.New("Unauthorized")
)

// apiHandler holds the HTTP endpoint to handle API calls.
type apiHandler struct{}

// Registers the handler to be used by an HTTP server.
func (h *apiHandler) registerHandler(s *Server) {
	http.HandleFunc("/api/plot", h.handlePlot(s))
	http.HandleFunc("/api/lift", h.handleLift(s))
}

// handleLift handles the lift HTTP request.
func (h *apiHandler) handleLift(s *Server) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		if header, err := authorize(s, req); err != nil {
			fmt.Fprintf(w, `{"error":%q}`, err.Error())
			w.WriteHeader(header)
			return
		}

		decoder := json.NewDecoder(req.Body)
		var lift objects.APIRequest
		if err := decoder.Decode(&lift); err != nil {
			log.Error("Error unmarshalling", "error", err)
			fmt.Fprintf(w, `{"error":%q}`, err.Error())
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		res, err := handlers.Handle(s.Common, "lift", &lift)
		if err != nil {
			fmt.Fprintf(w, `{"error":%q}`, err.Error())
			log.Error("Error unmarshalling", "error", err)
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		output, err := json.Marshal(res)
		if err != nil {
			fmt.Fprintf(w, `{"error":%q}`, err.Error())
			log.Error("Error marshalling", "error", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		fmt.Fprintf(w, string(output))
		w.WriteHeader(http.StatusCreated)
	}
}

// handlePlot handles the plot HTTP request.
func (h *apiHandler) handlePlot(s *Server) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		if header, err := authorize(s, req); err != nil {
			fmt.Fprintf(w, `{"error":%q}`, err.Error())
			w.WriteHeader(header)
			return
		}

		decoder := json.NewDecoder(req.Body)
		var plot objects.APIRequest
		if err := decoder.Decode(&plot); err != nil {
			log.Error("Error unmarshalling", "error", err)
			fmt.Fprintf(w, `{"error":%q}`, err.Error())
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		res, err := handlers.Handle(s.Common, "plot", &plot)
		if err != nil {
			fmt.Fprintf(w, `{"error":%q}`, err.Error())
			log.Error("Error unmarshalling", "error", err)
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		output, err := json.Marshal(res)
		if err != nil {
			fmt.Fprintf(w, `{"error":%q}`, err.Error())
			log.Error("Error marshalling", "error", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		fmt.Fprintf(w, string(output))
		w.WriteHeader(http.StatusCreated)
	}
}

func authorize(s *Server, req *http.Request) (int, error) {
	if s.Config.APIToken == "" {
		return http.StatusNotImplemented, ErrAPITokenNotSet
	}

	if req.Method != http.MethodPost {
		return http.StatusMethodNotAllowed, ErrMethodNotAllowed
	}

	if req.Header.Get("Authorization") != "Bearer "+s.Config.APIToken {
		return http.StatusUnauthorized, ErrUnauthorized
	}

	return http.StatusContinue, nil
}
