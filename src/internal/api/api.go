package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/jakerobb/nut-influx-relay/internal/store"
)

// Server exposes HTTP endpoints for UPS status.
type Server struct {
	s    *store.Store
	port int
}

// New creates a new API Server.
func New(s *store.Store, port int) *Server {
	return &Server{s: s, port: port}
}

// Start registers routes and begins listening. It blocks until the server fails.
func (srv *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", srv.handleHealth)
	mux.HandleFunc("/ups", srv.handleUPSList)
	mux.HandleFunc("/ups/", srv.handleUPSByLabel)

	addr := fmt.Sprintf("0.0.0.0:%d", srv.port)
	return http.ListenAndServe(addr, mux)
}

func (srv *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`)) //nolint:errcheck
}

func (srv *Server) handleUPSList(w http.ResponseWriter, _ *http.Request) {
	all := srv.s.All()
	if all == nil {
		all = []*store.UpsStats{}
	}
	writeJSON(w, http.StatusOK, all)
}

func (srv *Server) handleUPSByLabel(w http.ResponseWriter, r *http.Request) {
	// Strip "/ups/" prefix to get the label.
	label := r.URL.Path[len("/ups/"):]
	if label == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "label required"})
		return
	}

	stats := srv.s.Get(label)
	if stats == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	writeJSON(w, http.StatusOK, stats)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.Encode(v) //nolint:errcheck
}
