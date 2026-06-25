package panel

import (
	"net/http"
	"time"
)

func (s *Server) handlePanelTrafficOverview(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePanelUser(w, r); !ok {
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "store_unavailable", "node store is not configured")
		return
	}
	windowHours := parsePositiveInt(r.URL.Query().Get("window_hours"), 24, 168)
	limit := parsePositiveInt(r.URL.Query().Get("limit"), 20, 100)
	overview, err := s.store.GetTrafficOverview(r.Context(), TrafficOverviewFilter{
		Window: time.Duration(windowHours) * time.Hour,
		Limit:  limit,
		Now:    time.Now().UTC(),
	})
	if err != nil {
		s.logger.Error("get traffic overview", "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "读取流量统计失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"traffic":  overview,
		"overview": overview,
	})
}
