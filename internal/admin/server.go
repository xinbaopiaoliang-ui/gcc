package admin

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"time"

	"gaccel-node/internal/config"
	"gaccel-node/internal/metrics"
	"gaccel-node/internal/sessions"
)

type Server struct {
	cfg       *config.Manager
	logger    *slog.Logger
	collector *metrics.Collector
	sessions  *sessions.Registry
}

func NewServer(cfg *config.Manager, logger *slog.Logger, collector *metrics.Collector, sessionRegistry *sessions.Registry) *Server {
	return &Server{
		cfg:       cfg,
		logger:    logger.With("component", "admin"),
		collector: collector,
		sessions:  sessionRegistry,
	}
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/sessions", s.handleSessions)
	mux.HandleFunc("/metrics", s.handleMetrics)
	mux.HandleFunc("/config/reload", s.handleConfigReload)
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	server := &http.Server{
		Addr:              s.cfg.Current().Admin.Listen,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("admin listening", "listen", s.cfg.Current().Admin.Listen)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	cfg := s.cfg.Current()
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"config": map[string]any{
			"path": s.cfg.Path(),
		},
		"server": map[string]string{
			"listen": cfg.Server.Listen,
			"alpn":   cfg.Server.ALPN,
		},
		"node":    cfg.Node,
		"metrics": s.collector.Snapshot(),
	})
}

func (s *Server) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	s.collector.Snapshot().WritePrometheus(w)
}

func (s *Server) handleSessions(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"sessions": s.sessions.Snapshot(),
	})
}

func (s *Server) handleConfigReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	cfg, err := s.cfg.Reload()
	if err != nil {
		s.logger.Warn("config reload failed", "error", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.logger.Info("config reloaded", "path", s.cfg.Path())
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"path":   s.cfg.Path(),
		"server": map[string]string{
			"listen": cfg.Server.Listen,
			"alpn":   cfg.Server.ALPN,
		},
		"node": cfg.Node,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
