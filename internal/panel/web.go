package panel

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func (s *Server) handleWeb(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	root := strings.TrimSpace(s.cfg.Web.Root)
	if root == "" {
		http.NotFound(w, r)
		return
	}

	cleanPath := path.Clean("/" + r.URL.Path)
	relativePath := strings.TrimPrefix(cleanPath, "/")
	filePath := filepath.Join(root, filepath.FromSlash(relativePath))
	if info, err := os.Stat(filePath); err == nil && !info.IsDir() {
		http.ServeFile(w, r, filePath)
		return
	}

	indexPath := filepath.Join(root, "index.html")
	if info, err := os.Stat(indexPath); err == nil && !info.IsDir() {
		http.ServeFile(w, r, indexPath)
		return
	}
	http.NotFound(w, r)
}
