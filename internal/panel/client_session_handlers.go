package panel

import "net/http"

func (s *Server) handlePanelClientSessions(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePanelUser(w, r); !ok {
		return
	}
	s.handleListClientSessions(w, r)
}

func (s *Server) handleBackendClientSessions(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeAPIKey(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid API key")
		return
	}
	s.handleListClientSessions(w, r)
}

func (s *Server) handleListClientSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "store_unavailable", "node store is not configured")
		return
	}
	filter := ClientSessionFilter{
		NodeID:      r.URL.Query().Get("node_id"),
		UserID:      r.URL.Query().Get("user_id"),
		DeviceID:    r.URL.Query().Get("device_id"),
		Status:      r.URL.Query().Get("status"),
		CloseReason: r.URL.Query().Get("close_reason"),
		WindowHours: parsePositiveInt(r.URL.Query().Get("window_hours"), 24, 24*90),
		Limit:       parsePositiveInt(r.URL.Query().Get("limit"), 50, 500),
		Offset:      parsePositiveInt(r.URL.Query().Get("offset"), 0, 1000000),
	}
	result, err := s.store.ListClientSessions(r.Context(), filter)
	if err != nil {
		s.logger.Error("list client sessions", "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "list client sessions failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"sessions": result.Sessions,
		"overview": result.Overview,
		"limit":    result.Limit,
		"offset":   result.Offset,
		"count":    len(result.Sessions),
	})
}
