package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jakerobb/nut-relay/internal/metrics"
	"github.com/jakerobb/nut-relay/internal/store"
)

// Server exposes a JSON view of UPS state, plus Prometheus metrics when
// the output is prometheus.
type Server struct {
	s        *store.Store
	renderer *metrics.Renderer // nil unless the output is prometheus
	port     int
}

// New creates a new API Server. With a nil renderer, /metrics isn't served.
func New(s *store.Store, renderer *metrics.Renderer, port int) *Server {
	return &Server{s: s, renderer: renderer, port: port}
}

// Handler returns the server's routes.
func (srv *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	if srv.renderer != nil {
		mux.HandleFunc("/metrics", srv.handleMetrics)
	}
	mux.HandleFunc("/health", srv.handleHealth)
	mux.HandleFunc("/ups", srv.handleUPSList)
	mux.HandleFunc("/ups/", srv.handleUPSByLabel)
	return mux
}

// Start begins listening. It blocks until the server fails.
func (srv *Server) Start() error {
	server := &http.Server{
		Addr:              fmt.Sprintf("0.0.0.0:%d", srv.port),
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	return server.ListenAndServe()
}

func (srv *Server) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	// Render into a buffer first, so a failure can still be a 500.
	var buf bytes.Buffer
	if err := srv.renderer.Write(&buf, srv.s.All(), time.Now()); err != nil {
		slog.Error("failed to render metrics", "err", err)
		http.Error(w, "failed to render metrics", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	if _, err := w.Write(buf.Bytes()); err != nil {
		slog.Error("failed to write metrics response", "err", err)
	}
}

// handleHealth reports the process is up. It doesn't depend on any UPS
// being reachable; nut_up covers that.
func (srv *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, err := w.Write([]byte(`{"status":"ok"}`))
	if err != nil {
		slog.Error("failed to write health response", "err", err)
	}
}

func (srv *Server) handleUPSList(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, srv.s.All())
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
	err := enc.Encode(v)
	if err != nil {
		slog.Error("failed to encode JSON response", "err", err)
	}
}
