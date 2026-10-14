package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (s *Server) serveStatic(w http.ResponseWriter, r *http.Request) {
	if s.staticDir == "" {
		http.NotFound(w, r)
		return
	}
	root, err := filepath.Abs(s.staticDir)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	relativePath := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
	if relativePath == "." {
		relativePath = "index.html"
	}
	filePath := filepath.Join(root, relativePath)
	if filePath != root && !strings.HasPrefix(filePath, root+string(os.PathSeparator)) {
		http.NotFound(w, r)
		return
	}
	if info, err := os.Stat(filePath); err != nil || info.IsDir() {
		filePath = filepath.Join(root, "index.html")
	}
	http.ServeFile(w, r, filePath)
}
