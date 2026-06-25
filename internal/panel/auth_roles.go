package panel

import "net/http"

func panelUserHasRole(user *PanelUser, roles ...string) bool {
	if user == nil {
		return false
	}
	for _, role := range roles {
		if user.Role == role {
			return true
		}
	}
	return false
}

func (s *Server) requirePanelRole(w http.ResponseWriter, r *http.Request, roles ...string) (*PanelUser, bool) {
	user, ok := s.requirePanelUser(w, r)
	if !ok {
		return nil, false
	}
	if !panelUserHasRole(user, roles...) {
		writeError(w, http.StatusForbidden, "forbidden", "permission denied")
		return nil, false
	}
	return user, true
}

func (s *Server) requirePanelAdmin(w http.ResponseWriter, r *http.Request) (*PanelUser, bool) {
	return s.requirePanelRole(w, r, PanelUserRoleAdmin)
}
